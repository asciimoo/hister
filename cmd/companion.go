package cmd

import (
	"context"
	"errors"
	"os"
	"os/signal"

	qutebrowsercompanion "github.com/asciimoo/hister/cmd/companion/qutebrowser"
	safaricompanion "github.com/asciimoo/hister/cmd/companion/safari"
	"github.com/asciimoo/hister/server/crawler"

	"github.com/rs/zerolog/log"
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

The command submits rendered page content to the configured Hister server.
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
		"delay before retrying a failed Hister submission, zero disables retries",
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
	opts.HisterURL = cfg.BaseURL("")
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

var companionSafariCmd = &cobra.Command{
	Use:   "safari",
	Short: "Index newly visited Safari pages by watching its history",
	Long: `Watch Safari's history database and index pages as they are visited.

Safari has no Hister extension, so an index built with "hister import browser safari"
goes stale as soon as the import finishes. This follows the same database and indexes
new visits as they appear.

Only the URL is available, so each page is fetched and indexed the way "hister index"
would fetch it: a page behind a login is indexed as an anonymous visitor sees it.

Reading Safari's history requires Full Disk Access for the terminal or application
running hister. Destination settings come from the global --server-url, --token, and
--client-timeout options.`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
		defer stop()

		// One crawler for the life of the command, matching how "hister index" reuses a single
		// crawler across a URL list so a backend with a persistent connection is not rebuilt
		// for every page.
		cr, err := crawler.New(&cfg.Crawler, nil)
		if err != nil {
			exit(1, "Failed to create crawler: "+err.Error())
			return
		}
		defer func() {
			if err := cr.Close(); err != nil {
				log.Warn().Err(err).Msg("crawler close error")
			}
		}()

		opts := safariCompanionOptions(cmd)
		indexer := &crawlingURLIndexer{crawler: cr, label: opts.Label}
		if err := safaricompanion.Run(ctx, opts, indexer); err != nil &&
			!errors.Is(err, context.Canceled) {
			exit(1, "Safari companion error: "+err.Error())
		}
	},
}

// crawlingURLIndexer fetches and indexes URLs, which is what the Safari companion needs and the
// qutebrowser companion does not: DevTools hands qutebrowser a rendered page, while Safari's
// history yields only an address.
type crawlingURLIndexer struct {
	crawler crawler.Crawler
	label   string
}

func (i *crawlingURLIndexer) IndexURLs(ctx context.Context, urls []string) error {
	c := newClient()
	for _, u := range urls {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Revisiting a page is the common case, so ask before fetching. Without this the
		// companion would re-crawl a site every time it was opened.
		exists, err := c.DocumentExists(u)
		if err != nil {
			log.Warn().Err(err).Str("URL", u).Msg("Failed to check if URL is already indexed")
		} else if exists {
			continue
		}
		if err := indexURL(ctx, i.crawler, u, i.label); err != nil {
			// A page that will not fetch — a private host, a login wall, a 404 — must not stop
			// the ones after it, and must not make the whole pass fail and be retried forever.
			log.Warn().Err(err).Str("URL", u).Msg("Failed to index visited URL")
		}
	}
	return nil
}

func addSafariCompanionFlags(cmd *cobra.Command) {
	defaults := safaricompanion.DefaultOptions()
	cmd.Flags().String("history-path", defaults.HistoryPath, "Safari history database")
	cmd.Flags().String("state-path", defaults.StatePath, "file recording the last visit indexed")
	cmd.Flags().Duration(
		"poll-interval",
		defaults.PollInterval,
		"how often the history database is checked for new visits",
	)
	cmd.Flags().Int("batch-size", defaults.BatchSize, "how many URLs are indexed at a time")
	cmd.Flags().Bool(
		"catch-up",
		defaults.CatchUp,
		"index the whole existing history on first run instead of only new visits",
	)
	cmd.Flags().String("label", defaults.Label, "label applied to indexed documents")
}

func safariCompanionOptions(cmd *cobra.Command) safaricompanion.Options {
	opts := safaricompanion.DefaultOptions()
	opts.HistoryPath, _ = cmd.Flags().GetString("history-path")
	opts.StatePath, _ = cmd.Flags().GetString("state-path")
	opts.PollInterval, _ = cmd.Flags().GetDuration("poll-interval")
	opts.BatchSize, _ = cmd.Flags().GetInt("batch-size")
	opts.CatchUp, _ = cmd.Flags().GetBool("catch-up")
	opts.Label, _ = cmd.Flags().GetString("label")
	return opts
}
