package main

import (
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/anish/anthropic-proxy/proxy"
)

func main() {
	proxy.SetupLogger()

	if len(os.Args) > 1 && os.Args[1] == "login" {
		if err := proxy.RunLogin(); err != nil {
			slog.Error("login failed", "err", err)
			os.Exit(1)
		}
		return
	}

	port := flag.Int("port", 8080, "port to listen on")
	logRequests := flag.Bool("log", false, "log requests to the requests/ directory")
	swapCreds := flag.Bool("swap-creds", false, "replace client credentials with logged-in OAuth token")
	flag.Parse()

	addr := fmt.Sprintf(":%d", *port)
	p := proxy.New(proxy.Options{
		LogRequests: *logRequests,
		SwapCreds:   *swapCreds,
	})
	p.WatchPrompts()

	slog.Info("anthropic-proxy listening", "addr", "http://localhost"+addr)
	slog.Info("forwarding to https://api.anthropic.com")
	if *swapCreds {
		slog.Info("credential swap: ENABLED (using logged-in OAuth token)")
	}
	fmt.Println()
	fmt.Println("To use with Claude Code, run:")
	fmt.Printf("  ANTHROPIC_BASE_URL=http://localhost%s claude\n\n", addr)

	if err := http.ListenAndServe(addr, p); err != nil {
		slog.Error("server error", "err", err)
		os.Exit(1)
	}
}
