// SPDX-FileContributor: FlameFlag <github@flameflag.dev>
//
// SPDX-License-Identifier: AGPLv3+

package handle

import (
	"fmt"
	"strings"
	"time"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/ui/model"
	"github.com/asciimoo/hister/ui/network"
	"github.com/asciimoo/hister/ui/render"
	"github.com/asciimoo/hister/ui/theme"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pkg/browser"
)

func DialogKeys(m *model.Model, msg tea.KeyMsg) tea.Cmd {
	var cmd tea.Cmd
	action := config.Action(m.Cfg.Hotkeys.TUI[msg.String()])
	key := msg.String()
	switch {
	case key == "left" || key == "h" || key == "shift+tab":
		m.DialogBtnIdx = 0
	case key == "right" || key == "l" || key == "tab":
		m.DialogBtnIdx = 1
	case action == config.ActionOpenResult:
		if m.DialogBtnIdx == 1 && m.DialogConfirm != nil {
			cmd = m.DialogConfirm()
		}
		m.DialogConfirm = nil
		m.DismissDialog()
	case key == "y":
		if m.DialogConfirm != nil {
			cmd = m.DialogConfirm()
		}
		m.DialogConfirm = nil
		m.DismissDialog()
	case key == "n" || action == config.ActionToggleFocus:
		if key == "esc" || key == "n" {
			m.DialogConfirm = nil
			m.DismissDialog()
		}
	}
	return cmd
}

func previewTheme(m *model.Model) {
	darkNames, lightNames := theme.ClassifyThemes()
	var name string
	if m.ThemePickerSection == 0 && len(darkNames) > 0 {
		name = darkNames[m.DarkThemeIdx]
	} else if m.ThemePickerSection == 1 && len(lightNames) > 0 {
		name = lightNames[m.LightThemeIdx]
	}
	if name != "" {
		if p, ok := theme.GetPalette(name); ok {
			m.ApplyTheme(p)
			render.RefreshViewport(m)
		}
	}
}

func ThemePickerKeys(m *model.Model, msg tea.KeyMsg) tea.Cmd {
	darkNames, lightNames := theme.ClassifyThemes()
	action := config.Action(m.Cfg.Hotkeys.TUI[msg.String()])
	key := msg.String()
	switch action {
	case config.ActionScrollUp:
		if m.ThemePickerSection == 0 {
			if m.DarkThemeIdx > 0 {
				m.DarkThemeIdx--
			}
		} else {
			if m.LightThemeIdx > 0 {
				m.LightThemeIdx--
			}
		}
		previewTheme(m)
	case config.ActionScrollDown:
		if m.ThemePickerSection == 0 {
			if m.DarkThemeIdx < len(darkNames)-1 {
				m.DarkThemeIdx++
			}
		} else {
			if m.LightThemeIdx < len(lightNames)-1 {
				m.LightThemeIdx++
			}
		}
		previewTheme(m)
	case config.ActionToggleFocus:
		if key == "esc" {
			return CloseThemePickerWithRevert(m)
		}
		if key == "tab" {
			if m.ThemePickerSection == 0 {
				m.ThemePickerSection = 1
			} else {
				m.ThemePickerSection = 0
			}
			previewTheme(m)
		}
	case config.ActionToggleTheme:
		switch m.ThemePickerMode {
		case "auto":
			m.ThemePickerMode = "dark"
		case "dark":
			m.ThemePickerMode = "light"
		case "light":
			m.ThemePickerMode = "auto"
		default:
			m.ThemePickerMode = "auto"
		}
		m.Cfg.TUI.ColorScheme = m.ThemePickerMode
		p, _ := theme.ResolvePalette(&m.Cfg.TUI, m.IsDarkBg)
		m.ApplyTheme(p)
		render.RefreshViewport(m)
	case config.ActionOpenResult:
		m.Cfg.TUI.ColorScheme = m.ThemePickerMode
		if len(darkNames) > 0 {
			m.Cfg.TUI.DarkTheme = darkNames[m.DarkThemeIdx]
		}
		if len(lightNames) > 0 {
			m.Cfg.TUI.LightTheme = lightNames[m.LightThemeIdx]
		}
		p, _ := theme.ResolvePalette(&m.Cfg.TUI, m.IsDarkBg)
		m.ApplyTheme(p)
		render.RefreshViewport(m)
		m.State = m.PrevState
		if m.State == model.StateInput {
			return m.TextInput.Focus()
		}
	}
	return nil
}

func SettingsKeys(m *model.Model, msg tea.KeyMsg) tea.Cmd {
	if m.SettingsEditMode {
		return settingsEditKey(m, msg)
	}
	totalItems := len(m.Cfg.Hotkeys.TUI)
	action := config.Action(m.Cfg.Hotkeys.TUI[msg.String()])
	key := msg.String()
	switch action {
	case config.ActionScrollUp:
		model.ScrollIdx(&m.SettingsIdx, -1, 0, totalItems-1)
	case config.ActionScrollDown:
		model.ScrollIdx(&m.SettingsIdx, 1, 0, totalItems-1)
	case config.ActionOpenResult:
		m.SettingsEditMode = true
		m.SettingsEditErr = ""
	case config.ActionToggleFocus:
		if key == "esc" {
			m.State = m.PrevState
			if m.State == model.StateInput {
				return m.TextInput.Focus()
			}
		}
	}
	return nil
}

