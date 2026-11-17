// SPDX-FileContributor: 4evy <git@evy.pink>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package handle

import (
	"strings"

	"github.com/asciimoo/nogon/cmd/tui/model"
	"github.com/asciimoo/nogon/config"

	tea "charm.land/bubbletea/v2"
	"github.com/pkg/browser"
	"github.com/rs/zerolog/log"
)

func TabKeys(m *model.Model, msg tea.KeyPressMsg) tea.Cmd {
	action := m.Keys.Action(msg)
	// Suppress hotkey actions when a text input is focused.
	if len(msg.Text) > 0 && !msg.Mod.Contains(tea.ModAlt) {
		inputFocused := false
		switch m.ActiveTab {
		case model.TabAdd:
			inputFocused = m.AddFocusIdx >= 0 && m.AddFocusIdx < 3
		case model.TabRules:
			inputFocused = m.RulesFormFocus < model.RulesFocusList
		}
		if inputFocused {
			action = ""
		}
	}
	switch action {
	case config.ActionQuit, config.ActionToggleHelp, config.ActionToggleTheme, config.ActionToggleSettings,
		config.ActionTabSearch, config.ActionTabHistory, config.ActionTabRules, config.ActionTabAdd:
		cmd, _ := DispatchCommonAction(m, action)
		return cmd
	}

	if handler, ok := tabKeyHandlers[m.ActiveTab]; ok {
		return handler(m, msg)
	}
	return nil
}

var tabKeyHandlers = map[int]func(*model.Model, tea.KeyPressMsg) tea.Cmd{
	model.TabHistory: HistoryKeys,
	model.TabRules:   RulesKeys,
	model.TabAdd:     AddKeys,
}

func HistoryKeys(m *model.Model, msg tea.KeyPressMsg) tea.Cmd {
	action := m.Keys.Action(msg)
	switch action {
	case config.ActionScrollUp:
		model.ScrollIdx(&m.HistoryIdx, -1, 0, len(m.HistoryItems)-1)
		return m.FlashHint(config.ActionScrollUp)
	case config.ActionScrollDown:
		model.ScrollIdx(&m.HistoryIdx, 1, 0, len(m.HistoryItems)-1)
		return m.FlashHint(config.ActionScrollDown)
	case config.ActionOpenResult:
		if m.HistoryIdx >= 0 && m.HistoryIdx < len(m.HistoryItems) {
			if err := browser.OpenURL(m.HistoryItems[m.HistoryIdx].URL); err != nil {
				log.Warn().Err(err).Msg("failed to open URL in browser")
			}
		}
		return m.FlashHint(config.ActionOpenResult)
	case config.ActionCopyResult:
		return copySelectedURL(m)
	case config.ActionDeleteResult:
		if m.HistoryIdx >= 0 && m.HistoryIdx < len(m.HistoryItems) {
			h := m.HistoryItems[m.HistoryIdx]
			m.OpenDeleteDialog("Delete History Entry", h.URL, model.TabHistory, func() tea.Cmd {
				return m.DeleteHistoryEntryCmd(h.Query, h.URL)
			})
		}
		return m.FlashHint(config.ActionDeleteResult)
	case config.ActionToggleFocus:
		return SwitchTab(m, config.ActionTabSearch)
	}
	return nil
}

