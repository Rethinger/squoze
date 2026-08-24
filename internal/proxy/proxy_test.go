package proxy

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/Rethinger/squoze/internal/engine"
)

func TestProxyPassthroughPreservesBodyAndEchoesSavings(t *testing.T) {
	var upstreamGot []byte
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamGot, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer up.Close()

	u, err := url.Parse(up.URL)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"model":"claude","system":"be nice","messages":[{"role":"user","content":"hi"}],"max_tokens":10}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	New(u).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if string(upstreamGot) != body {
		t.Fatalf("passthrough violated: upstream got %q", upstreamGot)
	}
	for h, want := range map[string]string{
		"X-Squoze-Original-Bytes": strconv.Itoa(len(body)),
		"X-Squoze-Sent-Bytes":     strconv.Itoa(len(body)),
		"X-Squoze-Format":         "anthropic_messages",
	} {
		if got := rec.Header().Get(h); got != want {
			t.Fatalf("%s = %q, want %q", h, got, want)
		}
	}
}

func TestProxyFailOpenOnInvalidJSONStillForwards(t *testing.T) {
	var upstreamGot []byte
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamGot, _ = io.ReadAll(r.Body)
		_, _ = io.WriteString(w, `{}`)
	}))
	defer up.Close()
	u, _ := url.Parse(up.URL)

	broken := `{"model": broken`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(broken))
	rec := httptest.NewRecorder()
	New(u).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || string(upstreamGot) != broken {
		t.Fatalf("fail-open contract: status=%d upstream=%q", rec.Code, upstreamGot)
	}
	if got := rec.Header().Get("X-Squoze-Format"); got != "unknown" {
		t.Fatalf("format = %q, want unknown", got)
	}
}

func TestSafeProcessSurvivesEnginePanic(t *testing.T) {
	orig := processFn
	processFn = func([]byte) engine.Result { panic("boom") }
	defer func() { processFn = orig }()

	body := []byte(`{"anything":true}`)
	got, res := safeProcess(body)
	if !bytes.Equal(got, body) {
		t.Fatalf("panic must degrade to pass-through, got %q", got)
	}
	if res.SentBytes != len(body) || len(res.Transforms) != 0 {
		t.Fatalf("metadata wrong after recovery: %+v", res)
	}
}