func settingsEditKey(m *model.Model, msg tea.KeyMsg) tea.Cmd {
	newKey := msg.String()
	if newKey == "esc" {
		items := m.SortedSettingsItems()
		if m.SettingsIdx >= 0 && m.SettingsIdx < len(items) {
			action := items[m.SettingsIdx].Action
			oldKey := items[m.SettingsIdx].Key
			defaults := config.DefaultTUIHotkeys
			defaultKey := ""
			for k, v := range defaults {
				if v == string(action) {
					defaultKey = k
					break
				}
			}
			if defaultKey != "" && defaultKey != oldKey {
				if existingAction, exists := m.Cfg.Hotkeys.TUI[defaultKey]; exists && existingAction != string(action) {
					m.SettingsEditErr = fmt.Sprintf("default %s conflicts with %s", render.FormatKey(defaultKey), existingAction)
					m.SettingsEditMode = false
					return tea.Tick(2*time.Second, func(_ time.Time) tea.Msg { return model.SettingsErrClearMsg{} })
				}
				delete(m.Cfg.Hotkeys.TUI, oldKey)
				m.Cfg.Hotkeys.TUI[defaultKey] = string(action)
			}
		}
		m.SettingsEditMode = false
		return nil
	}
	if newKey == "enter" {
		m.SettingsEditErr = "Cannot bind Enter"
		return tea.Tick(2*time.Second, func(_ time.Time) tea.Msg { return model.SettingsErrClearMsg{} })
	}
	items := m.SortedSettingsItems()
	if m.SettingsIdx < 0 || m.SettingsIdx >= len(items) {
		m.SettingsEditMode = false
		return nil
	}
	oldKey := items[m.SettingsIdx].Key
	action := items[m.SettingsIdx].Action

	if existingAction, exists := m.Cfg.Hotkeys.TUI[newKey]; exists && existingAction != string(action) {
		m.SettingsEditErr = fmt.Sprintf("%s already bound to %s", render.FormatKey(newKey), existingAction)
		return tea.Tick(2*time.Second, func(_ time.Time) tea.Msg { return model.SettingsErrClearMsg{} })
	}

	delete(m.Cfg.Hotkeys.TUI, oldKey)
	m.Cfg.Hotkeys.TUI[newKey] = string(action)
	m.SettingsEditMode = false
	m.Cfg.SaveTUIConfig()
	return nil
}

func ContextMenuKeys(m *model.Model, msg tea.KeyMsg) tea.Cmd {
	action := config.Action(m.Cfg.Hotkeys.TUI[msg.String()])
	key := msg.String()
	switch action {
	case config.ActionScrollUp:
		model.ScrollIdx(&m.MenuSelIdx, -1, 0, model.MenuOptionCount-1)
	case config.ActionScrollDown:
		model.ScrollIdx(&m.MenuSelIdx, 1, 0, model.MenuOptionCount-1)
	case config.ActionOpenResult:
		return executeContextMenuAction(m)
	case config.ActionToggleFocus:
		if key == "esc" {
			m.State = m.PrevState
		}
	}
	return nil
}

func executeContextMenuAction(m *model.Model) tea.Cmd {
	m.State = m.PrevState
	baseURL := m.Cfg.BaseURL("")
	switch m.MenuSelIdx {
	case model.MenuOpen:
		if u := m.GetSelectedURL(); u != "" {
			browser.OpenURL(u)
			return network.PostHistory(baseURL, m.TextInput.Value(), u, m.GetSelectedTitle())
		}
	case model.MenuDelete:
		if u := m.GetSelectedURL(); u != "" {
			m.OpenDeleteDialog("Delete Result", u, -1, func() tea.Cmd {
				return tea.Batch(
					network.DeleteURL(baseURL, u),
					doSearch(m),
				)
			})
		}
	case model.MenuPrioritize:
		if u := m.GetSelectedURL(); u != "" {
			m.PrioritizeURL = u
			m.PrioritizeInput.SetValue("")
			m.PrioritizeInput.Focus()
			m.PrioritizeBtnIdx = 1
			m.State = model.StatePrioritizeInput
		}
	}
	return nil
}

func PrioritizeInputKeys(m *model.Model, msg tea.KeyMsg) tea.Cmd {
	action := config.Action(m.Cfg.Hotkeys.TUI[msg.String()])
	key := msg.String()
	switch {
	case key == "left" || key == "shift+tab":
		m.PrioritizeBtnIdx = 0
		return nil
	case key == "right" || key == "tab":
		m.PrioritizeBtnIdx = 1
		return nil
	case key == "esc" || (action == config.ActionToggleFocus && key == "esc"):
		m.PrioritizeInput.Blur()
		m.State = m.PrevState
		return nil
	case action == config.ActionOpenResult:
		if m.PrioritizeBtnIdx == 0 {
			// Cancel
			m.PrioritizeInput.Blur()
			m.State = m.PrevState
			return nil
		}
		pattern := strings.TrimSpace(m.PrioritizeInput.Value())
		m.PrioritizeInput.Blur()
		m.State = m.PrevState
		if pattern != "" {
			m.RulesData.Priority = append(m.RulesData.Priority, pattern)
			baseURL := m.Cfg.BaseURL("")
			return network.PostRules(baseURL, strings.Join(m.RulesData.Skip, "\n"), strings.Join(m.RulesData.Priority, "\n"))
		}
		return nil
	}
	var cmd tea.Cmd
	m.PrioritizeInput, cmd = m.PrioritizeInput.Update(msg)
	return cmd
}
