package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func BenchmarkProxyRoundTrip(b *testing.B) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer up.Close()
	u, err := url.Parse(up.URL)
	if err != nil {
		b.Fatal(err)
	}
	srv := httptest.NewServer(New(u))
	defer srv.Close()

	big := strings.Repeat("2026-08-24T10:00:00Z INFO handled ok\n", 200)
	raw, err := json.Marshal(big)
	if err != nil {
		b.Fatal(err)
	}
	payload := `{"model":"m","messages":[{"role":"tool","content":` + string(raw) + `}]}`

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json",
			strings.NewReader(payload))
		if err != nil {
			b.Fatal(err)
		}
		resp.Body.Close()
	}
}
