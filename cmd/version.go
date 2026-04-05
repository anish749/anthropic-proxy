package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version of anthropic-proxy",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("anthropic-proxy %s\n", version)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
