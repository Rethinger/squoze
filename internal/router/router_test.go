package router

import (
	"strings"
	"testing"
)

func TestClassifyKinds(t *testing.T) {
	goTest := strings.Repeat("=== RUN TestFoo\n", 5) + "--- FAIL: TestFoo (0.00s)\n" + strings.Repeat("    x_test.go:12: boom\n", 20)
	pyTest := strings.Repeat("test_spam.py PASSED [  1%]\n", 40)
	logs := strings.Repeat("2026-08-24T10:00:00Z INFO server ready\n", 30)
	items := make([]string, 40)
	for i := range items {
		items[i] = `{"id":1,"name":"aaaaaaaaaaaaaaaaaaaa"}`
	}
	jsonBlob := `{"results":[` + strings.Join(items, ",") + `]}`
	prose := "We should discuss the migration plan tomorrow. The team prefers a gradual rollout with feature flags and a rollback path."

	cases := []struct {
		name string
		in   string
		want Kind
	}{
		{"go test output", goTest, KindTestOutput},
		{"pytest output", pyTest, KindTestOutput},
		{"timestamped logs", logs, KindLogOutput},
		{"json", jsonBlob, KindJSON},
		{"prose", prose, KindProse},
		{"empty", "", KindProse},
	}
	for _, tc := range cases {
		if got := Classify(tc.in); got != tc.want {
			t.Errorf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}

func TestClassifyDeterministic(t *testing.T) {
	in := strings.Repeat("2026-08-24T10:00:00Z ERROR db timeout\n", 25)
	first := Classify(in)
	for i := 0; i < 5; i++ {
		if got := Classify(in); got != first {
			t.Fatalf("non-deterministic: %v vs %v", got, first)
		}
	}
}
