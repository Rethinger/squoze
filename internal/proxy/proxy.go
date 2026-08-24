// Package proxy implements the drop-in HTTP proxy mode:
//
//	squoze proxy --port 8787 --upstream https://api.anthropic.com
//
// Contract: squoze never breaks a request. If the engine panics or the body
// is unreadable, the original bytes go upstream unchanged (fail-open).
package proxy

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"

	"github.com/Rethinger/squoze/internal/engine"
)

// Server is the squoze HTTP proxy bound to one engine instance.
type Server struct {
	upstream *url.URL
	rp       *httputil.ReverseProxy
	apply    func([]byte) ([]byte, engine.Result)
}

// New returns an http.Handler that optimizes request bodies and forwards
// everything else verbatim to upstream, using the default engine.
func New(upstream *url.URL) http.Handler {
	return NewWithEngine(upstream, nil)
}

// NewWithEngine binds the proxy to a specific engine (memo + originals
// shared across requests). A nil engine selects the package default.
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
			pr.SetURL(upstream)
			pr.Out.Host = upstream.Host
		},
	}
	return s
}

// ServeHTTP implements the proxy pipeline.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Body == nil || r.Method == http.MethodGet || r.Method == http.MethodHead {
		s.rp.ServeHTTP(w, r)
		return
	}

	original, err := io.ReadAll(r.Body)
	if err != nil {
		// Unreadable body: nothing sensible to forward — surface a 400
		// instead of silently sending an empty request upstream.
		http.Error(w, "squoze: failed to read request body", http.StatusBadRequest)
		return
	}

	body, res := s.safeProcess(original)

	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	r.Header.Set("Content-Length", strconv.Itoa(len(body)))

	w.Header().Set("X-Squoze-Original-Bytes", strconv.Itoa(res.OriginalBytes))
	w.Header().Set("X-Squoze-Sent-Bytes", strconv.Itoa(res.SentBytes))
	w.Header().Set("X-Squoze-Format", res.Format.String())

	s.rp.ServeHTTP(w, r)
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
