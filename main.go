package main

import (
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/anish/anthropic-proxy/systemreminders"
	"github.com/anish/anthropic-proxy/proxy"
)

func main() {
	proxy.SetupLogger()

	// No subcommand or flag-like first arg → run the proxy server.
	if len(os.Args) < 2 || os.Args[1][0] == '-' {
		runProxy()
		return
	}

	switch os.Args[1] {
	case "login":
		runLogin()
	case "extract-reminders":
		runExtractReminders()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, "\nUsage: anthropic-proxy [command]\n\n")
	fmt.Fprintf(os.Stderr, "Commands:\n")
	fmt.Fprintf(os.Stderr, "  login               Log in via Anthropic OAuth\n")
	fmt.Fprintf(os.Stderr, "  extract-reminders   Extract system reminders from logged requests\n")
	fmt.Fprintf(os.Stderr, "\nRun without a command to start the proxy server.\n")
	fmt.Fprintf(os.Stderr, "\nProxy flags:\n")
	fmt.Fprintf(os.Stderr, "  -port int       port to listen on (default 8080)\n")
	fmt.Fprintf(os.Stderr, "  -log            log requests to the requests/ directory\n")
	fmt.Fprintf(os.Stderr, "  -swap-creds     replace client credentials with logged-in OAuth token\n")
}

func runProxy() {
	fs := flag.NewFlagSet("proxy", flag.ExitOnError)
	port := fs.Int("port", 8080, "port to listen on")
	logRequests := fs.Bool("log", false, "log requests to the requests/ directory")
	swapCreds := fs.Bool("swap-creds", false, "replace client credentials with logged-in OAuth token")
	fs.Parse(os.Args[1:])

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

func runLogin() {
	fs := flag.NewFlagSet("login", flag.ExitOnError)
	fs.Parse(os.Args[2:])

	if err := proxy.RunLogin(); err != nil {
		slog.Error("login failed", "err", err)
		os.Exit(1)
	}
}

func runExtractReminders() {
	fs := flag.NewFlagSet("extract-reminders", flag.ExitOnError)
	requestsDir := fs.String("requests", "requests", "directory containing logged request files")
	outputDir := fs.String("output", "systemreminder_logs", "directory to write extracted reminders")
	fs.Parse(os.Args[2:])

	if err := systemreminders.Run(*requestsDir, *outputDir); err != nil {
		slog.Error("extract-reminders failed", "err", err)
		os.Exit(1)
	}
}
