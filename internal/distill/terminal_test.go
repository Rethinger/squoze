package distill

import (
	"strings"
	"testing"
)

func TestStripANSI(t *testing.T) {
	input := "\x1b[32mPASS\x1b[0m \x1b[1mTestCalculation\x1b[0m (0.01s)\x1b[2K"
	want := "PASS TestCalculation (0.01s)"
	got := StripANSI(input)
	if got != want {
		t.Fatalf("StripANSI mismatch:\ngot:  %q\nwant: %q", got, want)
	}

	// No ANSI returns same string without allocation
	plain := "Hello world without colors"
	if StripANSI(plain) != plain {
		t.Fatalf("plain string altered")
	}
}

func TestCleanCarriageReturns(t *testing.T) {
	input := "Downloading: [=>     ] 10%\rDownloading: [===>   ] 40%\rDownloading: [======>] 100%\nDone!"
	got := CleanCarriageReturns(input)
	if !strings.Contains(got, "Downloading: [======>] 100%") || strings.Contains(got, "10%") {
		t.Fatalf("failed to resolve progress bar overwrite:\n%s", got)
	}
}

func TestFoldRepeatedLines(t *testing.T) {
	var lines []string
	lines = append(lines, "Starting worker pool...")
	for i := 0; i < 20; i++ {
		lines = append(lines, "Waiting for database lock...")
	}
	lines = append(lines, "Lock acquired successfully.")
	input := strings.Join(lines, "\n")

	got := FoldRepeatedLines(input)
	if !strings.Contains(got, "line repeated 19 times") {
		t.Fatalf("failed to fold repetitive lines:\n%s", got)
	}
	if strings.Count(got, "Waiting for database lock...") != 1 {
		t.Fatalf("expected exactly 1 instance of repeated line, got count=%d", strings.Count(got, "Waiting for database lock..."))
	}
}

func TestSanitizeTerminal(t *testing.T) {
	raw := "\x1b[34m[INFO]\x1b[0m Loading dependencies...\n" +
		strings.Repeat("\x1b[33mScanning module cache...\x1b[0m\n", 15) +
		"\x1b[32m[SUCCESS]\x1b[0m All dependencies resolved.\n"

	sanitized, changed := SanitizeTerminal(raw)
	if !changed {
		t.Fatal("expected SanitizeTerminal to report changed=true")
	}
	if strings.Contains(sanitized, "\x1b") {
		t.Fatal("ANSI escape codes remain in output")
	}
	if !strings.Contains(sanitized, "line repeated 14 times") {
		t.Fatalf("line folding missing:\n%s", sanitized)
	}
}
