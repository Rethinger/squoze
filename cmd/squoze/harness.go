package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/Rethinger/squoze/internal/engine"
	"github.com/Rethinger/squoze/internal/harness"
	"github.com/Rethinger/squoze/internal/proxy"
	"github.com/Rethinger/squoze/internal/store"
	"github.com/Rethinger/squoze/internal/wrap"
)

// runHarness implements `squoze harness`:
//
//	squoze harness list
//	squoze harness <name> [--upstream URL] [--port N] [--log F] [-- CMD...] —
//	   without CMD: print env lines for the harness and serve a proxy;
//	   with CMD: wrap the command, injecting the preset's endpoint envs.
func runHarness(args []string) int {
	if len(args) == 0 || args[0] == "list" || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprintln(os.Stderr, "Supported harnesses (usage: squoze harness <name> [flags] [-- CMD...]):")
		for _, n := range harness.Names() {
			p, _ := harness.Lookup(n)
			fmt.Fprintf(os.Stderr, "  %-12s env: %-60s default: %s\n",
				p.Name, strings.Join(p.BaseURLEnvs, ","), p.DefaultUpstream)
		}
		return 2
	}
	name := args[0]
	fs := flag.NewFlagSet("harness "+name, flag.ExitOnError)
	upstream := fs.String("upstream", "", "provider base URL (default: preset's provider root)")
	port := fs.Int("port", 8787, "proxy port when no CMD is given")
	logFile := fs.String("log", "", "full JSONL request log path")
	originsDir := fs.String("origins-dir", "", "persist squeezed originals")
	listen := fs.String("listen", "", "wrap-mode listen address (default ephemeral)")
	fs.Parse(args[1:])

	p, err := harness.Lookup(name)
	if err != nil {
		fmt.Fprintln(os.Stderr, "squoze harness:", err)
		return 2
	}
	if *upstream == "" {
		*upstream = p.DefaultUpstream
	}
	u, uerr := url.Parse(*upstream)
	if uerr != nil {
		fmt.Fprintf(os.Stderr, "squoze harness: bad upstream: %v\n", uerr)
		return 2
	}

	newHandler := func() (*proxy.Server, func()) {
		var eng *engine.Engine
		if *originsDir != "" {
			orig, oerr := store.OpenOriginals(*originsDir)
			if oerr != nil {
				fmt.Fprintf(os.Stderr, "squoze harness: origins store: %v\n", oerr)
				os.Exit(1)
			}
			eng = engine.NewEngineWith(engine.DefaultMemoCapacity, orig)
		} else {
			eng = engine.NewEngine(engine.DefaultMemoCapacity)
		}
		h := proxy.NewWithEngine(u, eng)
		cleanup := func() {}
		if *logFile != "" {
			f, ferr := os.OpenFile(*logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
			if ferr != nil {
				fmt.Fprintf(os.Stderr, "squoze harness: log file: %v\n", ferr)
				os.Exit(1)
			}
			h.WithLog(f)
			cleanup = func() { _ = f.Close() }
		}
		return h, cleanup
	}

	if fs.NArg() > 0 {
		werr := wrap.Run(context.Background(), wrap.Options{
			Command:     fs.Args(),
			Upstream:    u,
			OriginsDir:  *originsDir,
			ListenAddr:  *listen,
			LogFile:     *logFile,
			BaseURLEnvs: p.BaseURLEnvs,
		})
		if werr != nil {
			fmt.Fprintln(os.Stderr, werr)
			return 1
		}
		return 0
	}

	handler, cleanup := newHandler()
	defer cleanup()
	addr := fmt.Sprintf(":%d", *port)
	fmt.Printf("squoze v%s: proxying %s → %s on %s\n\n", engine.Version, p.Name, u, addr)
	fmt.Println("Point your harness at the proxy (pick your shell):")
	for _, e := range p.EnvFor(fmt.Sprintf("localhost:%d", *port)) {
		k, v, _ := strings.Cut(e, "=")
		fmt.Printf("  bash:            export %s=\"%s\"\n", k, v)
		fmt.Printf("  PowerShell 5.1:  $env:%s = \"%s\"\n", k, v)
	}
	if lerr := http.ListenAndServe(addr, handler); lerr != nil {
		fmt.Fprintln(os.Stderr, lerr)
		return 1
	}
	return 0
}
