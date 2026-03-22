package cmd

import (
	"github.com/anish/anthropic-proxy/proxy"
	"github.com/spf13/cobra"
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Log in via Anthropic OAuth",
	Long:  "Opens a browser for the Anthropic OAuth flow and stores credentials locally.",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return proxy.RunLogin()
	},
}

func init() {
	rootCmd.AddCommand(loginCmd)
}
