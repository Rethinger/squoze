// Package wrap implements `squoze wrap CMD`: run an agent command with its
// provider base URLs pointed at an in-process squoze proxy. The agent needs
// zero configuration — env injection is the whole trick.
package wrap

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/Rethinger/squoze/internal/engine"
	"github.com/Rethinger/squoze/internal/proxy"
	"github.com/Rethinger/squoze/internal/store"
)

// Options configures one wrap session.
type Options struct {
	Command    []string // argv of the agent to run (required)
	Upstream   *url.URL // real provider base URL (required)
	OriginsDir string   // "" = memory-only originals
	ListenAddr string   // default "127.0.0.1:0" (ephemeral)
	Stdin      io.Reader
	Stdout     io.Writer
	Stderr     io.Writer
}

// BaseURLEnvs lists the environment variables agents commonly honor for
// provider endpoints. Setting all of them covers Claude Code, OpenAI SDKs,
// Codex and most OpenAI-compatible clients.
var BaseURLEnvs = []string{
	"ANTHROPIC_BASE_URL",
	"OPENAI_BASE_URL",
	"OPENAI_API_BASE",
}

// EnvVars returns the child-process environment entries pointing at addr.
func EnvVars(addr string) []string {
	out := make([]string, 0, len(BaseURLEnvs))
	for _, k := range BaseURLEnvs {
		out = append(out, k+"=http://"+addr)
	}
	return out
}

// Run starts the proxy, launches the command with injected environment,
// streams stdio, and waits. Returns the command's exit error (if any).
func Run(ctx context.Context, opts Options) error {
	if len(opts.Command) == 0 {
		return fmt.Errorf("wrap: no command given")
	}
	if opts.Upstream == nil {
		return fmt.Errorf("wrap: --upstream is required")
	}
	addr := opts.ListenAddr
	if addr == "" {
		addr = "127.0.0.1:0"
	}

	var eng *engine.Engine
	if opts.OriginsDir != "" {
		orig, err := store.OpenOriginals(opts.OriginsDir)
		if err != nil {
			return fmt.Errorf("wrap: originals store: %w", err)
		}
		eng = engine.NewEngineWith(engine.DefaultMemoCapacity, orig)
	} else {
		eng = engine.NewEngine(engine.DefaultMemoCapacity)
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("wrap: listen: %w", err)
	}
	srv := &http.Server{Handler: proxy.NewWithEngine(opts.Upstream, eng)}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	cmd := exec.CommandContext(ctx, opts.Command[0], opts.Command[1:]...)
	cmd.Env = append(os.Environ(), EnvVars(ln.Addr().String())...)
	cmd.Stdin = opts.Stdin
	if cmd.Stdin == nil {
		cmd.Stdin = os.Stdin
	}
	cmd.Stdout = opts.Stdout
	if cmd.Stdout == nil {
		cmd.Stdout = os.Stdout
	}
	cmd.Stderr = opts.Stderr
	if cmd.Stderr == nil {
		cmd.Stderr = os.Stderr
	}

	fmt.Fprintf(os.Stderr, "squoze: wrapping %v → %s via http://%s\n",
		opts.Command, opts.Upstream, ln.Addr())
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			os.Exit(ee.ExitCode())
		}
		return err
	}
	return nil
}
