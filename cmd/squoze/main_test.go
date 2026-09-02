package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rethinger/squoze/internal/engine"
)

// buildCLI compiles the command once per test binary and returns its path. The
// CLI surface is only observable through a real process (exit codes, stdout vs
// stderr), so the table tests below drive the built binary rather than calling
// main().
func buildCLI(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "squoze")
	if os.PathSeparator == '\\' {
		bin += ".exe"
	}
	out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}
	return bin
}

// run executes the CLI with args, returning stdout, stderr and the exit code
// separately — which stream carries usage is part of the contract.
func run(t *testing.T, bin string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	var so, se strings.Builder
	cmd.Stdout = &so
	cmd.Stderr = &se
	err := cmd.Run()
	switch e := err.(type) {
	case nil:
		code = 0
	case *exec.ExitError:
		code = e.ExitCode()
	default:
		t.Fatalf("run %v: %v", args, err)
	}
	return so.String(), se.String(), code
}

// TestVersionSpellings pins that every documented spelling prints the same line
// to stdout and exits 0. SECURITY.md tells vulnerability reporters to run
// `squoze --version`, and scripts may parse the output, so both the wording and
// the stream matter.
func TestVersionSpellings(t *testing.T) {
	bin := buildCLI(t)
	want := "squoze v" + engine.Version + "\n"
	for _, arg := range []string{"version", "-v", "-version", "--version"} {
		t.Run(arg, func(t *testing.T) {
			stdout, stderr, code := run(t, bin, arg)
			if code != 0 {
				t.Errorf("exit code = %d, want 0 (stderr: %s)", code, stderr)
			}
			if stdout != want {
				t.Errorf("stdout = %q, want %q", stdout, want)
			}
		})
	}
}

// TestHelpGoesToStdout guards the convention that an explicit help request is a
// successful, pipeable result rather than an error.
func TestHelpGoesToStdout(t *testing.T) {
	bin := buildCLI(t)
	for _, arg := range []string{"help", "-h", "--help"} {
		t.Run(arg, func(t *testing.T) {
			stdout, stderr, code := run(t, bin, arg)
			if code != 0 {
				t.Errorf("exit code = %d, want 0", code)
			}
			if !strings.Contains(stdout, "Usage:") {
				t.Errorf("usage missing from stdout: %q", stdout)
			}
			if stderr != "" {
				t.Errorf("stderr = %q, want empty", stderr)
			}
		})
	}
}

// TestUsageListsEverySubcommand keeps the help text honest: it drifted once
// already, advertising four subcommands while main() dispatched seven, which hid
// agent/harness/livecheck from anyone not reading the README.
func TestUsageListsEverySubcommand(t *testing.T) {
	bin := buildCLI(t)
	stdout, _, _ := run(t, bin, "help")
	for _, sub := range []string{"proxy", "wrap", "agent", "harness", "livecheck", "retrieve", "version", "help"} {
		if !strings.Contains(stdout, "squoze "+sub) {
			t.Errorf("usage does not mention %q", sub)
		}
	}
	// The top-level agent shorthand is the headline ergonomic feature; it is
	// invisible in the dispatch table, so usage must call it out explicitly.
	if !strings.Contains(stdout, "squoze oc") {
		t.Error("usage does not document the top-level agent shorthand")
	}
}

// TestNoArgsFailsWithUsageOnStderr pins exit 2 on bare invocation. Turning this
// into a successful help print would strip shell callers of their error signal.
func TestNoArgsFailsWithUsageOnStderr(t *testing.T) {
	bin := buildCLI(t)
	stdout, stderr, code := run(t, bin)
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr, "Usage:") {
		t.Errorf("usage missing from stderr: %q", stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
}

// TestUnknownCommandNamesTheCommand — previously an unknown subcommand printed
// bare usage, leaving a typo indistinguishable from a missing feature.
func TestUnknownCommandNamesTheCommand(t *testing.T) {
	bin := buildCLI(t)
	_, stderr, code := run(t, bin, "bogus-command")
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr, `unknown command "bogus-command"`) {
		t.Errorf("stderr does not name the unknown command: %q", stderr)
	}
	if !strings.Contains(stderr, "Usage:") {
		t.Errorf("usage missing from stderr: %q", stderr)
	}
}

// TestAgentAliasStillResolves covers the regression risk introduced by handling
// reserved words before the alias lookup: `squoze oc` must still reach the agent
// path, identically to `squoze agent opencode`.
func TestAgentAliasStillResolves(t *testing.T) {
	bin := buildCLI(t)
	viaAlias, _, aliasCode := run(t, bin, "oc", "--help")
	viaSubcommand, _, subCode := run(t, bin, "agent", "opencode", "--help")
	if aliasCode != subCode {
		t.Errorf("exit codes differ: alias %d vs subcommand %d", aliasCode, subCode)
	}
	if viaAlias != viaSubcommand {
		t.Errorf("alias output differs from subcommand output:\nalias: %q\nsubcommand: %q", viaAlias, viaSubcommand)
	}
}
