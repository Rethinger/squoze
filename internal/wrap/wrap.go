// Package wrap implements `squoze wrap CMD`: run an agent command with its
// provider base URLs pointed at an in-process squoze proxy. The agent needs
// zero configuration — env injection is the whole trick.
package wrap

import (
	"context"
	"errors"
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
	Command     []string // argv of the agent to run (required)
	Upstream    *url.URL // real provider base URL (required)
	OriginsDir  string   // "" = memory-only originals
	ListenAddr  string   // default "127.0.0.1:0" (ephemeral)
	LogFile     string   // "" = no request log; else append JSONL per request
	BaseURLEnvs []string // override the injected endpoint env names (harness presets)
	Stdin       io.Reader
	Stdout      io.Writer
	Stderr      io.Writer
	OnExit      func() // runs after the child exits, before Run returns
	// Upstreams enables multi-provider mode: one local listener per entry
	// (addr → real provider URL). Overrides Upstream; used by `sq <agent>`
	// so every wired provider keeps its own port and original endpoint.
	Upstreams map[string]string
}

// BaseURLEnvs lists the environment variables agents commonly honor for
// provider endpoints. Setting all of them covers Claude Code, OpenAI SDKs,
// Codex and most OpenAI-compatible clients.
var BaseURLEnvs = []string{
	"ANTHROPIC_BASE_URL",
	"OPENAI_BASE_URL",
	"OPENAI_API_BASE",
}

// EnvVars returns KEY=VALUE entries pointing at addr.
func EnvVars(addr string) []string { return envVars(BaseURLEnvs, addr) }

func envVars(keys []string, addr string) []string {
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"=http://"+addr)
	}
	return out
}

// Run starts the proxy (single upstream or a per-provider listener farm),
// launches the command with injected environment, streams stdio, and waits.
// Returns the command's exit error (if any).
func Run(ctx context.Context, opts Options) error {
	if len(opts.Command) == 0 {
		return fmt.Errorf("wrap: no command given")
	}
	if opts.Upstream == nil && len(opts.Upstreams) == 0 {
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

	var closers []func()
	defer func() {
		for _, c := range closers {
			c()
		}
	}()

	// Multi-provider mode: one listener per wired provider, each with its
	// own static upstream — works for custom providers whose options the
	// agent SDK drops (opencode#5674), no routing headers needed.
	if len(opts.Upstreams) > 0 {
		type listener struct {
			addr string
			srv  *http.Server
		}
		var listeners []listener
		first := ""
		for laddr, up := range opts.Upstreams {
			u, perr := url.Parse(up)
			if perr != nil {
				return fmt.Errorf("wrap: upstream %q: %w", up, perr)
			}
			ln, lerr := net.Listen("tcp", laddr)
			if lerr != nil {
				return fmt.Errorf("wrap: listen %s: %w", laddr, lerr)
			}
			h := proxy.NewWithEngine(u, eng)
			if opts.LogFile != "" {
				// Log file is opened once by the single-mode branch below;
				// in multi mode open a dedicated handle per listener share.
				if f, ferr := os.OpenFile(opts.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); ferr == nil {
					h.WithLog(f)
					closers = append(closers, func() { _ = f.Close() })
				}
			}
			srv := &http.Server{Handler: h}
			go func(l net.Listener, s *http.Server) { _ = s.Serve(l) }(ln, srv)
			listeners = append(listeners, listener{addr: ln.Addr().String(), srv: srv})
			if first == "" {
				first = ln.Addr().String()
			}
		}
		closers = append([]func(){func() {
			for _, l := range listeners {
				_ = l.srv.Close()
			}
		}}, closers...)

		ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
		defer stop()
		cmd := exec.CommandContext(ctx, opts.Command[0], opts.Command[1:]...)
		cmd.Env = append(os.Environ(), envVars(keysFor(opts), first)...)
		applyStd(cmd, opts)
		fmt.Fprintf(os.Stderr, "squoze: wrapping %v via %d provider listeners\n", opts.Command, len(listeners))
		if opts.OnExit != nil {
			defer opts.OnExit()
		}
		return runChild(cmd)
	}

	if opts.Upstream == nil {
		return fmt.Errorf("wrap: --upstream is required")
	}
	handler := proxy.NewWithEngine(opts.Upstream, eng)
	if opts.LogFile != "" {
		f, err := os.OpenFile(opts.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return fmt.Errorf("wrap: log file: %w", err)
		}
		defer f.Close()
		handler.WithLog(f)
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("wrap: listen: %w", err)
	}
	srv := &http.Server{Handler: handler}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	keys := BaseURLEnvs
	if len(opts.BaseURLEnvs) > 0 {
		keys = opts.BaseURLEnvs
	}
	cmd := exec.CommandContext(ctx, opts.Command[0], opts.Command[1:]...)
	cmd.Env = append(os.Environ(), envVars(keys, ln.Addr().String())...)
	applyStd(cmd, opts)

	fmt.Fprintf(os.Stderr, "squoze: wrapping %v → %s via http://%s\n",
		opts.Command, opts.Upstream, ln.Addr())
	if opts.OnExit != nil {
		defer opts.OnExit()
	}
	return runChild(cmd)
}

func keysFor(opts Options) []string {
	if len(opts.BaseURLEnvs) > 0 {
		return opts.BaseURLEnvs
	}
	return BaseURLEnvs
}

func applyStd(cmd *exec.Cmd, opts Options) {
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
}

func runChild(cmd *exec.Cmd) error {
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return &ExitError{Code: ee.ExitCode()}
		}
		return err
	}
	return nil
}

// ExitError carries the child's exit code through Run's error return so
// callers can re-exit with the same code (after running their own cleanup).
type ExitError struct{ Code int }

func (e *ExitError) Error() string { return fmt.Sprintf("agent exited with code %d", e.Code) }

// ExitCode extracts the child exit code from a Run error (0 when none).
func ExitCode(err error) int {
	var ee *ExitError
	if errors.As(err, &ee) {
		return ee.Code
	}
	return 1
}