func RulesKeys(m *model.Model, msg tea.KeyPressMsg) tea.Cmd {
	if m.RulesFormFocus < model.RulesFocusList {
		return rulesFormKeys(m, msg)
	}

	action := m.Keys.Action(msg)
	switch action {
	case config.ActionScrollUp:
		if m.RulesIdx > 0 {
			m.RulesIdx--
		} else if m.RulesSection > 0 {
			m.RulesSection--
			n := m.RulesSectionLen(m.RulesSection)
			if n > 0 {
				m.RulesIdx = n - 1
			} else {
				m.RulesIdx = 0
			}
		}
		return m.FlashHint(config.ActionScrollUp)
	case config.ActionScrollDown:
		n := m.RulesSectionLen(m.RulesSection)
		if m.RulesIdx < n-1 {
			m.RulesIdx++
		} else if m.RulesSection < len(model.RulesSections)-1 {
			m.RulesSection++
			m.RulesIdx = 0
		}
		return m.FlashHint(config.ActionScrollDown)
	case config.ActionToggleFocus:
		// Differentiate between tab (jump to form) and esc (switch tabs)
		if msg.String() == "esc" {
			return SwitchTab(m, config.ActionTabSearch)
		}
		if m.RulesSection == model.RulesSectionAliases {
			m.RulesFormFocus = model.RulesFocusAliasKey
			m.RulesAliasKeyInput.Focus()
		} else {
			m.RulesFormFocus = model.RulesFocusPattern
			m.RulesPatternInputs[m.RulesSection].Focus()
		}
		m.RulesEditingIdx = -1
		m.RulesEditingSection = m.RulesSection
		return nil
	case config.ActionOpenResult:
		if m.RulesSectionLen(m.RulesSection) > 0 {
			m.RulesEditingIdx = m.RulesIdx
			m.RulesEditingSection = m.RulesSection
			if patterns := m.RulesPatterns(m.RulesSection); patterns != nil {
				if m.RulesIdx < len(*patterns) {
					input := &m.RulesPatternInputs[m.RulesSection]
					input.SetValue((*patterns)[m.RulesIdx])
					input.SetCursor(len([]rune(input.Value())))
					input.Focus()
					m.RulesFormFocus = model.RulesFocusPattern
				}
			} else {
				keys := m.SortedAliasKeys()
				if m.RulesIdx < len(keys) {
					m.RulesAliasKeyInput.SetValue(keys[m.RulesIdx])
					m.RulesAliasValInput.SetValue(m.RulesData.Aliases[keys[m.RulesIdx]])
					m.RulesAliasKeyInput.SetCursor(len([]rune(m.RulesAliasKeyInput.Value())))
					m.RulesAliasKeyInput.Focus()
					m.RulesFormFocus = model.RulesFocusAliasKey
				}
			}
		}
		return nil
	case config.ActionDeleteResult:
		if m.RulesSectionLen(m.RulesSection) > 0 {
			section := m.RulesSection
			idx := m.RulesIdx
			var label string
			if patterns := m.RulesPatterns(section); patterns != nil {
				if idx < len(*patterns) {
					label = (*patterns)[idx]
				}
			} else {
				keys := m.SortedAliasKeys()
				if idx < len(keys) {
					label = keys[idx]
				}
			}
			if label != "" {
				m.OpenDeleteDialog("Delete Rule", label, model.TabRules, func() tea.Cmd {
					if patterns := m.RulesPatterns(section); patterns != nil {
						if idx < len(*patterns) {
							*patterns = append((*patterns)[:idx], (*patterns)[idx+1:]...)
							return m.SaveRulesCmd()
						}
					} else {
						keys := m.SortedAliasKeys()
						if idx < len(keys) {
							return m.DeleteAliasCmd(keys[idx])
						}
					}
					return nil
				})
			}
		}
		return m.FlashHint(config.ActionDeleteResult)
	case config.ActionToggleSort:
		m.RulesLoading = true
		return m.FetchRulesCmd()
	}
	return nil
}

