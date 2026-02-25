// SPDX-FileContributor: FlameFlag <github@flameflag.dev>
//
// SPDX-License-Identifier: AGPLv3+

package render

import (
	"strings"

	"github.com/asciimoo/hister/ui/model"

	"github.com/charmbracelet/lipgloss"
)

func HistoryTab(m *model.Model) string {
	if m.HistoryLoading {
		return m.Styles.Gray.Render("  " + m.Spinner.View() + " loading…")
	}
	if len(m.HistoryItems) == 0 {
		return m.Styles.Gray.Render("  No history items")
	}
	contentW := max(1, m.Width-5)
	var lines []string
	lines = append(lines, "")
	for i, h := range m.HistoryItems {
		queryPart := ""
		if h.Query != "" {
			queryPart = m.Styles.Gray.Render(" [" + truncateLine(h.Query, 20) + "]")
		}
		title := truncateLine(h.Title, contentW-lipgloss.Width(queryPart))
		if title == "" {
			title = truncateLine(h.URL, contentW)
		}
		var row string
		if i == m.HistoryIdx {
			row = m.Styles.SelTitle.Render(title) + queryPart
			row = m.Styles.SelectedItem.Render(row + "\n" + renderURL(m.Styles, h.URL, "", contentW))
		} else {
			row = m.Styles.Title.Render(title) + queryPart
			row = m.Styles.Item.Render(row + "\n" + renderURL(m.Styles, h.URL, "", contentW))
		}
		lines = append(lines, row)
	}
	return strings.Join(lines, "\n\n")
}

func RulesTab(m *model.Model) string {
	if m.RulesLoading {
		return m.Styles.Gray.Render("  " + m.Spinner.View() + " loading…")
	}
	var lines []string
	lines = append(lines, "")

	aliasKeys := m.SortedAliasKeys()

	type sectionDef struct {
		header   string
		items    []string
		inputIdx int // RulesFormFocus value for this section's input
		section  int // section number (0/1/2)
	}

	// Build alias display items
	var aliasItems []string
	for _, k := range aliasKeys {
		aliasItems = append(aliasItems, k+" → "+m.RulesData.Aliases[k])
	}

	sections := []sectionDef{
		{"Skip Patterns", m.RulesData.Skip, 0, 0},
		{"Priority Patterns", m.RulesData.Priority, 1, 1},
		{"Aliases", aliasItems, 2, 2},
	}

	for _, sec := range sections {
		// Section header
		headerStyle := m.Styles.Title
		if sec.section == m.RulesSection && m.RulesFormFocus == model.RulesFieldList {
			headerStyle = m.Styles.SelTitle
		}
		lines = append(lines, headerStyle.Render("  "+sec.header))

		// Form input(s) for this section
		if sec.section == 2 {
			// Alias: two inputs side by side
			kwStyle := m.Styles.Gray
			valStyle := m.Styles.Gray
			if m.RulesFormFocus == model.RulesFieldAliasKey {
				kwStyle = m.Styles.SelTitle
			}
			if m.RulesFormFocus == model.RulesFieldAliasVal {
				valStyle = m.Styles.SelTitle
			}
			btnLabel := " + Add "
			if m.RulesEditingIdx >= 0 && m.RulesEditingSection == 2 {
				btnLabel = " Save "
			}
			lines = append(lines, "  "+kwStyle.Render("Keyword:")+" "+m.RulesAliasKeyInput.View()+"  "+valStyle.Render("Value:")+" "+m.RulesAliasValInput.View()+"  "+m.Styles.CancelBtn.Render(btnLabel))
		} else {
			inputStyle := m.Styles.Gray
			if m.RulesFormFocus == sec.inputIdx {
				inputStyle = m.Styles.SelTitle
			}
			var inp string
			if sec.inputIdx == 0 {
				inp = m.RulesSkipInput.View()
			} else {
				inp = m.RulesPriorityInput.View()
			}
			btnLabel := " + Add "
			if m.RulesEditingIdx >= 0 && m.RulesEditingSection == sec.section {
				btnLabel = " Save "
			}
			lines = append(lines, "  "+inputStyle.Render("Pattern:")+" "+inp+"  "+m.Styles.CancelBtn.Render(btnLabel))
		}

		// List items
		if len(sec.items) == 0 {
			lines = append(lines, m.Styles.Gray.Render("    (none)"))
		}
		for i, item := range sec.items {
			if sec.section == m.RulesSection && i == m.RulesIdx && m.RulesFormFocus == model.RulesFieldList {
				lines = append(lines, m.Styles.SelectedItem.Render("  ▸ "+item))
			} else {
				lines = append(lines, m.Styles.Item.Render("    "+item))
			}
		}
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func AddTab(m *model.Model) string {
	var lines []string
	lines = append(lines, "")
	lines = append(lines, m.Styles.Title.Render("  Add Document"))
	lines = append(lines, "")
	labels := []string{"URL", "Title", "Text"}
	for i, label := range labels {
		style := m.Styles.Gray
		if i == m.AddFocusIdx {
			style = m.Styles.SelTitle
		}
		lines = append(lines, "  "+style.Render(label+":"))
		lines = append(lines, "    "+m.AddInputs[i].View())
		lines = append(lines, "")
	}
	submitStyle := m.Styles.CancelBtn
	if m.AddFocusIdx == 3 {
		submitStyle = m.Styles.CancelBtnSel
	}
	lines = append(lines, "  "+submitStyle.Render(" Submit "))
	if m.AddStatus != "" {
		lines = append(lines, "")
		lines = append(lines, "  "+m.Styles.Conn.Render(m.AddStatus))
	}
	return strings.Join(lines, "\n")
}
