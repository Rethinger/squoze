package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"

	"github.com/Rethinger/squoze/internal/engine"
	"github.com/Rethinger/squoze/internal/proxy"
)

// runLivecheck proves the whole loop against a REAL provider: a big tool
// output with known failure facts goes through squoze, the provider must
// accept the squeezed body and answer questions that require the facts.
// API key is read from an env var and never logged.
func runLivecheck(args []string) int {
	fs := flag.NewFlagSet("livecheck", flag.ExitOnError)
	upstream := fs.String("upstream", "", "provider base URL without version path, e.g. https://api.fireworks.ai/inference")
	model := fs.String("model", "", "model id (required)")
	authHeader := fs.String("auth-header", "", "raw Authorization header value; if empty, read from --auth-env")
	authEnv := fs.String("auth-env", "SQUOZE_LIVECHECK_TOKEN", "env var holding the bearer token")
	fs.Parse(args)

	if *upstream == "" || *model == "" {
		fmt.Fprintln(os.Stderr, "usage: squoze livecheck --upstream URL --model ID [--auth-header VALUE]")
		return 2
	}
	token := *authHeader
	if token == "" {
		token = os.Getenv(*authEnv)
	}
	if token == "" {
		fmt.Fprintf(os.Stderr, "livecheck: no auth: set --auth-header or env %s\n", *authEnv)
		return 2
	}

	u, err := url.Parse(*upstream)
	if err != nil {
		fmt.Fprintf(os.Stderr, "livecheck: bad upstream: %v\n", err)
		return 2
	}

	// Synthetic tool output: verbose, with facts we will quiz the model on.
	var blob strings.Builder
	blob.WriteString("$ go test ./... -count=1\n")
	for i := 0; i < 800; i++ {
		switch i {
		case 200:
			blob.WriteString("--- FAIL: TestCheckout (0.03s)\n    checkout_test.go:88: total mismatch: want 4200 got 420\n")
		case 400:
			blob.WriteString("--- FAIL: TestRefund (0.01s)\n    refund_test.go:12: refund idempotency violated\n")
		default:
			blob.WriteString("ok  verbose padding line for livecheck payload padding padding\n")
		}
	}
	raw, _ := json.Marshal(blob.String())
	body := []byte(`{"model":"` + *model + `","max_tokens":1024,"messages":[` +
		`{"role":"user","content":"Run the test suite and tell me what broke."},` +
		`{"role":"tool","content":` + string(raw) + `},` +
		`{"role":"user","content":"Answer strictly from the tool output above, one line: which tests failed and what total did TestCheckout expect?"}]}`)

	squozed := httptest.NewServer(proxy.NewWithEngine(mustURL(u), engine.NewEngine(engine.DefaultMemoCapacity)))
	defer squozed.Close()

	req, _ := http.NewRequest(http.MethodPost, squozed.URL+"/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "livecheck: request failed: %v\n", err)
		return 1
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	fmt.Printf("HTTP %d | format=%s original=%s sent=%s\n",
		resp.StatusCode,
		resp.Header.Get("X-Squoze-Format"),
		resp.Header.Get("X-Squoze-Original-Bytes"),
		resp.Header.Get("X-Squoze-Sent-Bytes"))

	var parsed struct {
		Choices []struct {
			Message struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		fmt.Fprintf(os.Stderr, "livecheck: non-JSON response: %.200s\n", respBody)
		return 1
	}
	if parsed.Error != nil {
		fmt.Fprintf(os.Stderr, "livecheck: provider error: %s\n", parsed.Error.Message)
		return 1
	}
	answer := ""
	if len(parsed.Choices) > 0 {
		answer = parsed.Choices[0].Message.Content
		if strings.TrimSpace(answer) == "" {
			// Reasoning models may spend the whole budget on thoughts; the
			// reasoning trace still proves whether the facts survived.
			answer = parsed.Choices[0].Message.ReasoningContent
		}
	}
	if os.Getenv("SQUOZE_LIVECHECK_DEBUG") == "1" {
		fmt.Printf("raw response (%d bytes): %.800s\n", len(respBody), respBody)
	}
	fmt.Printf("answer: %s\n", strings.TrimSpace(answer))

	okStatus := resp.StatusCode == http.StatusOK
	okFacts := strings.Contains(answer, "TestCheckout") && strings.Contains(answer, "4200")
	if okFacts && okStatus {
		fmt.Println("LIVECHECK PASS: provider accepted squeezed body and facts survived end-to-end")
		return 0
	}
	fmt.Printf("LIVECHECK FAIL: status_ok=%v facts_ok=%v\n", okStatus, okFacts)
	return 1
}

func mustURL(raw *url.URL) *url.URL { return raw }
