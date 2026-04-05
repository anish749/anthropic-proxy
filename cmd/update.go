package cmd

import (
	"github.com/anish/anthropic-proxy/internal/selfupdate"
	"github.com/spf13/cobra"
)

func newUpdateCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Update anthropic-proxy to the latest version",
		Long:  "Update anthropic-proxy to the latest version from GitHub Releases.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return selfupdate.Update(version)
		},
	}
}
