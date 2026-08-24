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

// runAgent implements `squoze agent`:
//
//	squoze agent list
//	squoze agent <name> [--upstream URL] [--port N] [--log F] [-- CMD...]
//
// Env-driven agents (claude-code, codex, gemini-cli, dsh) behave exactly
// like `squoze harness <provider>`; config-file agents (opencode, omp)
// print ready-to-paste snippets and serve the proxy.
func runAgent(args []string) int {
	if len(args) == 0 || args[0] == "list" || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprintln(os.Stderr, "Supported agents (usage: squoze agent <name> [flags] [-- CMD...]):")
		for _, n := range harness.AgentNames() {
			a, _ := harness.LookupAgent(n)
			fmt.Fprintf(os.Stderr, "  %-12s preset: %-9s launch: %s\n", a.Name, a.Preset, a.Launch)
		}
		return 2
	}
	name := args[0]
	fs := flag.NewFlagSet("agent "+name, flag.ExitOnError)
	upstream := fs.String("upstream", "", "provider base URL (default: preset's provider root)")
	port := fs.Int("port", 8787, "proxy port")
	logFile := fs.String("log", "", "full JSONL request log path")
	originsDir := fs.String("origins-dir", "", "persist squeezed originals")
	listen := fs.String("listen", "", "wrap-mode listen address (default ephemeral)")
	auto := fs.Bool("auto", false, "opencode/omp: edit the agent's config file automatically (backup first)")
	unwire := fs.Bool("unwire", false, "opencode/omp: restore the config from the pre-wire backup and exit")
	fs.Parse(args[1:])

	a, aerr := harness.LookupAgent(name)
	if aerr != nil {
		fmt.Fprintln(os.Stderr, "squoze agent:", aerr)
		return 2
	}

	var p *harness.Preset
	if a.Preset != "" {
		p, _ = harness.Lookup(a.Preset)
	} else {
		p = &harness.Preset{Name: a.Name, DefaultUpstream: "https://api.anthropic.com"}
	}
	if *upstream == "" {
		*upstream = p.DefaultUpstream
	}

	// Env-driven agent with an explicit command → wrap (preset envs injected).
	if fs.NArg() > 0 && a.Preset != "" {
		u, uerr := url.Parse(*upstream)
		if uerr != nil {
			fmt.Fprintln(os.Stderr, "squoze agent:", uerr)
			return 2
		}
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

	u, uerr := url.Parse(*upstream)
	if uerr != nil {
		fmt.Fprintln(os.Stderr, "squoze agent:", uerr)
		return 2
	}
	handler, cleanup := buildProxyHandler(*originsDir, *logFile, u)
	defer cleanup()

	localAddr := fmt.Sprintf("localhost:%d", *port)
	fmt.Printf("squoze v%s: proxy for %s → %s on %s\n\n", engine.Version, a.Name, u, localAddr)

	// Unwire exits immediately: restore the backup, never start a server.
	if *unwire {
		if a.Kind == "opencode" {
			path, restored, err := harness.UnwireOpenCode(homeDir())
			fmt.Printf("unwire %s: restored=%v err=%v\n", path, restored, err)
			return 0
		}
		if a.Kind == "omp" {
			path, restored, err := harness.UnwireOMP(homeDir())
			fmt.Printf("unwire %s: restored=%v err=%v\n", path, restored, err)
			return 0
		}
		fmt.Printf("agent %s is env-driven: nothing to unwire (just unset its base-URL variable)\n", a.Name)
		return 0
	}

	switch a.Kind {
	case "opencode":
		if *auto {
			path, changed, werr := harness.WireOpenCode(homeDir(), "anthropic", localAddr)
			if werr != nil {
				fmt.Fprintf(os.Stderr, "auto-wire failed (%s):\n%v\nFalling back to manual snippet.\n", path, werr)
			} else {
				fmt.Printf("config wired automatically: %s (changed=%v; backup at %s.squoze-bak)\n",
					path, changed, path)
				break
			}
		}
		fmt.Println("Add to ~/.config/opencode/opencode.json (override your provider):")
		fmt.Println(harness.OpenCodeSnippet("anthropic", localAddr))
		fmt.Println("\nThen just start opencode as usual. Auth headers pass through squoze untouched.")
	case "omp":
		if *auto {
			path, changed, werr := harness.WireOMP(homeDir(), "anthropic", localAddr, "ANTHROPIC_API_KEY")
			if werr != nil {
				fmt.Fprintf(os.Stderr, "auto-wire failed (%s):\n%v\nFalling back to manual snippet.\n", path, werr)
			} else {
				fmt.Printf("models.yml wired automatically: %s (changed=%v; backup at %s.squoze-bak)\n",
					path, changed, path)
				break
			}
		}
		fmt.Println("Add to ~/.omp/agent/models.yml (route catalog provider through squoze):")
		fmt.Println(harness.OMPSnippet("anthropic", localAddr, "ANTHROPIC_API_KEY"))
		fmt.Println("\nVerify with `omp models anthropic`, then start omp as usual.")
	default:
		for _, e := range p.EnvFor(localAddr) {
			k, v, _ := strings.Cut(e, "=")
			fmt.Printf("  bash:            export %s=\"%s\"\n", k, v)
			fmt.Printf("  PowerShell 5.1:  $env:%s = \"%s\"\n", k, v)
		}
		fmt.Printf("\nOr wrap it in one line: squoze agent %s -- %s\n", a.Name, a.Launch)
	}

	if lerr := http.ListenAndServe(fmt.Sprintf(":%d", *port), handler); lerr != nil {
		fmt.Fprintln(os.Stderr, lerr)
		return 1
	}
	return 0
}

func homeDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "squoze: cannot resolve home dir:", err)
		os.Exit(1)
	}
	return h
}

func buildProxyHandler(originsDir, logFile string, u *url.URL) (*proxy.Server, func()) {
	var eng *engine.Engine
	if originsDir != "" {
		orig, oerr := store.OpenOriginals(originsDir)
		if oerr != nil {
			fmt.Fprintf(os.Stderr, "squoze: origins store: %v\n", oerr)
			os.Exit(1)
		}
		eng = engine.NewEngineWith(engine.DefaultMemoCapacity, orig)
	} else {
		eng = engine.NewEngine(engine.DefaultMemoCapacity)
	}
	h := proxy.NewWithEngine(u, eng)
	cleanup := func() {}
	if logFile != "" {
		f, ferr := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if ferr != nil {
			fmt.Fprintf(os.Stderr, "squoze: log file: %v\n", ferr)
			os.Exit(1)
		}
		h.WithLog(f)
		cleanup = func() { _ = f.Close() }
	}
	return h, cleanup
}
