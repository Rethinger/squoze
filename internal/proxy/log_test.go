package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestRequestLogJSONL(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer up.Close()
	u, err := url.Parse(up.URL)
	if err != nil {
		t.Fatal(err)
	}

	var logBuf strings.Builder
	srv := NewWithEngine(u, nil).WithLog(&logBuf)

	big := strings.Repeat("2026-08-24T10:00:00Z INFO handled ok\n", 200)
	payload := `{"model":"m","messages":[{"role":"tool","content":` + jsonStr(big) + `}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(payload))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	lines := strings.Split(strings.TrimSpace(logBuf.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("want 1 log line, got %d", len(lines))
	}
	var entry map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatalf("log line is not JSON: %v", err)
	}
	for _, k := range []string{"ts", "method", "path", "status", "format", "original_bytes", "sent_bytes", "saved_pct", "blocks", "duration_ms"} {
		if _, ok := entry[k]; !ok {
			t.Fatalf("log entry missing key %q: %v", k, entry)
		}
	}
	if entry["path"] != "/v1/chat/completions" || entry["blocks"].(float64) != 1 {
		t.Fatalf("wrong entry values: %v", entry)
	}
	if entry["sent_bytes"].(float64) >= entry["original_bytes"].(float64) {
		t.Fatal("expected real savings in logged request")
	}
}

// jsonStr marshals s into a JSON string literal.
func jsonStr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
