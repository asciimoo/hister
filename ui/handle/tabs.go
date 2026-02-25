// SPDX-FileContributor: FlameFlag <github@flameflag.dev>
//
// SPDX-License-Identifier: AGPLv3+

package handle

import (
	"strings"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/ui/model"
	"github.com/asciimoo/hister/ui/network"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pkg/browser"
)

func TabKeys(m *model.Model, msg tea.KeyMsg) tea.Cmd {
	action := config.Action(m.Cfg.Hotkeys.TUI[msg.String()])
	// Suppress hotkey actions when a text input is focused.
	if msg.Type == tea.KeyRunes && !msg.Alt {
		inputFocused := false
		switch m.ActiveTab {
		case model.TabAdd:
			inputFocused = m.AddFocusIdx >= 0 && m.AddFocusIdx < 3
		case model.TabRules:
			inputFocused = m.RulesFormFocus < model.RulesFieldList // form inputs 0-3
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

var tabKeyHandlers = map[int]func(*model.Model, tea.KeyMsg) tea.Cmd{
	model.TabHistory: HistoryKeys,
	model.TabRules:   RulesKeys,
	model.TabAdd:     AddKeys,
}

func HistoryKeys(m *model.Model, msg tea.KeyMsg) tea.Cmd {
	action := config.Action(m.Cfg.Hotkeys.TUI[msg.String()])
	switch action {
	case config.ActionScrollUp:
		model.ScrollIdx(&m.HistoryIdx, -1, 0, len(m.HistoryItems)-1)
		return m.FlashHint(config.ActionScrollUp)
	case config.ActionScrollDown:
		model.ScrollIdx(&m.HistoryIdx, 1, 0, len(m.HistoryItems)-1)
		return m.FlashHint(config.ActionScrollDown)
	case config.ActionOpenResult:
		if m.HistoryIdx >= 0 && m.HistoryIdx < len(m.HistoryItems) {
			browser.OpenURL(m.HistoryItems[m.HistoryIdx].URL)
		}
		return m.FlashHint(config.ActionOpenResult)
	case config.ActionDeleteResult:
		if m.HistoryIdx >= 0 && m.HistoryIdx < len(m.HistoryItems) {
			h := m.HistoryItems[m.HistoryIdx]
			baseURL := m.Cfg.BaseURL("")
			m.OpenDeleteDialog("Delete History Entry", h.URL, model.TabHistory, func() tea.Cmd {
				return network.DeleteHistoryEntry(baseURL, h.Query, h.URL)
			})
		}
		return m.FlashHint(config.ActionDeleteResult)
	case config.ActionToggleFocus:
		return SwitchTab(m, config.ActionTabSearch)
	}
	return nil
}

func RulesKeys(m *model.Model, msg tea.KeyMsg) tea.Cmd {
	baseURL := m.Cfg.BaseURL("")

	// If a form input is focused (0-3), handle text input
	if m.RulesFormFocus < model.RulesFieldList {
		return rulesFormKeys(m, msg, baseURL)
	}

	// List navigation (RulesFormFocus == 4)
	action := config.Action(m.Cfg.Hotkeys.TUI[msg.String()])
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
		} else if m.RulesSection < 2 {
			m.RulesSection++
			m.RulesIdx = 0
		}
		return m.FlashHint(config.ActionScrollDown)
	case config.ActionToggleFocus:
		// Differentiate between tab (jump to form) and esc (switch tabs)
		if msg.String() == "esc" {
			return SwitchTab(m, config.ActionTabSearch)
		}
		// Jump to the form input for the current section
		switch m.RulesSection {
		case 0:
			m.RulesFormFocus = model.RulesFieldSkip
			m.RulesSkipInput.Focus()
		case 1:
			m.RulesFormFocus = model.RulesFieldPriority
			m.RulesPriorityInput.Focus()
		case 2:
			m.RulesFormFocus = model.RulesFieldAliasKey
			m.RulesAliasKeyInput.Focus()
		}
		m.RulesEditingIdx = -1
		m.RulesEditingSection = m.RulesSection
		return nil
	case config.ActionOpenResult:
		// Edit existing item: populate form input with existing value
		if m.RulesSectionLen(m.RulesSection) > 0 {
			m.RulesEditingIdx = m.RulesIdx
			m.RulesEditingSection = m.RulesSection
			switch m.RulesSection {
			case 0:
				if m.RulesIdx < len(m.RulesData.Skip) {
					m.RulesSkipInput.SetValue(m.RulesData.Skip[m.RulesIdx])
					m.RulesSkipInput.SetCursor(len([]rune(m.RulesSkipInput.Value())))
					m.RulesSkipInput.Focus()
					m.RulesFormFocus = model.RulesFieldSkip
				}
			case 1:
				if m.RulesIdx < len(m.RulesData.Priority) {
					m.RulesPriorityInput.SetValue(m.RulesData.Priority[m.RulesIdx])
					m.RulesPriorityInput.SetCursor(len([]rune(m.RulesPriorityInput.Value())))
					m.RulesPriorityInput.Focus()
					m.RulesFormFocus = model.RulesFieldPriority
				}
			case 2:
				keys := m.SortedAliasKeys()
				if m.RulesIdx < len(keys) {
					m.RulesAliasKeyInput.SetValue(keys[m.RulesIdx])
					m.RulesAliasValInput.SetValue(m.RulesData.Aliases[keys[m.RulesIdx]])
					m.RulesAliasKeyInput.SetCursor(len([]rune(m.RulesAliasKeyInput.Value())))
					m.RulesAliasKeyInput.Focus()
					m.RulesFormFocus = model.RulesFieldAliasKey
				}
			}
		}
		return nil
	case config.ActionDeleteResult:
		if m.RulesSectionLen(m.RulesSection) > 0 {
			section := m.RulesSection
			idx := m.RulesIdx
			var label string
			switch section {
			case 0:
				if idx < len(m.RulesData.Skip) {
					label = m.RulesData.Skip[idx]
				}
			case 1:
				if idx < len(m.RulesData.Priority) {
					label = m.RulesData.Priority[idx]
				}
			case 2:
				keys := m.SortedAliasKeys()
				if idx < len(keys) {
					label = keys[idx]
				}
			}
			if label != "" {
				m.OpenDeleteDialog("Delete Rule", label, model.TabRules, func() tea.Cmd {
					switch section {
					case 0:
						if idx < len(m.RulesData.Skip) {
							m.RulesData.Skip = append(m.RulesData.Skip[:idx], m.RulesData.Skip[idx+1:]...)
							return network.PostRules(baseURL, strings.Join(m.RulesData.Skip, "\n"), strings.Join(m.RulesData.Priority, "\n"))
						}
					case 1:
						if idx < len(m.RulesData.Priority) {
							m.RulesData.Priority = append(m.RulesData.Priority[:idx], m.RulesData.Priority[idx+1:]...)
							return network.PostRules(baseURL, strings.Join(m.RulesData.Skip, "\n"), strings.Join(m.RulesData.Priority, "\n"))
						}
					case 2:
						keys := m.SortedAliasKeys()
						if idx < len(keys) {
							return network.PostDeleteAlias(baseURL, keys[idx])
						}
					}
					return nil
				})
			}
		}
		return m.FlashHint(config.ActionDeleteResult)
	case config.ActionToggleSort:
		m.RulesLoading = true
		return network.FetchRules(baseURL)
	}
	return nil
}

func rulesFormKeys(m *model.Model, msg tea.KeyMsg, baseURL string) tea.Cmd {
	action := config.Action(m.Cfg.Hotkeys.TUI[msg.String()])
	switch action {
	case config.ActionOpenResult:
		var cmd tea.Cmd
		switch m.RulesFormFocus {
		case model.RulesFieldSkip:
			pattern := strings.TrimSpace(m.RulesSkipInput.Value())
			if pattern != "" {
				if m.RulesEditingIdx >= 0 && m.RulesEditingSection == 0 && m.RulesEditingIdx < len(m.RulesData.Skip) {
					m.RulesData.Skip[m.RulesEditingIdx] = pattern
				} else {
					m.RulesData.Skip = append(m.RulesData.Skip, pattern)
				}
				cmd = network.PostRules(baseURL, strings.Join(m.RulesData.Skip, "\n"), strings.Join(m.RulesData.Priority, "\n"))
			}
			m.RulesSkipInput.SetValue("")
		case model.RulesFieldPriority:
			pattern := strings.TrimSpace(m.RulesPriorityInput.Value())
			if pattern != "" {
				if m.RulesEditingIdx >= 0 && m.RulesEditingSection == 1 && m.RulesEditingIdx < len(m.RulesData.Priority) {
					m.RulesData.Priority[m.RulesEditingIdx] = pattern
				} else {
					m.RulesData.Priority = append(m.RulesData.Priority, pattern)
				}
				cmd = network.PostRules(baseURL, strings.Join(m.RulesData.Skip, "\n"), strings.Join(m.RulesData.Priority, "\n"))
			}
			m.RulesPriorityInput.SetValue("")
		case model.RulesFieldAliasKey, model.RulesFieldAliasVal:
			keyword := strings.TrimSpace(m.RulesAliasKeyInput.Value())
			value := strings.TrimSpace(m.RulesAliasValInput.Value())
			if keyword != "" && value != "" {
				if m.RulesEditingIdx >= 0 && m.RulesEditingSection == 2 {
					keys := m.SortedAliasKeys()
					if m.RulesEditingIdx < len(keys) {
						oldKey := keys[m.RulesEditingIdx]
						if oldKey != keyword {
							cmd = tea.Batch(network.PostDeleteAlias(baseURL, oldKey), network.PostAddAlias(baseURL, keyword, value))
						} else {
							cmd = network.PostAddAlias(baseURL, keyword, value)
						}
					}
				} else {
					cmd = network.PostAddAlias(baseURL, keyword, value)
				}
			}
			m.RulesAliasKeyInput.SetValue("")
			m.RulesAliasValInput.SetValue("")
		}
		m.BlurAllRulesInputs()
		m.RulesFormFocus = model.RulesFieldList
		m.RulesEditingIdx = -1
		return cmd

	case config.ActionToggleFocus:
		if msg.String() == "esc" {
			// Cancel editing
			m.BlurAllRulesInputs()
			m.RulesFormFocus = model.RulesFieldList
			m.RulesEditingIdx = -1
			// Clear the input that was being edited
			m.RulesSkipInput.SetValue("")
			m.RulesPriorityInput.SetValue("")
			m.RulesAliasKeyInput.SetValue("")
			m.RulesAliasValInput.SetValue("")
			return nil
		}
		// Cycle through form inputs: skip → priority → alias key → alias val → list
		m.BlurAllRulesInputs()
		next := m.RulesFormFocus + 1
		if next > model.RulesFieldList {
			next = model.RulesFieldSkip
		}
		m.RulesFormFocus = next
		switch next {
		case model.RulesFieldSkip:
			m.RulesSkipInput.Focus()
		case model.RulesFieldPriority:
			m.RulesPriorityInput.Focus()
		case model.RulesFieldAliasKey:
			m.RulesAliasKeyInput.Focus()
		case model.RulesFieldAliasVal:
			m.RulesAliasValInput.Focus()
		case model.RulesFieldList:
			// list mode, nothing to focus
		}
		return nil
	}

	// Pass key to the focused input
	var cmd tea.Cmd
	switch m.RulesFormFocus {
	case model.RulesFieldSkip:
		m.RulesSkipInput, cmd = m.RulesSkipInput.Update(msg)
	case model.RulesFieldPriority:
		m.RulesPriorityInput, cmd = m.RulesPriorityInput.Update(msg)
	case model.RulesFieldAliasKey:
		m.RulesAliasKeyInput, cmd = m.RulesAliasKeyInput.Update(msg)
	case model.RulesFieldAliasVal:
		m.RulesAliasValInput, cmd = m.RulesAliasValInput.Update(msg)
	}
	return cmd
}

func AddKeys(m *model.Model, msg tea.KeyMsg) tea.Cmd {
	baseURL := m.Cfg.BaseURL("")
	action := config.Action(m.Cfg.Hotkeys.TUI[msg.String()])
	switch action {
	case config.ActionToggleFocus:
		if msg.String() == "esc" {
			return SwitchTab(m, config.ActionTabSearch)
		}
		if m.AddFocusIdx < len(m.AddInputs) {
			m.AddInputs[m.AddFocusIdx].Blur()
		}
		m.AddFocusIdx = (m.AddFocusIdx + 1) % 4
		if m.AddFocusIdx < 3 {
			m.AddInputs[m.AddFocusIdx].Focus()
		}
		return m.FlashHint(config.ActionToggleFocus)
	case config.ActionOpenResult:
		if m.AddFocusIdx == 3 || m.AddFocusIdx == 2 {
			u := strings.TrimSpace(m.AddInputs[0].Value())
			if u == "" {
				m.AddStatus = "URL is required"
				return nil
			}
			if !strings.Contains(u, "://") {
				u = "https://" + u
				m.AddInputs[0].SetValue(u)
			}
			title := strings.TrimSpace(m.AddInputs[1].Value())
			text := strings.TrimSpace(m.AddInputs[2].Value())
			m.AddStatus = "Adding..."
			return network.PostAdd(baseURL, u, title, text)
		}
		if m.AddFocusIdx < len(m.AddInputs) {
			m.AddInputs[m.AddFocusIdx].Blur()
		}
		m.AddFocusIdx++
		if m.AddFocusIdx < 3 {
			m.AddInputs[m.AddFocusIdx].Focus()
		}
		return nil
	}
	if m.AddFocusIdx < 3 {
		var cmd tea.Cmd
		m.AddInputs[m.AddFocusIdx], cmd = m.AddInputs[m.AddFocusIdx].Update(msg)
		return cmd
	}
	return nil
}
