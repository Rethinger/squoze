package wrap

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestEnvVarsPointAtProxy(t *testing.T) {
	env := EnvVars("127.0.0.1:4567")
	if len(env) != len(BaseURLEnvs) {
		t.Fatalf("env count = %d, want %d", len(env), len(BaseURLEnvs))
	}
	for _, e := range env {
		if !strings.HasSuffix(e, "=http://127.0.0.1:4567") {
			t.Fatalf("bad env entry %q", e)
		}
	}
}

// TestRunInjectsEnvAndProxies uses the re-exec pattern: the child is this
// test binary re-run with SQUOZE_WRAP_CHILD=1; it prints the base-URL envs
// and makes a request through the proxy to prove the whole loop works.
func TestRunInjectsEnvAndProxies(t *testing.T) {
	if os.Getenv("SQUOZE_WRAP_CHILD") == "1" {
		childBody()
		return
	}

	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer up.Close()
	u, err := url.Parse(up.URL)
	if err != nil {
		t.Fatal(err)
	}

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// The child inherits this flag and takes the childBody branch; the
	// parent already passed the guard above, so it cannot self-retrigger.
	t.Setenv("SQUOZE_WRAP_CHILD", "1")

	var out bytes.Buffer
	cmd := []string{exe, "-test.run=TestRunInjectsEnvAndProxies"}
	err = Run(context.Background(), Options{
		Command:    cmd,
		Upstream:   u,
		ListenAddr: "127.0.0.1:0",
		Stdout:     &out,
		Stderr:     &out,
	})
	if err != nil {
		t.Fatalf("wrap run failed: %v\n%s", err, out.String())
	}

	got := out.String()
	if !strings.Contains(got, "ANTHROPIC_BASE_URL=http://127.0.0.1:") {
		t.Fatalf("child did not see injected env:\n%s", got)
	}
	if !strings.Contains(got, "PROXY_HIT") {
		t.Fatalf("request through wrapped proxy failed:\n%s", got)
	}
}

// childBody runs inside the re-executed child process.
func childBody() {
	var sb strings.Builder
	for _, k := range BaseURLEnvs {
		sb.WriteString(k + "=" + os.Getenv(k) + "\n")
	}
	base := os.Getenv("ANTHROPIC_BASE_URL")
	resp, err := http.Post(base+"/v1/messages", "application/json",
		strings.NewReader(`{"model":"claude","system":"s","messages":[{"role":"user","content":"hi"}],"max_tokens":8}`))
	if err != nil {
		sb.WriteString("POST ERROR: " + err.Error() + "\n")
	} else {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.Header.Get("X-Squoze-Format") != "" && string(body) == `{"ok":true}` {
			sb.WriteString("PROXY_HIT\n")
		} else {
			sb.WriteString("UNEXPECTED RESPONSE\n")
		}
	}
	os.Stdout.WriteString(sb.String())
	os.Exit(0)
}
