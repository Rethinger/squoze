// Command squoze is the universal, deterministic LLM context optimizer.
//
//	squoze proxy  --port 8787 --upstream https://api.anthropic.com
//	squoze wrap   <agent command>
//	squoze version
package main

import (
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"

	"github.com/Rethinger/squoze/internal/engine"
	"github.com/Rethinger/squoze/internal/proxy"
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
		addr := fmt.Sprintf(":%d", *port)
		fmt.Printf("squoze v%s: proxying :%d → %s\n", engine.Version, *port, u)
		if err := http.ListenAndServe(addr, proxy.New(u)); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "version":
		fmt.Printf("squoze v%s\n", engine.Version)
	case "wrap":
		fmt.Fprintln(os.Stderr, "squoze wrap: not implemented yet (MVP step 7)")
		os.Exit(2)
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
  squoze version                            print version
`)
}
