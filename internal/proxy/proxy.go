// Package proxy implements the drop-in HTTP proxy mode:
//
//	squoze proxy --port 8787 --upstream https://api.anthropic.com
//
// Contract: squoze never breaks a request. If the engine panics or the body
// is unreadable, the original bytes go upstream unchanged (fail-open).
package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/Rethinger/squoze/internal/engine"
	"github.com/Rethinger/squoze/internal/harness"
)

// Server is the squoze HTTP proxy bound to one engine instance.
type Server struct {
	upstream *url.URL // static default; per-request header overrides it
	rp       *httputil.ReverseProxy
	apply    func([]byte) ([]byte, engine.Result)

	logMu sync.Mutex
	logW  io.Writer // nil = no request log
}

// upstreamHeader aliases the harness routing header (single source there).
const upstreamHeader = harness.UpstreamHeader

// WithLog attaches a request log sink (JSONL, one object per request).
func (s *Server) WithLog(w io.Writer) *Server {
	s.logW = w
	return s
}

// New returns an http.Handler that optimizes request bodies and forwards
// everything else verbatim to upstream, using the default engine.
func New(upstream *url.URL) http.Handler {
	return NewWithEngine(upstream, nil)
}

// NewWithEngine binds the proxy to a specific engine (memo + originals
// shared across requests). A nil engine selects the package default.
// A nil upstream enables header-only routing: every request MUST carry
// X-Squoze-Upstream or gets a 502 (used by `sq <config-agent>` where each
// provider carries its own original endpoint).
func NewWithEngine(upstream *url.URL, eng *engine.Engine) *Server {
	if eng == nil {
		eng = engine.Default()
	}
	s := &Server{
		upstream: upstream,
		apply:    eng.Apply,
	}
	s.rp = &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			target := s.upstream
			if h := pr.In.Header.Get(upstreamHeader); h != "" {
				pr.Out.Header.Del(upstreamHeader)
				if u, err := url.Parse(h); err == nil && u.Scheme != "" && u.Host != "" {
					target = u
				}
			}
			if target != nil {
				// Do NOT use SetURL — it joins target.Path with the incoming
				// path (singleJoiningSlash). Our local baseURL already
				// includes the provider's path prefix (e.g. /v1,
				// /inference/v1), so the incoming path is already the
				// complete upstream path. Joining would double it:
				// /inference/v1 + /inference/v1/chat -> 404.
				pr.Out.URL.Scheme = target.Scheme
				pr.Out.URL.Host = target.Host
				if target.RawQuery != "" {
					if pr.Out.URL.RawQuery != "" {
						pr.Out.URL.RawQuery = target.RawQuery + "&" + pr.Out.URL.RawQuery
					} else {
						pr.Out.URL.RawQuery = target.RawQuery
					}
				}
				pr.Out.Host = target.Host
			}
		},
	}
	return s
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// ServeHTTP implements the proxy pipeline.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
	var res engine.Result
	defer func() {
		if s.logW == nil {
			return
		}
		s.logMu.Lock()
		defer s.logMu.Unlock()
		var savedPct float64
		if res.SavedBytes > 0 && res.OriginalBytes > 0 {
			savedPct = 100 * float64(res.SavedBytes) / float64(res.OriginalBytes)
		}
		line, _ := json.Marshal(map[string]any{
			"ts":             start.UTC().Format(time.RFC3339Nano),
			"method":         r.Method,
			"path":           r.URL.Path,
			"status":         rec.status,
			"format":         res.Format.String(),
			"original_bytes": res.OriginalBytes,
			"sent_bytes":     res.SentBytes,
			"saved_pct":      math.Round(savedPct*10) / 10,
			"blocks":         res.BlocksSqueezed,
			"memo_hits":      res.MemoHits,
			"duration_ms":    float64(time.Since(start).Microseconds()) / 1000,
		})
		s.logW.Write(append(line, '\n'))
	}()

	// Header-only routing mode: without a static upstream every request
	// must name its destination, or there is nowhere to go.
	if s.upstream == nil && r.Header.Get(upstreamHeader) == "" {
		http.Error(rec, "squoze: no route: request lacks "+upstreamHeader+" and no default upstream is configured", http.StatusBadGateway)
		return
	}

	if r.Body == nil || r.Method == http.MethodGet || r.Method == http.MethodHead {
		s.rp.ServeHTTP(rec, r)
		return
	}

	original, err := io.ReadAll(r.Body)
	if err != nil {
		// Unreadable body: nothing sensible to forward — surface a 400
		// instead of silently sending an empty request upstream.
		http.Error(rec, "squoze: failed to read request body", http.StatusBadRequest)
		return
	}

	var body []byte
	body, res = s.safeProcess(original)

	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	r.Header.Set("Content-Length", strconv.Itoa(len(body)))

	w.Header().Set("X-Squoze-Original-Bytes", strconv.Itoa(res.OriginalBytes))
	w.Header().Set("X-Squoze-Sent-Bytes", strconv.Itoa(res.SentBytes))
	w.Header().Set("X-Squoze-Format", res.Format.String())

	s.rp.ServeHTTP(rec, r)
}

// safeProcess isolates the engine: any panic degrades to pass-through.
// Returns the bytes to send upstream plus the report metadata.
func (s *Server) safeProcess(body []byte) (out []byte, res engine.Result) {
	res = engine.Result{OriginalBytes: len(body), SentBytes: len(body)}
	defer func() {
		if rec := recover(); rec != nil {
			// Fail-open: original bytes, no transforms claimed.
			out = body
			res = engine.Result{OriginalBytes: len(body), SentBytes: len(body)}
		}
	}()
	return s.apply(body)
}
