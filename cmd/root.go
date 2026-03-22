package cmd

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/anish/anthropic-proxy/proxy"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "anthropic-proxy [flags]",
	Short: "An HTTP proxy for the Anthropic API",
	Long: `anthropic-proxy sits between Claude Code and the Anthropic API,
enabling request logging, prompt rewriting, and credential swapping.

Run without a subcommand to start the proxy server.`,
	RunE:          runServe,
	SilenceUsage:  true,
	SilenceErrors: true,
}

var (
	port      int
	logReqs   bool
	swapCreds bool
	logFormat string
)

func init() {
	rootCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		proxy.SetupLogger()
	}

	rootCmd.Flags().IntVar(&port, "port", 8080, "port to listen on")
	rootCmd.Flags().BoolVar(&logReqs, "log", false, "log requests to the requests/ directory")
	rootCmd.Flags().BoolVar(&swapCreds, "swap-creds", false, "replace client credentials with logged-in OAuth token")
	rootCmd.Flags().StringVar(&logFormat, "log-format", "yaml", "output format for logged requests (json, yaml)")
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runServe(cmd *cobra.Command, args []string) error {
	addr := fmt.Sprintf(":%d", port)
	p := proxy.New(proxy.Options{
		LogRequests: logReqs,
		SwapCreds:   swapCreds,
		LogFormat:   logFormat,
	})
	p.WatchPrompts()

	slog.Info("anthropic-proxy listening", "addr", "http://localhost"+addr)
	slog.Info("forwarding to https://api.anthropic.com")
	if swapCreds {
		slog.Info("credential swap: ENABLED (using logged-in OAuth token)")
	}
	fmt.Println()
	fmt.Println("To use with Claude Code, run:")
	fmt.Printf("  ANTHROPIC_BASE_URL=http://localhost%s claude\n\n", addr)

	return http.ListenAndServe(addr, p)
}
