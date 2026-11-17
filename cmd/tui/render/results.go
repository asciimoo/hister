// SPDX-FileContributor: 4evy <git@evy.pink>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package render

import (
	"fmt"
	"strings"

	"github.com/asciimoo/nogon/client"
	"github.com/asciimoo/nogon/cmd/tui/model"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func Results(m *model.Model) string {
	documents := m.VisibleDocuments()
	if m.Results == nil || (len(documents) == 0 && len(m.Results.History) == 0) {
		m.LineOffsets = nil
		m.SuggestionHeight = 0
		if m.IsSearching {
			return m.Styles.Gray.Render("  " + m.Spinner.View() + " searching…")
		}
		query := sanitizeTerminalLine(m.TextInput.Value())
		if !m.WsReady {
			return emptyState(m, "Search is offline", "Nogon is reconnecting automatically. Your query will stay here.")
		}
		if m.Results != nil && m.Results.Error != "" {
			detail := sanitizeTerminalLine(m.Results.Error)
			if m.Results.Query != nil && m.Results.Query.Text != m.TextInput.Value() {
				detail += "\nExpanded query: " + sanitizeTerminalLine(m.Results.Query.Text)
			}
			return emptyState(m, "Invalid query", detail)
		}
		if query != "" {
			title := "No results for “" + truncateLine(query, max(8, m.Viewport.Width()-22)) + "”"
			detail := "Try fewer words or check the spelling."
			if m.Results != nil && m.Results.QuerySuggestion != "" {
				suggestion := sanitizeTerminalLine(m.Results.QuerySuggestion)
				detail = "Did you mean “" + truncateLine(suggestion, max(8, m.Viewport.Width()-24)) + "”? Press Enter to try it."
			}
			return emptyState(m, title, detail)
		}
		return emptyState(m, "Search your history", "Type above to search indexed content and past visits.")
	}
	var items []string
	var lineOffsets []int
	currentLine, currentIdx := 0, 0

	w := max(1, m.Viewport.Width()-3)
	contentW := max(1, w-2)
	style := lipgloss.NewStyle().MaxWidth(w)

	histCount := 0
	for _, h := range m.Results.History {
		if currentIdx >= m.Limit {
			break
		}
		lineOffsets = append(lineOffsets, currentLine)
		item := style.Render(HistoryItem(m, h, currentIdx == m.SelectedIdx, contentW))
		items = append(items, item)
		currentLine += lipgloss.Height(item) + 1
		currentIdx++
		histCount++
	}

	if histCount > 0 && len(documents) > 0 && currentIdx < m.Limit {
		div := sectionDivider(m.Styles, w)
		items = append(items, div)
		currentLine += lipgloss.Height(div) + 1
	}

	lastDomain := ""
	for _, d := range documents {
		if currentIdx >= m.Limit {
			break
		}
		// Domain separator when sorting by domain
		if m.SortMode == "domain" && d.Domain != "" && d.Domain != lastDomain {
			// Close previous domain group
			if lastDomain != "" {
				closingDiv := "  " + m.Styles.Div.Render(strings.Repeat("─", max(0, w-2)))
				items = append(items, closingDiv)
				currentLine += lipgloss.Height(closingDiv) + 1
			}
			lastDomain = d.Domain
			domLabel := strings.TrimPrefix(d.Domain, "www.")
			ruleW := max(0, w-lipgloss.Width(domLabel)-3)
			domDiv := "  " + m.Styles.DomainHeader.Render(domLabel) + " " + m.Styles.Div.Render(strings.Repeat("─", ruleW))
			items = append(items, domDiv)
			currentLine += lipgloss.Height(domDiv) + 1
		}
		lineOffsets = append(lineOffsets, currentLine)
		item := style.Render(Document(m, d, currentIdx == m.SelectedIdx, contentW))
		items = append(items, item)
		currentLine += lipgloss.Height(item) + 1
		currentIdx++
	}

	// Close last domain group
	if m.SortMode == "domain" && lastDomain != "" {
		closingDiv := "  " + m.Styles.Div.Render(strings.Repeat("─", max(0, w-2)))
		items = append(items, closingDiv)
		currentLine += lipgloss.Height(closingDiv) + 1
	}

	totalItems := len(m.Results.History) + len(documents)
	if totalItems > m.Limit {
		lineOffsets = append(lineOffsets, currentLine)
		totalAvailable := max(int(m.Results.Total)+len(m.Results.History), totalItems)
		rem := max(0, totalAvailable-m.Limit)
		var content string
		if currentIdx == m.SelectedIdx && resultListFocused(m) {
			content = m.Styles.LoadMoreSelected.Render(fmt.Sprintf("[ ▼ Load 10 more (%d remaining) ]", rem))
		} else {
			content = m.Styles.LoadMore.Render(fmt.Sprintf("[ ▼ Load 10 more (%d remaining) ]", rem))
		}
		var item string
		if currentIdx == m.SelectedIdx {
			item = style.Render(selectedResultStyle(m).Render(content))
		} else {
			item = style.Render(m.Styles.Item.Render(content))
		}
		items = append(items, item)
	}

	output := strings.Join(items, "\n\n")
	if m.Results.QuerySuggestion != "" {
		sugg := "  " + m.Styles.SuggLabel.Render("did you mean: ") + m.Styles.SuggTerm.Render(sanitizeTerminalLine(m.Results.QuerySuggestion))
		suggH := lipgloss.Height(sugg) + 1
		for i := range lineOffsets {
			lineOffsets[i] += suggH
		}
		output = sugg + "\n\n" + output
		m.SuggestionHeight = suggH
	} else {
		m.SuggestionHeight = 0
	}
	m.LineOffsets = lineOffsets
	return output
}

func emptyState(m *model.Model, title, detail string) string {
	return strings.Join([]string{
		"",
		"  " + m.Styles.HelpHeader.Render(title),
		"  " + m.Styles.Gray.Render(detail),
	}, "\n")
}

func resultListFocused(m *model.Model) bool {
	return m.State == model.StateResults ||
		(m.State == model.StateDetails && renderResultsPaneFocused(m))
}

func renderResultsPaneFocused(m *model.Model) bool {
	return DetailsSplit(m) && !m.DetailsFocused
}

func selectedResultStyle(m *model.Model) lipgloss.Style {
	if resultListFocused(m) {
		return m.Styles.SelectedItem
	}
	return m.Styles.SelectedItemBlur
}

func HistoryItem(m *model.Model, h *client.SearchHistoryItem, sel bool, contentW int) string {
	ts := m.Styles.Title
	if sel && resultListFocused(m) {
		ts = m.Styles.SelTitle
	}
	const badgeW = 4

	countRendered := ""
	countW := 0
	if h.Count > 0 {
		countRendered = m.Styles.Count.Render(fmt.Sprintf("×%d", h.Count))
		countW = lipgloss.Width(countRendered) + 1
	}

	titleMaxW := max(1, contentW-badgeW-countW)
	titleRendered := renderTitle(ts, strings.Join(strings.Fields(h.Title), " "), titleMaxW)
	titleLine := m.Styles.Hist.Render("[H] ") + rightPad(titleRendered, contentW-badgeW-countW) +
		strings.Repeat(" ", max(0, countW-lipgloss.Width(countRendered))) + countRendered

	content := titleLine + "\n" + renderURL(m.Styles, h.URL, "", contentW)
	if sel {
		return selectedResultStyle(m).Render(content)
	}
	return m.Styles.Item.Render(content)
}

func Document(m *model.Model, d *client.Document, sel bool, contentW int) string {
	ts := m.Styles.Title
	if sel && resultListFocused(m) {
		ts = m.Styles.SelTitle
	}

	domainBadge := ""
	domainBadgeW := 0
	if d.Domain != "" {
		shortDomain := strings.TrimPrefix(d.Domain, "www.")
		domainBadge = m.Styles.DomainLabel.Render("["+shortDomain+"]") + " "
		domainBadgeW = lipgloss.Width(domainBadge)
	}
	labelBadge := ""
	labelBadgeW := 0
	if d.Label != "" {
		labelBadge = m.Styles.SuggTerm.Render("["+truncateLine(d.Label, 18)+"]") + " "
		labelBadgeW = lipgloss.Width(labelBadge)
	}

	relTime := relativeTime(d.Updated)
	timeRendered := m.Styles.Time.Render(relTime)
	timeW := 0
	if relTime != "" {
		timeW = lipgloss.Width(timeRendered) + 1
	}

	titleMaxW := max(1, contentW-timeW-domainBadgeW-labelBadgeW)
	titleRendered := renderTitle(ts, strings.Join(strings.Fields(d.Title), " "), titleMaxW)
	titleLine := labelBadge + domainBadge + rightPad(titleRendered, contentW-timeW-domainBadgeW-labelBadgeW) +
		strings.Repeat(" ", max(0, timeW-lipgloss.Width(timeRendered))) + timeRendered

	var sb strings.Builder
	sb.WriteString(titleLine)
	sb.WriteString("\n")
	sb.WriteString(renderURL(m.Styles, d.URL, d.Domain, contentW))
	if d.Text != "" && sel {
		snippet := truncateLine(strings.Join(strings.Fields(d.Text), " "), contentW)
		sb.WriteString("\n")
		sb.WriteString(m.Styles.SecText.Render(snippet))
	}
	if sel {
		return selectedResultStyle(m).Render(sb.String())
	}
	return m.Styles.Item.Render(sb.String())
}

// renderTitle reapplies the title style after each search-highlight reset.
// The server's TUI highlighter wraps matches in its own SGR style, whose reset
// would otherwise also cancel the surrounding selected-title color.
func renderTitle(style lipgloss.Style, title string, maxW int) string {
	title = truncateLine(title, maxW)
	// Lip Gloss emits ESC[m, while accepting ESC[0m from other SGR producers
	// costs little and keeps the style boundary well-defined.
	title = strings.ReplaceAll(title, "\x1b[0m", ansi.ResetStyle)
	parts := strings.Split(title, ansi.ResetStyle)
	var rendered strings.Builder
	for _, part := range parts {
		if part != "" {
			rendered.WriteString(style.Render(part))
		}
	}
	return rendered.String()
}

func Scrollbar(m *model.Model) string {
	pct := m.Viewport.ScrollPercent()
	thumbPos := int(pct * float64(m.Viewport.Height()-1))

	thumbChar := m.Styles.Thumb.Render("█")
	trackChar := m.Styles.Track.Render("│")

	var sb strings.Builder
	for i := 0; i < m.Viewport.Height(); i++ {
		if i == thumbPos {
			sb.WriteString(thumbChar)
		} else {
			sb.WriteString(trackChar)
		}
		if i < m.Viewport.Height()-1 {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}