func rulesFormKeys(m *model.Model, msg tea.KeyPressMsg) tea.Cmd {
	action := m.Keys.Action(msg)
	switch action {
	case config.ActionOpenResult:
		var cmd tea.Cmd
		switch m.RulesFormFocus {
		case model.RulesFocusPattern:
			input := &m.RulesPatternInputs[m.RulesSection]
			pattern := strings.TrimSpace(input.Value())
			patterns := m.RulesPatterns(m.RulesSection)
			if pattern != "" {
				if m.RulesEditingIdx >= 0 && m.RulesEditingSection == m.RulesSection && m.RulesEditingIdx < len(*patterns) {
					(*patterns)[m.RulesEditingIdx] = pattern
				} else {
					*patterns = append(*patterns, pattern)
				}
				cmd = m.SaveRulesCmd()
			}
			input.SetValue("")
		case model.RulesFocusAliasKey, model.RulesFocusAliasValue:
			keyword := strings.TrimSpace(m.RulesAliasKeyInput.Value())
			value := strings.TrimSpace(m.RulesAliasValInput.Value())
			if keyword != "" && value != "" {
				if m.RulesEditingIdx >= 0 && m.RulesEditingSection == model.RulesSectionAliases {
					keys := m.SortedAliasKeys()
					if m.RulesEditingIdx < len(keys) {
						oldKey := keys[m.RulesEditingIdx]
						if oldKey != keyword {
							cmd = tea.Batch(m.DeleteAliasCmd(oldKey), m.AddAliasCmd(keyword, value))
						} else {
							cmd = m.AddAliasCmd(keyword, value)
						}
					}
				} else {
					cmd = m.AddAliasCmd(keyword, value)
				}
			}
			m.RulesAliasKeyInput.SetValue("")
			m.RulesAliasValInput.SetValue("")
		}
		m.BlurAllRulesInputs()
		m.RulesFormFocus = model.RulesFocusList
		m.RulesEditingIdx = -1
		return cmd

	case config.ActionToggleFocus:
		if msg.String() == "esc" {
			m.BlurAllRulesInputs()
			m.RulesFormFocus = model.RulesFocusList
			m.RulesEditingIdx = -1
			for i := range m.RulesPatternInputs {
				m.RulesPatternInputs[i].SetValue("")
			}
			m.RulesAliasKeyInput.SetValue("")
			m.RulesAliasValInput.SetValue("")
			return nil
		}
		m.BlurAllRulesInputs()
		if m.RulesSection == model.RulesSectionAliases {
			switch m.RulesFormFocus {
			case model.RulesFocusAliasKey:
				m.RulesFormFocus = model.RulesFocusAliasValue
				m.RulesAliasValInput.Focus()
			default:
				m.RulesFormFocus = model.RulesFocusList
			}
		} else {
			m.RulesFormFocus = model.RulesFocusList
		}
		return nil
	}

	return updateRulesInput(m, msg)
}

func updateRulesInput(m *model.Model, msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	switch m.RulesFormFocus {
	case model.RulesFocusPattern:
		input := &m.RulesPatternInputs[m.RulesSection]
		*input, cmd = input.Update(msg)
	case model.RulesFocusAliasKey:
		m.RulesAliasKeyInput, cmd = m.RulesAliasKeyInput.Update(msg)
	case model.RulesFocusAliasValue:
		m.RulesAliasValInput, cmd = m.RulesAliasValInput.Update(msg)
	}
	return cmd
}

func AddKeys(m *model.Model, msg tea.KeyPressMsg) tea.Cmd {
	// Enter is content inside the multi-line field, not the global submit action.
	if m.AddFocusIdx == 2 && msg.String() == "enter" {
		var cmd tea.Cmd
		m.AddText, cmd = m.AddText.Update(msg)
		return cmd
	}
	action := m.Keys.Action(msg)
	switch action {
	case config.ActionToggleFocus:
		if msg.String() == "esc" {
			return SwitchTab(m, config.ActionTabSearch)
		}
		if m.AddFocusIdx < len(m.AddInputs) {
			m.AddInputs[m.AddFocusIdx].Blur()
		} else if m.AddFocusIdx == 2 {
			m.AddText.Blur()
		}
		m.AddFocusIdx = (m.AddFocusIdx + 1) % 4
		if m.AddFocusIdx < len(m.AddInputs) {
			m.AddInputs[m.AddFocusIdx].Focus()
		} else if m.AddFocusIdx == 2 {
			m.AddText.Focus()
		}
		return m.FlashHint(config.ActionToggleFocus)
	case config.ActionOpenResult:
		if m.AddFocusIdx == 3 {
			return submitAdd(m)
		}
		if m.AddFocusIdx < len(m.AddInputs) {
			m.AddInputs[m.AddFocusIdx].Blur()
		}
		m.AddFocusIdx++
		if m.AddFocusIdx < len(m.AddInputs) {
			m.AddInputs[m.AddFocusIdx].Focus()
		} else if m.AddFocusIdx == 2 {
			m.AddText.Focus()
		}
		return nil
	}
	if m.AddFocusIdx < len(m.AddInputs) {
		return updateAddInput(m, msg)
	}
	if m.AddFocusIdx == 2 {
		return updateAddInput(m, msg)
	}
	return nil
}

func updateAddInput(m *model.Model, msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	if m.AddFocusIdx < len(m.AddInputs) {
		m.AddInputs[m.AddFocusIdx], cmd = m.AddInputs[m.AddFocusIdx].Update(msg)
	} else if m.AddFocusIdx == 2 {
		m.AddText, cmd = m.AddText.Update(msg)
	}
	return cmd
}
