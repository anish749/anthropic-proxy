package cmd

import (
	"github.com/anish/anthropic-proxy/systemreminders"
	"github.com/spf13/cobra"
)

var systemRemindersCmd = &cobra.Command{
	Use:   "system-reminders",
	Short: "Extract system reminders from logged requests",
	Long: `Scans the requests directory for logged API calls, extracts all unique
<system-reminder> blocks from user messages, and writes each to a separate file.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		requestsDir, _ := cmd.Flags().GetString("requests")
		outputDir, _ := cmd.Flags().GetString("output")
		return systemreminders.Run(requestsDir, outputDir)
	},
}

func init() {
	systemRemindersCmd.Flags().String("requests", "requests", "directory containing logged request files")
	systemRemindersCmd.Flags().String("output", "systemreminder_logs", "directory to write extracted system reminders")
	rootCmd.AddCommand(systemRemindersCmd)
}
