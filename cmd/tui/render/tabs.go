// SPDX-FileContributor: 4evy <git@evy.pink>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package render

import (
	"strings"

	"github.com/asciimoo/nogon/cmd/tui/model"

	"charm.land/lipgloss/v2"
)

func HistoryTab(m *model.Model) string {
	m.WorkspaceTargets = nil
	m.WorkspaceSelectionY = 0
	if m.HistoryLoading {
		return m.Styles.Gray.Render("  " + m.Spinner.View() + " loading…")
	}
	if len(m.HistoryItems) == 0 {
		return workspaceEmptyState(m, "No history yet", "Open a search result and it will appear here.")
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
		y := lipgloss.Height(strings.Join(lines, "\n\n")) + 1
		m.WorkspaceTargets = append(m.WorkspaceTargets, model.WorkspaceTarget{
			Y: y, Height: lipgloss.Height(row), Kind: model.WorkspaceHistoryItem, Index: i,
		})
		if i == m.HistoryIdx {
			m.WorkspaceSelectionY = y
		}
		lines = append(lines, row)
	}
	return strings.Join(lines, "\n\n")
}

func RulesTab(m *model.Model) string {
	m.WorkspaceTargets = nil
	m.WorkspaceSelectionY = 0
	if m.RulesLoading {
		return m.Styles.Gray.Render("  " + m.Spinner.View() + " loading…")
	}
	lines := []string{""}
	aliasItems := make([]string, 0, len(m.RulesData.Aliases))
	for _, key := range m.SortedAliasKeys() {
		aliasItems = append(aliasItems, key+" → "+m.RulesData.Aliases[key])
	}
	for _, section := range model.RulesSections {
		items := aliasItems
		if patterns := m.RulesPatterns(section.ID); patterns != nil {
			items = *patterns
		}
		headerStyle := m.Styles.Title
		if section.ID == m.RulesSection && m.RulesFormFocus == model.RulesFocusList {
			headerStyle = m.Styles.SelTitle
		}
		lines = append(lines, headerStyle.Render("  "+section.Title))

		var form string
		if section.Aliases {
			kwStyle := m.Styles.Gray
			valStyle := m.Styles.Gray
			if m.RulesSection == section.ID && m.RulesFormFocus == model.RulesFocusAliasKey {
				kwStyle = m.Styles.SelTitle
			}
			if m.RulesSection == section.ID && m.RulesFormFocus == model.RulesFocusAliasValue {
				valStyle = m.Styles.SelTitle
			}
			btnLabel := " + Add "
			if m.RulesEditingIdx >= 0 && m.RulesEditingSection == section.ID {
				btnLabel = " Save "
			}
			form = "  " + kwStyle.Render("Keyword:") + " " + m.RulesAliasKeyInput.View() + "  " + valStyle.Render("Value:") + " " + m.RulesAliasValInput.View() + "  " + m.Styles.CancelBtn.Render(btnLabel)
		} else {
			inputStyle := m.Styles.Gray
			if m.RulesSection == section.ID && m.RulesFormFocus == model.RulesFocusPattern {
				inputStyle = m.Styles.SelTitle
			}
			btnLabel := " + Add "
			if m.RulesEditingIdx >= 0 && m.RulesEditingSection == section.ID {
				btnLabel = " Save "
			}
			form = "  " + inputStyle.Render("Pattern:") + " " + m.RulesPatternInputs[section.ID].View() + "  " + m.Styles.CancelBtn.Render(btnLabel)
		}
		formY := lipgloss.Height(strings.Join(lines, "\n"))
		m.WorkspaceTargets = append(m.WorkspaceTargets, model.WorkspaceTarget{
			Y: formY, Height: lipgloss.Height(form), Kind: model.WorkspaceRulesForm, Section: section.ID,
		})
		if section.ID == m.RulesSection && m.RulesFormFocus != model.RulesFocusList {
			m.WorkspaceSelectionY = formY
		}
		lines = append(lines, form)

		if len(items) == 0 {
			if section.ID == m.RulesSection && m.RulesFormFocus == model.RulesFocusList {
				m.WorkspaceSelectionY = formY
			}
			lines = append(lines, m.Styles.Gray.Render("    No entries yet — use the form above to add one."))
		}
		for i, item := range items {
			var row string
			if section.ID == m.RulesSection && i == m.RulesIdx && m.RulesFormFocus == model.RulesFocusList {
				row = m.Styles.SelectedItem.Render("  ▸ " + item)
			} else {
				row = m.Styles.Item.Render("    " + item)
			}
			y := lipgloss.Height(strings.Join(lines, "\n"))
			m.WorkspaceTargets = append(m.WorkspaceTargets, model.WorkspaceTarget{
				Y: y, Height: lipgloss.Height(row), Kind: model.WorkspaceRulesItem, Section: section.ID, Index: i,
			})
			if section.ID == m.RulesSection && i == m.RulesIdx && m.RulesFormFocus == model.RulesFocusList {
				m.WorkspaceSelectionY = y
			}
			lines = append(lines, row)
		}
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func AddTab(m *model.Model) string {
	m.WorkspaceTargets = nil
	m.WorkspaceSelectionY = 0
	lines := []string{"", m.Styles.Title.Render("  Add Document"), ""}
	for i, label := range []string{"URL", "Title"} {
		style := m.Styles.Gray
		if i == m.AddFocusIdx {
			style = m.Styles.SelTitle
		}
		y := lipgloss.Height(strings.Join(lines, "\n"))
		labelView := "  " + style.Render(label+":")
		inputView := "    " + m.AddInputs[i].View()
		m.WorkspaceTargets = append(m.WorkspaceTargets, model.WorkspaceTarget{
			Y: y, Height: lipgloss.Height(labelView) + lipgloss.Height(inputView), Kind: model.WorkspaceAddField, Index: i,
		})
		if i == m.AddFocusIdx {
			m.WorkspaceSelectionY = y
		}
		lines = append(lines, labelView, inputView)
		lines = append(lines, "")
	}
	textStyle := m.Styles.Gray
	if m.AddFocusIdx == 2 {
		textStyle = m.Styles.SelTitle
	}
	textY := lipgloss.Height(strings.Join(lines, "\n"))
	textLabel := "  " + textStyle.Render("Text:")
	textInput := "    " + m.AddText.View()
	m.WorkspaceTargets = append(m.WorkspaceTargets, model.WorkspaceTarget{
		Y: textY, Height: lipgloss.Height(textLabel) + lipgloss.Height(textInput), Kind: model.WorkspaceAddField, Index: 2,
	})
	if m.AddFocusIdx == 2 {
		m.WorkspaceSelectionY = textY
	}
	lines = append(lines, textLabel, textInput)
	lines = append(lines, "")
	submitStyle := m.Styles.CancelBtn
	if m.AddFocusIdx == 3 {
		submitStyle = m.Styles.CancelBtnSel
	}
	submitY := lipgloss.Height(strings.Join(lines, "\n"))
	submit := "  " + submitStyle.Render(" Submit ")
	m.WorkspaceTargets = append(m.WorkspaceTargets, model.WorkspaceTarget{
		Y: submitY, Height: lipgloss.Height(submit), Kind: model.WorkspaceAddSubmit, Index: model.AddSubmitFieldIdx,
	})
	if m.AddFocusIdx == model.AddSubmitFieldIdx {
		m.WorkspaceSelectionY = submitY
	}
	lines = append(lines, submit)
	if m.AddStatus != "" {
		lines = append(lines, "")
		lines = append(lines, "  "+renderStatusMessage(m, m.AddStatus, m.AddStatusKind))
	}
	return strings.Join(lines, "\n")
}

func workspaceEmptyState(m *model.Model, title, detail string) string {
	return strings.Join([]string{
		"",
		"  " + m.Styles.HelpHeader.Render(title),
		"  " + m.Styles.Gray.Render(detail),
	}, "\n")
}
