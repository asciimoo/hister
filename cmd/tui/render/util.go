// SPDX-FileContributor: 4evy <git@evy.pink>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package render

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/asciimoo/nogon/cmd/tui/component"
	"github.com/asciimoo/nogon/cmd/tui/theme"
)

// pads s with spaces on the right to reach exactly width display columns
func rightPad(s string, width int) string {
	pad := max(0, width-lipgloss.Width(s))
	return s + strings.Repeat(" ", pad)
}

// returns a compact human-readable age string for a unix timestamp
func relativeTime(unixTs int64) string {
	if unixTs == 0 {
		return ""
	}
	d := time.Since(time.Unix(unixTs, 0))
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dw", int(d.Hours()/(24*7)))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo", int(d.Hours()/(24*30)))
	default:
		return fmt.Sprintf("%dy", int(d.Hours()/(24*365)))
	}
}

// truncates s to maxW terminal cells without splitting ANSI sequences or
// grapheme clusters, appending "…" if it was cut.
func truncateLine(s string, maxW int) string {
	if maxW <= 0 {
		return ""
	}
	return ansi.Truncate(s, maxW, "…")
}

// renders a URL as "host · /path" where the path is dimmed
func renderURL(st theme.Styles, rawURL, domain string, maxW int) string {
	var host, path string
	u, err := url.Parse(rawURL)
	if err != nil || (u.Host == "" && domain == "") {
		return st.URL.Render(truncateLine(rawURL, maxW))
	}
	if domain != "" {
		host = strings.TrimPrefix(domain, "www.")
	} else {
		host = strings.TrimPrefix(u.Host, "www.")
	}
	if u != nil {
		path = u.Path
		if path == "/" {
			path = ""
		}
		if u.RawQuery != "" {
			path += "?" + u.RawQuery
		}
	}

	hs := st.URL
	if isLocalHost(host) {
		hs = st.URLLocal
	}
	hostPart := hs.Render(host)
	hostW := lipgloss.Width(hostPart)

	if path == "" || hostW >= maxW {
		return hs.Render(truncateLine(host, maxW))
	}

	const sepStr = " · "
	pathMaxW := max(0, maxW-hostW-lipgloss.Width(sepStr))
	return hostPart + st.URLPath.Render(sepStr) + st.URLPath.Render(truncateLine(path, pathMaxW))
}

func isLocalHost(host string) bool {
	h := host
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		h = parsed
	}
	h = strings.Trim(h, "[]")
	addr, err := netip.ParseAddr(h)
	return strings.EqualFold(h, "localhost") || err == nil && addr.IsLoopback()
}

// renders a subtle full-width rule.
func sectionDivider(st theme.Styles, width int) string {
	label := " results "
	ruleW := max(0, width-lipgloss.Width(label)-2)
	return st.Section.Render("  " + label + strings.Repeat("─", ruleW))
}

// returns the first maxCols visible columns of s, preserving ANSI
func truncateAnsi(s string, maxCols int) string {
	if maxCols <= 0 {
		return ""
	}
	truncated := ansi.Truncate(s, maxCols, "")
	return truncated + strings.Repeat(" ", max(0, maxCols-ansi.StringWidth(truncated)))
}

// sanitizeTerminalText removes styling and terminal control sequences from
// untrusted document metadata/content before it enters the renderer. Tabs are
// expanded so they cannot move the cursor outside the layout's cell model.
func sanitizeTerminalText(s string) string {
	s = strings.ReplaceAll(ansi.Strip(s), "\t", "    ")
	return strings.Map(func(r rune) rune {
		if r == '\n' {
			return r
		}
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return -1
		}
		return r
	}, s)
}

func sanitizeTerminalLine(s string) string {
	return strings.Join(strings.Fields(sanitizeTerminalText(s)), " ")
}

func FormatKey(k string) string {
	return component.FormatKey(k)
}
