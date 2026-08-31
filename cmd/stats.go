package cmd

import (
	"fmt"
	"strings"

	"github.com/asciimoo/hister/client"

	"github.com/spf13/cobra"
)

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Report index statistics",
}

var statsDomainsCmd = &cobra.Command{
	Use:   "domains",
	Short: "Report storage used per domain",
	Long: `Report how much storage each domain accounts for, largest first.

Sizes cover the indexed text, the stored HTML, and the stored favicons. Stored
data is content addressed, so a file shared by several domains is charged to
none of them: the figures are what deleting a domain would actually release,
not what it references.

Use it to find what is worth pruning, then remove a domain with:

  hister delete "domain:example.com"`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		limit, _ := cmd.Flags().GetInt("limit")
		bytesOnly, _ := cmd.Flags().GetBool("bytes")

		// No timeout: the report walks every document, and on a large index that is not quick.
		// Failing a report halfway because a default elapsed would be worse than waiting.
		stats, err := newClient(client.WithTimeout(0)).DomainStats()
		if err != nil {
			exit(1, "Failed to fetch domain statistics: "+err.Error())
			return
		}
		if len(stats) == 0 {
			fmt.Println("No indexed documents.")
			return
		}

		shown := stats
		if limit > 0 && limit < len(shown) {
			shown = shown[:limit]
		}

		size := humanBytes
		if bytesOnly {
			size = func(n uint64) string { return fmt.Sprint(n) }
		}

		width := len("DOMAIN")
		for _, s := range shown {
			if len(s.Domain) > width {
				width = len(s.Domain)
			}
		}

		var grandTotal uint64
		for _, s := range stats {
			grandTotal += s.TotalBytes
		}
		// "% OF TOTAL" rather than a second "share" word: SHARED beside it means bytes held in
		// common with other domains, which is a different idea entirely.
		fmt.Printf("%-*s %7s %10s %10s %10s %10s %10s %9s\n",
			width, "DOMAIN", "PAGES", "TEXT", "HTML", "FAVICON", "SHARED", "TOTAL", "% OF TOTAL")
		for _, s := range shown {
			fmt.Printf("%-*s %7d %10s %10s %10s %10s %10s %9s\n",
				width, s.Domain, s.Pages,
				size(s.TextBytes), size(s.HTMLBytes), size(s.FaviconBytes),
				size(s.SharedBytes), size(s.TotalBytes), percentOf(s.TotalBytes, grandTotal))
		}

		// Column totals, not just a grand total. A storage report is read by adding things up, and
		// a footer that leaves most columns blank invites the reader to wonder what is missing.
		var totals client.DomainStat
		for _, s := range stats {
			totals.Pages += s.Pages
			totals.TextBytes += s.TextBytes
			totals.HTMLBytes += s.HTMLBytes
			totals.FaviconBytes += s.FaviconBytes
			totals.TotalBytes += s.TotalBytes
		}
		fmt.Println(strings.Repeat("─", width+78))
		label := fmt.Sprintf("%d domains", len(stats))
		if len(shown) < len(stats) {
			label = fmt.Sprintf("%d of %d domains", len(shown), len(stats))
		}
		fmt.Printf("%-*s %7d %10s %10s %10s %10s %10s %9s\n",
			width, label, totals.Pages,
			size(totals.TextBytes), size(totals.HTMLBytes), size(totals.FaviconBytes),
			"", size(totals.TotalBytes), "100.0%")
		// Deliberately blank above rather than summed: a shared file is referenced by several
		// domains, so adding the column would count the same bytes more than once.
		if anyShared(stats) {
			fmt.Println("\nSHARED is data a domain references but does not own alone — identical " +
				"content stored\nonce. Deleting one of the domains that references it releases " +
				"nothing, so it is\nexcluded from TOTAL.")
		}
	},
}

// humanBytes formats a byte count for reading.
//
// One decimal place from kilobytes upward, deliberately. A storage table whose columns visibly
// fail to add up destroys confidence in the whole report, and rounding to whole units does exactly
// that: 1.53 MB beside two 4 KB figures reads as "2M" and looks like an error when it is not.
func humanBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	value := float64(n)
	units := []string{"KB", "MB", "GB", "TB"}
	for _, u := range units {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, u)
		}
	}
	return fmt.Sprintf("%.1f PB", value/unit)
}

// percentOf renders a domain's portion of the reclaimable total.
//
// Anything above zero but below a tenth of a percent is shown as "<0.1%" rather than "0.0%", which
// would claim a domain occupies nothing at all.
func percentOf(n, total uint64) string {
	if total == 0 {
		return ""
	}
	pct := float64(n) / float64(total) * 100
	if pct > 0 && pct < 0.1 {
		return "<0.1%"
	}
	return fmt.Sprintf("%.1f%%", pct)
}

func anyShared(stats []client.DomainStat) bool {
	for _, s := range stats {
		if s.SharedBytes > 0 {
			return true
		}
	}
	return false
}

func addStatsDomainsFlags(cmd *cobra.Command) {
	cmd.Flags().Int("limit", 0, "show only the largest N domains (0 = all)")
	cmd.Flags().Bool("bytes", false, "print raw byte counts instead of human readable sizes")
}
