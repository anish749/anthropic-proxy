package cmd

import (
	"github.com/anish/anthropic-proxy/internal/selfupdate"
	"github.com/spf13/cobra"
)

func newUpdateCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Update anthropic-proxy to the latest version",
		Long: `Update anthropic-proxy to the latest version from GitHub Releases.

Requires either:
  - GitHub CLI (gh) installed and authenticated: gh auth login
  - GITHUB_TOKEN environment variable set

Examples:
  $ anthropic-proxy update
  Updating v0.1.0 → v0.2.0...
  Updated to v0.2.0`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return selfupdate.Update(version)
		},
	}
}
