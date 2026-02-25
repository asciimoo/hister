// SPDX-FileContributor: FlameFlag <github@flameflag.dev>
//
// SPDX-License-Identifier: AGPLv3+

package render

import (
	"fmt"
	"strings"

	"github.com/asciimoo/hister/server/indexer"
	smodel "github.com/asciimoo/hister/server/model"
	"github.com/asciimoo/hister/ui/model"

	"github.com/charmbracelet/lipgloss"
)

func Results(m *model.Model) string {
	if m.Results == nil || (len(m.Results.Documents) == 0 && len(m.Results.History) == 0) {
		m.LineOffsets, m.TotalLines = nil, 0
		if m.IsSearching {
			return m.Styles.Gray.Render("  " + m.Spinner.View() + " searching…")
		}
		if m.TextInput.Value() != "" {
			return m.Styles.Gray.Render("  No results found")
		}
		return m.Styles.Gray.Render("  " + model.SearchTips[m.TipIdx])
	}
	var items []string
	var lineOffsets []int
	currentLine, currentIdx := 0, 0

	w := max(1, m.Viewport.Width-3)
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

	if histCount > 0 && len(m.Results.Documents) > 0 && currentIdx < m.Limit {
		div := sectionDivider(m.Styles, w)
		items = append(items, div)
		currentLine += lipgloss.Height(div) + 1
	}

	lastDomain := ""
	for _, d := range m.Results.Documents {
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
			ruleW := max(0, w-len([]rune(domLabel))-3)
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

	totalItems := len(m.Results.History) + len(m.Results.Documents)
	if totalItems > m.Limit {
		lineOffsets = append(lineOffsets, currentLine)
		rem := max(0, int(m.Results.Total)+len(m.Results.History)-m.Limit)
		var content string
		if currentIdx == m.SelectedIdx {
			content = m.Styles.LoadMoreSelected.Render(fmt.Sprintf("[ ▼ Load 10 more (%d remaining) ]", rem))
		} else {
			content = m.Styles.LoadMore.Render(fmt.Sprintf("[ ▼ Load 10 more (%d remaining) ]", rem))
		}
		var item string
		if currentIdx == m.SelectedIdx {
			item = style.Render(m.Styles.SelectedItem.Render(content))
		} else {
			item = style.Render(m.Styles.Item.Render(content))
		}
		items = append(items, item)
		currentLine += lipgloss.Height(item) + 1
	}

	output := strings.Join(items, "\n\n")
	if m.Results.QuerySuggestion != "" {
		sugg := "  " + m.Styles.SuggLabel.Render("did you mean: ") + m.Styles.SuggTerm.Render(m.Results.QuerySuggestion)
		suggH := lipgloss.Height(sugg) + 1
		for i := range lineOffsets {
			lineOffsets[i] += suggH
		}
		currentLine += suggH
		output = sugg + "\n\n" + output
		m.SuggestionHeight = suggH
	} else {
		m.SuggestionHeight = 0
	}
	m.LineOffsets, m.TotalLines = lineOffsets, currentLine
	return output
}

func HistoryItem(m *model.Model, h *smodel.URLCount, sel bool, contentW int) string {
	ts := m.Styles.Title
	if sel {
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
	titleRendered := ts.Render(truncateLine(strings.Join(strings.Fields(h.Title), " "), titleMaxW))
	titleLine := m.Styles.Hist.Render("[H] ") + rightPad(titleRendered, contentW-badgeW-countW) +
		strings.Repeat(" ", max(0, countW-lipgloss.Width(countRendered))) + countRendered

	content := titleLine + "\n" + renderURL(m.Styles, h.URL, "", contentW)
	if sel {
		return m.Styles.SelectedItem.Render(content)
	}
	return m.Styles.Item.Render(content)
}

func Document(m *model.Model, d *indexer.Document, sel bool, contentW int) string {
	ts := m.Styles.Title
	if sel {
		ts = m.Styles.SelTitle
	}

	domainBadge := ""
	domainBadgeW := 0
	if d.Domain != "" {
		shortDomain := strings.TrimPrefix(d.Domain, "www.")
		domainBadge = m.Styles.DomainLabel.Render("["+shortDomain+"]") + " "
		domainBadgeW = lipgloss.Width(domainBadge)
	}

	relTime := relativeTime(d.Added)
	timeRendered := m.Styles.Time.Render(relTime)
	timeW := 0
	if relTime != "" {
		timeW = lipgloss.Width(timeRendered) + 1
	}

	titleMaxW := max(1, contentW-timeW-domainBadgeW)
	titleRendered := ts.Render(truncateLine(strings.Join(strings.Fields(d.Title), " "), titleMaxW))
	titleLine := domainBadge + rightPad(titleRendered, contentW-timeW-domainBadgeW) +
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
		return m.Styles.SelectedItem.Render(sb.String())
	}
	return m.Styles.Item.Render(sb.String())
}

func Scrollbar(m *model.Model) string {
	maxScroll := m.TotalLines - m.Viewport.Height
	pct := 0.0
	if maxScroll > 0 {
		pct = float64(m.Viewport.YOffset) / float64(maxScroll)
	}
	pct = max(0, min(1, pct))
	thumbPos := int(pct * float64(m.Viewport.Height-1))

	thumbChar := m.Styles.Thumb.Render("█")
	trackChar := m.Styles.Track.Render("│")

	var sb strings.Builder
	for i := 0; i < m.Viewport.Height; i++ {
		if i == thumbPos {
			sb.WriteString(thumbChar)
		} else {
			sb.WriteString(trackChar)
		}
		if i < m.Viewport.Height-1 {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}
