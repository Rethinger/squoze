package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestDynamicUpstreamRouting proves the header-driven multi-provider mode:
// each request names its own destination; the routing header is consumed
// (never leaked upstream) and the static default still works as fallback.
func TestDynamicUpstreamRouting(t *testing.T) {
	var hitA, hitB, sawHeader bool
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitA = true
		_, _ = io.WriteString(w, `{"from":"A"}`)
	}))
	defer a.Close()
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitB = true
		sawHeader = r.Header.Get(upstreamHeader) != ""
		_, _ = io.WriteString(w, `{"from":"B"}`)
	}))
	defer b.Close()

	srv := httptest.NewServer(NewWithEngine(mustURL(t, a.URL), nil))
	defer srv.Close()

	post := func(body, dyn string) *http.Response {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/chat/completions", strings.NewReader(body))
		if dyn != "" {
			req.Header.Set(upstreamHeader, dyn)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp
	}

	// Header routes to B; header is stripped before egress.
	post(`{"m":1}`, b.URL)
	if !hitB || hitA {
		t.Fatalf("header routing wrong: A=%v B=%v", hitA, hitB)
	}
	if sawHeader {
		t.Fatal("routing header leaked upstream")
	}

	// No header → static fallback.
	post(`{"m":2}`, "")
	if !hitA {
		t.Fatal("static fallback did not receive the request")
	}

	// Header-only mode: no static upstream, missing header → 502.
	hOnly := httptest.NewServer(NewWithEngine(nil, nil))
	defer hOnly.Close()
	resp, err := http.Post(hOnly.URL+"/x", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("missing route must 502, got %d", resp.StatusCode)
	}

	// Header-only mode with header → routes fine.
	hitB = false
	req, _ := http.NewRequest(http.MethodPost, hOnly.URL+"/v1/x", strings.NewReader(`{"m":3}`))
	req.Header.Set(upstreamHeader, b.URL)
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if !hitB {
		t.Fatal("header-only mode did not route")
	}
}
