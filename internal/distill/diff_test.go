package distill

import (
	"strings"
	"testing"
)

func TestIsLockfileOrGenerated(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"package-lock.json", true},
		{"frontend/pnpm-lock.yaml", true},
		{"go.sum", true},
		{"Cargo.lock", true},
		{"dist/bundle.min.js", true},
		{"app.js.map", true},
		{"internal/server/server.go", false},
		{"README.md", false},
	}

	for _, tt := range tests {
		if got := IsLockfileOrGenerated(tt.path); got != tt.want {
			t.Errorf("IsLockfileOrGenerated(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestDistillDiff(t *testing.T) {
	var diffBuilder strings.Builder

	// Real code diff
	diffBuilder.WriteString("diff --git a/internal/server/server.go b/internal/server/server.go\n")
	diffBuilder.WriteString("--- a/internal/server/server.go\n")
	diffBuilder.WriteString("+++ b/internal/server/server.go\n")
	diffBuilder.WriteString("@@ -10,6 +10,8 @@ package server\n")
	for i := 0; i < 15; i++ {
		diffBuilder.WriteString(" func ExistingUnchangedFunction" + string(rune('A'+i)) + "() {}\n")
	}
	diffBuilder.WriteString("+func NewFeatureAdded() string {\n")
	diffBuilder.WriteString("+    return \"awesome feature\"\n")
	diffBuilder.WriteString("+}\n")

	// Massive lockfile diff (300 lines)
	diffBuilder.WriteString("diff --git a/package-lock.json b/package-lock.json\n")
	diffBuilder.WriteString("--- a/package-lock.json\n")
	diffBuilder.WriteString("+++ b/package-lock.json\n")
	diffBuilder.WriteString("@@ -100,200 +100,500 @@\n")
	for i := 0; i < 300; i++ {
		if i%2 == 0 {
			diffBuilder.WriteString("+\"node_modules/dep-" + string(rune('a'+(i%26))) + "\": {\"version\": \"1.2.3\"},\n")
		} else {
			diffBuilder.WriteString("-\"node_modules/dep-" + string(rune('a'+(i%26))) + "\": {\"version\": \"1.0.0\"},\n")
		}
	}

	rawDiff := diffBuilder.String()
	distilled, changed := DistillDiff(rawDiff)
	if !changed {
		t.Fatal("expected DistillDiff to report changed=true")
	}

	// 1. Real source code addition MUST survive verbatim
	if !strings.Contains(distilled, "func NewFeatureAdded() string {") {
		t.Fatal("source code additions were erroneously removed")
	}
	if !strings.Contains(distilled, "return \"awesome feature\"") {
		t.Fatal("source code return statement missing")
	}

	// 2. Unchanged context lines should be compacted
	if !strings.Contains(distilled, "unchanged context lines elided") {
		t.Fatal("expected unchanged context lines to be compacted")
	}

	// 3. Lockfile diff MUST be summarized and elided
	if !strings.Contains(distilled, "[... squoze:") || !strings.Contains(distilled, "package-lock.json diff elided") {
		t.Fatalf("lockfile diff was not elided:\n%s", distilled)
	}
	if strings.Contains(distilled, "\"node_modules/dep-") {
		t.Fatal("raw lockfile JSON lines survived in output")
	}
}
