package cmd

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/anish/anthropic-proxy/internal/selfupdate"
	"github.com/anish/anthropic-proxy/proxy"
	"github.com/spf13/cobra"
)

var (
	port      int
	logReqs   bool
	swapCreds bool
	logFormat string
)

func newRootCmd(version string) *cobra.Command {
	root := &cobra.Command{
		Use:   "anthropic-proxy [flags]",
		Short: "An HTTP proxy for the Anthropic API",
		Long: `anthropic-proxy sits between Claude Code and the Anthropic API,
enabling request logging, prompt rewriting, and credential swapping.

Run without a subcommand to start the proxy server.`,
		Version:       version,
		RunE:          runServe,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			proxy.SetupLogger()
			selfupdate.AutoCheck(version)
		},
	}

	root.Flags().IntVar(&port, "port", 8080, "port to listen on")
	root.Flags().BoolVar(&logReqs, "log", false, "log requests to the requests/ directory")
	root.Flags().BoolVar(&swapCreds, "swap-creds", false, "replace client credentials with logged-in OAuth token")
	root.Flags().StringVar(&logFormat, "log-format", "yaml", "output format for logged requests (json, yaml)")

	root.AddCommand(
		newLoginCmd(),
		newVersionCmd(version),
		newUpdateCmd(version),
	)

	return root
}

// Execute runs the root command.
func Execute(version string) {
	if err := newRootCmd(version).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runServe(cmd *cobra.Command, args []string) error {
	fmt.Printf("anthropic-proxy %s\n", cmd.Version)

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
