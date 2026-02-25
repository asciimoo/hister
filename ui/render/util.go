// SPDX-FileContributor: FlameFlag <github@flameflag.dev>
//
// SPDX-License-Identifier: AGPLv3+

package render

import (
	"fmt"
	"net/url"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/ui/theme"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
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

// truncates s to maxW runes, appending "…" if it was cut
func truncateLine(s string, maxW int) string {
	if maxW <= 1 {
		return ""
	}
	r := []rune(s)
	if len(r) <= maxW {
		return s
	}
	return string(r[:maxW-1]) + "…"
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
	pathMaxW := max(0, maxW-hostW-len([]rune(sepStr)))
	return hostPart + st.URLPath.Render(sepStr) + st.URLPath.Render(truncateLine(path, pathMaxW))
}

func isLocalHost(host string) bool {
	h := strings.SplitN(host, ":", 2)[0]
	return h == "localhost" || h == "127.0.0.1" || h == "::1"
}

// renders a subtle full-width rule.
func sectionDivider(st theme.Styles, width int) string {
	label := " results "
	ruleW := max(0, width-len([]rune(label))-2)
	return st.Section.Render("  " + label + strings.Repeat("─", ruleW))
}

// returns the first maxCols visible columns of s, preserving ANSI
func truncateAnsi(s string, maxCols int) string {
	if maxCols <= 0 {
		return ""
	}
	var sb strings.Builder
	col := 0
	i := 0
	for i < len(s) && col < maxCols {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && s[j] >= 0x30 && s[j] <= 0x3F {
				j++
			}
			for j < len(s) && s[j] >= 0x20 && s[j] <= 0x2F {
				j++
			}
			if j < len(s) {
				j++
			}
			sb.WriteString(s[i:j])
			i = j
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		w := runewidth.RuneWidth(r)
		if col+w > maxCols {
			break
		}
		sb.WriteRune(r)
		col += w
		i += size
	}
	for col < maxCols {
		sb.WriteByte(' ')
		col++
	}
	return sb.String()
}

// returns everything after the first skipCols visible columns of s.
func sliceAnsiFrom(s string, skipCols int) string {
	if skipCols <= 0 {
		return s
	}
	col := 0
	i := 0
	var lastSeq string
	for i < len(s) && col < skipCols {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && s[j] >= 0x30 && s[j] <= 0x3F {
				j++
			}
			for j < len(s) && s[j] >= 0x20 && s[j] <= 0x2F {
				j++
			}
			if j < len(s) {
				j++
			}
			lastSeq = s[i:j]
			i = j
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		w := runewidth.RuneWidth(r)
		col += w
		i += size
	}
	remainder := s[i:]
	if lastSeq != "" && remainder != "" {
		return lastSeq + remainder
	}
	return remainder
}

// returns the shortest key bound to the given action, formatted for display.
func BestKey(hotkeys map[string]string, action config.Action) string {
	best := ""
	for k, v := range hotkeys {
		if v == string(action) && (best == "" || len(k) < len(best)) {
			best = k
		}
	}
	return FormatKey(best)
}

func FormatKey(k string) string {
	switch k {
	case "up":
		return "↑"
	case "down":
		return "↓"
	case "enter":
		return "↵"
	case "esc":
		return "⎋"
	case "tab":
		return "⇥"
	case "":
		return ""
	}
	if runtime.GOOS == "darwin" {
		k = strings.ReplaceAll(k, "ctrl+", "⌃")
		k = strings.ReplaceAll(k, "alt+", "⌥")
	} else {
		k = strings.ReplaceAll(k, "ctrl+", "^")
		k = strings.ReplaceAll(k, "alt+", "Alt+")
	}
	return k
}
