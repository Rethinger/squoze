package eval

import (
	"encoding/json"
	"strings"

	"github.com/tidwall/gjson"
)

// wrapToolContent builds an OpenAI-chat request carrying content as a tool
// message for the given model id.
func wrapToolContent(model, content string) ([]byte, error) {
	raw, err := json.Marshal(content)
	if err != nil {
		return nil, err
	}
	body := `{"model":"` + model + `","messages":[` +
		`{"role":"user","content":"run the suite"},` +
		`{"role":"tool","content":` + string(raw) + `}]}`
	return []byte(body), nil
}

func extractToolContent(body []byte) string {
	return gjson.GetBytes(body, "messages.1.content").String()
}

// StandardCases returns the deterministic fixture set: one per common tool-
// output class, each with facts that must survive and a savings floor.
func StandardCases() []Case {
	var b strings.Builder
	b.WriteString("$ go test ./internal/... -count=1\n")
	// FAILs deliberately deep in the middle: they must survive via the
	// error-rescue path, not by landing in head/tail windows.
	for i := 0; i < 600; i++ {
		switch {
		case i == 137:
			b.WriteString("--- FAIL: TestCheckout (0.03s)\n    checkout_test.go:88: total mismatch: want 4200 got 420\n")
		case i == 421:
			b.WriteString("--- FAIL: TestRefund (0.01s)\n    refund_test.go:12: refund idempotency violated\n")
		case i%7 == 0:
			b.WriteString("ok  internal/payments 0.42s\n")
		default:
			b.WriteString("    payments_test.go:12: verbose assertion padding padding padding ok\n")
		}
	}
	goTest := b.String()

	py := strings.Repeat("tests/test_api.py::test_login PASSED [  2%]\n", 300)
	for i := 0; i < 5; i++ {
		py += "FAILED tests/test_payments.py::test_refund - assert 420 == 4200\n"
		py += "E       AssertionError: refund amount mismatch\n"
	}

	logs := ""
	for i := 0; i < 500; i++ {
		lvl := "INFO"
		if i%250 == 0 {
			lvl = "ERROR"
		}
		logs += "2026-08-24T10:" + twoDigits(i/60%60) + ":" + twoDigits(i%60) + "Z " + lvl + " api request handled latency=12ms\n"
	}

	return []Case{
		{
			Name:     "go_test_600",
			Model:    "gpt-5",
			Original: goTest,
			MustKeep: []string{
				"--- FAIL: TestCheckout",
				"total mismatch: want 4200 got 420",
				"refund idempotency violated",
				"$ go test ./internal/...",
			},
			MinSavingsPct: 80,
		},
		{
			Name:     "pytest_verbose",
			Model:    "claude-sonnet-4-20250514",
			Original: py + strings.Repeat("tests/test_misc.py PASSED [ 99%]\n", 300),
			MustKeep: []string{
				"FAILED tests/test_payments.py::test_refund",
				"AssertionError: refund amount mismatch",
			},
			MinSavingsPct: 30,
		},
		{
			Name:          "server_logs_errors",
			Model:         "deepseek-chat",
			Original:      logs,
			MustKeep:      []string{"ERROR"},
			MinSavingsPct: 70,
		},
	}
}

func twoDigits(n int) string {
	if n < 10 {
		return "0" + string(rune('0'+n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}
