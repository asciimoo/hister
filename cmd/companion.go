package cmd

import (
	"context"
	"errors"
	"os"
	"os/signal"

	qutebrowsercompanion "github.com/asciimoo/nogon/cmd/companion/qutebrowser"

	"github.com/spf13/cobra"
)

var companionCmd = &cobra.Command{
	Use:   "companion",
	Short: "Run browser integration companions",
}

var companionQutebrowserCmd = &cobra.Command{
	Use:   "qutebrowser",
	Short: "Index rendered qutebrowser pages through DevTools",
	Long: `Watch qutebrowser tabs through a local Qt WebEngine DevTools endpoint.

The command submits rendered page content to the configured Nogon server.
Destination settings come from the global --server-url, --token, and
--client-timeout options.`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
		defer stop()

		err := qutebrowsercompanion.Run(
			ctx,
			qutebrowserCompanionOptions(cmd),
			newClient(),
		)
		if err != nil && !errors.Is(err, context.Canceled) {
			exit(1, "Qutebrowser companion error: "+err.Error())
		}
	},
}

func addQutebrowserCompanionFlags(cmd *cobra.Command) {
	defaults := qutebrowsercompanion.DefaultOptions()
	cmd.Flags().String(
		"devtools-url",
		defaults.DevToolsURL,
		"qutebrowser DevTools HTTP endpoint",
	)
	cmd.Flags().String("label", defaults.Label, "label applied to submitted documents")
	cmd.Flags().Duration(
		"initial-delay",
		defaults.InitialDelay,
		"delay before submitting a newly loaded page",
	)
	cmd.Flags().Duration(
		"debounce",
		defaults.Debounce,
		"quiet period before updated page content is submitted",
	)
	cmd.Flags().Duration(
		"max-wait",
		defaults.MaxWait,
		"longest page update burst allowed to postpone submission, zero disables the limit",
	)
	cmd.Flags().Duration(
		"retry-delay",
		defaults.RetryDelay,
		"delay before retrying a failed Nogon submission, zero disables retries",
	)
	cmd.Flags().Duration(
		"reconnect-delay",
		defaults.ReconnectDelay,
		"delay before reconnecting to qutebrowser",
	)
	cmd.Flags().Duration(
		"command-timeout",
		defaults.CommandTimeout,
		"timeout for a DevTools command",
	)
	cmd.Flags().Duration(
		"request-timeout",
		defaults.RequestTimeout,
		"timeout for DevTools discovery and favicon requests",
	)
	cmd.Flags().Int64(
		"max-favicon-bytes",
		defaults.MaxFaviconBytes,
		"largest favicon response accepted",
	)
}

func qutebrowserCompanionOptions(cmd *cobra.Command) qutebrowsercompanion.Options {
	opts := qutebrowsercompanion.DefaultOptions()
	opts.DevToolsURL, _ = cmd.Flags().GetString("devtools-url")
	opts.NogonURL = cfg.BaseURL("")
	opts.Label, _ = cmd.Flags().GetString("label")
	opts.InitialDelay, _ = cmd.Flags().GetDuration("initial-delay")
	opts.Debounce, _ = cmd.Flags().GetDuration("debounce")
	opts.MaxWait, _ = cmd.Flags().GetDuration("max-wait")
	opts.RetryDelay, _ = cmd.Flags().GetDuration("retry-delay")
	opts.ReconnectDelay, _ = cmd.Flags().GetDuration("reconnect-delay")
	opts.CommandTimeout, _ = cmd.Flags().GetDuration("command-timeout")
	opts.RequestTimeout, _ = cmd.Flags().GetDuration("request-timeout")
	opts.MaxFaviconBytes, _ = cmd.Flags().GetInt64("max-favicon-bytes")
	opts.UserAgent = UserAgent
	return opts
}
