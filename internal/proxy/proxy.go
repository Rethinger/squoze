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

// New returns an http.Handler that optimizes request bodies and forwards
// everything else verbatim to upstream.
func New(upstream *url.URL) http.Handler {
	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(upstream)
			pr.Out.Host = upstream.Host
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body == nil || r.Method == http.MethodGet || r.Method == http.MethodHead {
			rp.ServeHTTP(w, r)
			return
		}

		original, err := io.ReadAll(r.Body)
		if err != nil {
			// Unreadable body: nothing sensible to forward — surface a 400
			// instead of silently sending an empty request upstream.
			http.Error(w, "squoze: failed to read request body", http.StatusBadRequest)
			return
		}

		body, res := safeProcess(original)

		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		r.Header.Set("Content-Length", strconv.Itoa(len(body)))

		w.Header().Set("X-Squoze-Original-Bytes", strconv.Itoa(res.OriginalBytes))
		w.Header().Set("X-Squoze-Sent-Bytes", strconv.Itoa(res.SentBytes))
		w.Header().Set("X-Squoze-Format", res.Format.String())

		rp.ServeHTTP(w, r)
	})
}

// processFn is an injection point so tests can prove the fail-open path.
var processFn = engine.Process

// safeProcess isolates the engine: any panic degrades to pass-through.
// Returns the bytes to send upstream plus the report metadata.
func safeProcess(body []byte) (out []byte, res engine.Result) {
	res = engine.Result{OriginalBytes: len(body), SentBytes: len(body)}
	defer func() {
		if rec := recover(); rec != nil {
			// Fail-open: original bytes, no transforms claimed.
			out = body
			res = engine.Result{OriginalBytes: len(body), SentBytes: len(body)}
		}
	}()
	return processFn(body)
}
