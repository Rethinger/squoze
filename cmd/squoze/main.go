// Command squoze is the universal, deterministic LLM context optimizer.
//
//	squoze proxy  --port 8787 --upstream https://api.anthropic.com
//	squoze wrap   --upstream URL <agent command>
//	squoze version
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"github.com/Rethinger/squoze/internal/engine"
	"github.com/Rethinger/squoze/internal/proxy"
	"github.com/Rethinger/squoze/internal/store"
	"github.com/Rethinger/squoze/internal/wrap"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "proxy":
		fs := flag.NewFlagSet("proxy", flag.ExitOnError)
		port := fs.Int("port", 8787, "listen port")
		upstream := fs.String("upstream", "", "upstream base URL (required)")
		originsDir := fs.String("origins-dir", "", "persist squeezed originals here (default: memory only)")
		fs.Parse(os.Args[2:])
		if *upstream == "" {
			fmt.Fprintln(os.Stderr, "squoze proxy: --upstream is required")
			os.Exit(2)
		}
		u, err := url.Parse(*upstream)
		if err != nil {
			fmt.Fprintf(os.Stderr, "squoze proxy: bad --upstream: %v\n", err)
			os.Exit(2)
		}
		var handler http.Handler
		if *originsDir != "" {
			orig, oerr := store.OpenOriginals(*originsDir)
			if oerr != nil {
				fmt.Fprintf(os.Stderr, "squoze proxy: origins store: %v\n", oerr)
				os.Exit(1)
			}
			handler = proxy.NewWithEngine(u, engine.NewEngineWith(engine.DefaultMemoCapacity, orig))
		} else {
			handler = proxy.New(u)
		}
		addr := fmt.Sprintf(":%d", *port)
		fmt.Printf("squoze v%s: proxying :%d → %s\n", engine.Version, *port, u)
		if err := http.ListenAndServe(addr, handler); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "wrap":
		fs := flag.NewFlagSet("wrap", flag.ExitOnError)
		upstream := fs.String("upstream", "", "upstream base URL (required)")
		originsDir := fs.String("origins-dir", "", "persist squeezed originals here (default: memory only)")
		addr := fs.String("listen", "", "proxy listen address (default 127.0.0.1 ephemeral)")
		fs.Parse(os.Args[2:])
		if *upstream == "" || fs.NArg() == 0 {
			fmt.Fprintln(os.Stderr, "usage: squoze wrap --upstream URL CMD [args...]")
			os.Exit(2)
		}
		u, err := url.Parse(*upstream)
		if err != nil {
			fmt.Fprintf(os.Stderr, "squoze wrap: bad --upstream: %v\n", err)
			os.Exit(2)
		}
		werr := wrap.Run(context.Background(), wrap.Options{
			Command:    fs.Args(),
			Upstream:   u,
			OriginsDir: *originsDir,
			ListenAddr: *addr,
		})
		if werr != nil {
			fmt.Fprintln(os.Stderr, werr)
			os.Exit(1)
		}
	case "version":
		fmt.Printf("squoze v%s\n", engine.Version)
	case "livecheck":
		os.Exit(runLivecheck(os.Args[2:]))
	case "retrieve":
		fs := flag.NewFlagSet("retrieve", flag.ExitOnError)
		home := fs.String("home", "", "squoze data dir (default: OS config dir /squoze)")
		fs.Parse(os.Args[2:])
		if fs.NArg() != 1 {
			fmt.Fprintln(os.Stderr, "usage: squoze retrieve <ref> [--home DIR]")
			os.Exit(2)
		}
		dir := *home
		if dir == "" {
			if cfg, err := os.UserConfigDir(); err == nil {
				dir = filepath.Join(cfg, "squoze")
			}
		}
		orig, err := store.OpenOriginals(dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "squoze retrieve: %v\n", err)
			os.Exit(1)
		}
		text, rerr := orig.Resolve(fs.Arg(0))
		if rerr != nil {
			fmt.Fprintf(os.Stderr, "squoze retrieve: %v\n", rerr)
			os.Exit(1)
		}
		os.Stdout.WriteString(text)
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `squoze — your context, squoze.

Usage:
  squoze proxy --port 8787 --upstream URL   optimize requests to an LLM provider
  squoze wrap CMD                           run an agent through squoze
  squoze retrieve <ref> [--home DIR]        resolve a marker ref to the original text
  squoze version                            print version
`)
}
