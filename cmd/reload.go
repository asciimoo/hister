package cmd

import (
	"fmt"

	"github.com/asciimoo/nogon/client"
	"github.com/asciimoo/nogon/config"

	"github.com/spf13/cobra"
)

var reloadCmd = &cobra.Command{
	Use:              "reload",
	Short:            "Reload the running server's configuration and rules",
	Long:             "Apply updated settings to the running server, including rules and logging. You can also reload after rotating a log file. Expect a brief interruption and refresh open Nogon web pages afterwards. Requires an admin token in multi user mode.",
	Args:             cobra.NoArgs,
	PersistentPreRun: skipRuntimeInitialization,
	RunE: func(cmd *cobra.Command, _ []string) error {
		cmd.SilenceUsage = true
		c, _, err := inspectConfig()
		if err != nil {
			return err
		}
		var options []client.Option
		if !rootCmd.PersistentFlags().Changed("client-timeout") {
			options = append(options, client.WithTimeout(0))
		}
		if err := newClientForConfig(c, options...).Reload(cmd.Context()); err != nil {
			return fmt.Errorf("reload failed: %w", err)
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), "Server, database, and indexer reloaded.")
		return err
	},
}

// Capture explicit startup flags once. A later reload must not discard them
// or adopt values from the client that requested the reload.
func listenConfigOverrides(cmd *cobra.Command) []config.Override {
	return configFlagOverrides(rootCmd.PersistentFlags(), cmd.Flags())
}
