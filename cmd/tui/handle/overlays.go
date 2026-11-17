// SPDX-FileContributor: 4evy <git@evy.pink>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package handle

import (
	"fmt"
	"strings"
	"time"

	"github.com/asciimoo/nogon/cmd/tui/component"
	"github.com/asciimoo/nogon/cmd/tui/model"
	"github.com/asciimoo/nogon/cmd/tui/render"
	"github.com/asciimoo/nogon/cmd/tui/theme"
	"github.com/asciimoo/nogon/config"

	tea "charm.land/bubbletea/v2"
	"github.com/pkg/browser"
	"github.com/rs/zerolog/log"
)

func DialogKeys(m *model.Model, msg tea.KeyPressMsg) tea.Cmd {
	var cmd tea.Cmd
	action := m.Keys.Action(msg)
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

func leaveTerminalModeForThemeList(m *model.Model) {
	if m.ThemePickerMode != theme.TerminalName {
		return
	}
	if m.ThemePickerSection == 0 {
		m.ThemePickerMode = "dark"
	} else {
		m.ThemePickerMode = "light"
	}
	m.Cfg.TUI.ColorScheme = m.ThemePickerMode
}

func ThemePickerKeys(m *model.Model, msg tea.KeyPressMsg) tea.Cmd {
	darkNames, lightNames := theme.ClassifyThemes()
	action := m.Keys.Action(msg)
	key := msg.String()
	switch action {
	case config.ActionScrollUp:
		leaveTerminalModeForThemeList(m)
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
		leaveTerminalModeForThemeList(m)
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
			if m.ThemePickerMode != theme.TerminalName {
				previewTheme(m)
			}
		}
	case config.ActionToggleTheme:
		m.ThemePickerMode = theme.NextColorSchemeMode(m.ThemePickerMode)
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
		if err := m.Cfg.SaveTUIConfig(); err != nil {
			log.Warn().Err(err).Msg("failed to save TUI theme")
		}
		m.DismissOverlay()
		if m.State == model.StateInput {
			return m.TextInput.Focus()
		}
	}
	return nil
}

func SettingsKeys(m *model.Model, msg tea.KeyPressMsg) tea.Cmd {
	if m.SettingsEditMode {
		return settingsEditKey(m, msg)
	}
	totalItems := len(m.Cfg.Hotkeys.TUI) + 1
	action := m.Keys.Action(msg)
	key := msg.String()
	switch action {
	case config.ActionScrollUp:
		model.ScrollIdx(&m.SettingsIdx, -1, 0, totalItems-1)
	case config.ActionScrollDown:
		model.ScrollIdx(&m.SettingsIdx, 1, 0, totalItems-1)
	case config.ActionOpenResult:
		if m.SettingsIdx == 0 {
			return cycleAppearanceMode(m)
		}
		m.SettingsEditMode = true
		m.SettingsEditErr = ""
	case config.ActionToggleTheme:
		m.OpenThemePicker()
	case config.ActionToggleFocus:
		if key == "esc" {
			m.DismissOverlay()
			if m.State == model.StateInput {
				return m.TextInput.Focus()
			}
		}
	}
	return nil
}

func cycleAppearanceMode(m *model.Model) tea.Cmd {
	mode := m.Cfg.TUI.ColorScheme
	if mode == "" {
		mode = theme.TerminalName
	}
	m.Cfg.TUI.ColorScheme = theme.NextColorSchemeMode(mode)
	m.ThemePickerMode = m.Cfg.TUI.ColorScheme
	p, _ := theme.ResolvePalette(&m.Cfg.TUI, m.IsDarkBg)
	m.ApplyTheme(p)
	render.RefreshViewport(m)
	if err := m.Cfg.SaveTUIConfig(); err != nil {
		log.Warn().Err(err).Msg("failed to save TUI appearance")
		m.SettingsEditErr = "Could not save appearance"
	}
	return nil
}

func settingsEditKey(m *model.Model, msg tea.KeyPressMsg) tea.Cmd {
	newKey := msg.String()
	if newKey == "esc" {
		items := m.SortedSettingsItems()
		itemIdx := m.SettingsIdx - 1
		if itemIdx >= 0 && itemIdx < len(items) {
			action := items[itemIdx].Action
			oldKey := items[itemIdx].Key
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
					m.SettingsEditErr = fmt.Sprintf("default %s conflicts with %s", component.FormatKey(defaultKey), existingAction)
					m.SettingsEditMode = false
					return tea.Tick(2*time.Second, func(_ time.Time) tea.Msg { return model.SettingsErrClearMsg{} })
				}
				delete(m.Cfg.Hotkeys.TUI, oldKey)
				m.Cfg.Hotkeys.TUI[defaultKey] = string(action)
				m.Keys.Rebuild(m.Cfg.Hotkeys.TUI)
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
	itemIdx := m.SettingsIdx - 1
	if itemIdx < 0 || itemIdx >= len(items) {
		m.SettingsEditMode = false
		return nil
	}
	oldKey := items[itemIdx].Key
	action := items[itemIdx].Action

	if existingAction, exists := m.Cfg.Hotkeys.TUI[newKey]; exists && existingAction != string(action) {
		m.SettingsEditErr = fmt.Sprintf("%s already bound to %s", component.FormatKey(newKey), existingAction)
		return tea.Tick(2*time.Second, func(_ time.Time) tea.Msg { return model.SettingsErrClearMsg{} })
	}

	delete(m.Cfg.Hotkeys.TUI, oldKey)
	m.Cfg.Hotkeys.TUI[newKey] = string(action)
	m.Keys.Rebuild(m.Cfg.Hotkeys.TUI)
	m.SettingsEditMode = false
	if err := m.Cfg.SaveTUIConfig(); err != nil {
		log.Warn().Err(err).Msg("failed to save TUI config")
	}
	return nil
}

func ContextMenuKeys(m *model.Model, msg tea.KeyPressMsg) tea.Cmd {
	action := m.Keys.Action(msg)
	key := msg.String()
	switch action {
	case config.ActionScrollUp:
		model.ScrollIdx(&m.MenuSelIdx, -1, 0, len(model.MenuOptions)-1)
	case config.ActionScrollDown:
		model.ScrollIdx(&m.MenuSelIdx, 1, 0, len(model.MenuOptions)-1)
	case config.ActionOpenResult:
		return executeContextMenuAction(m)
	case config.ActionToggleFocus:
		if key == "esc" {
			m.DismissOverlay()
		}
	}
	return nil
}

func executeContextMenuAction(m *model.Model) tea.Cmd {
	m.DismissOverlay()
	option, ok := model.MenuOptionAt(m.MenuSelIdx)
	if !ok {
		return nil
	}
	switch option.ID {
	case model.MenuOpen:
		if u := m.GetSelectedURL(); u != "" {
			if err := browser.OpenURL(u); err != nil {
				log.Warn().Err(err).Msg("failed to open URL in browser")
			}
			return m.PostHistoryCmd(u)
		}
	case model.MenuCopy:
		return copySelectedURL(m)
	case model.MenuDetails:
		return OpenDetails(m)
	case model.MenuLabel:
		return OpenLabelEditor(m)
	case model.MenuDelete:
		if u := m.GetSelectedURL(); u != "" {
			m.OpenDeleteDialog("Delete Result", u, -1, func() tea.Cmd {
				return m.DeleteURLCmd(u)
			})
		}
	case model.MenuPrioritize:
		if u := m.GetSelectedURL(); u != "" {
			m.PrioritizeURL = u
			m.PrioritizeInput.SetValue("")
			m.PrioritizeInput.Focus()
			m.PrioritizeBtnIdx = 1
			m.OpenOverlay(model.StatePrioritizeInput)
		}
	}
	return nil
}

func PrioritizeInputKeys(m *model.Model, msg tea.KeyPressMsg) tea.Cmd {
	action := m.Keys.Action(msg)
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
		m.DismissOverlay()
		return nil
	case action == config.ActionOpenResult:
		if m.PrioritizeBtnIdx == 0 {
			// Cancel
			m.PrioritizeInput.Blur()
			m.DismissOverlay()
			return nil
		}
		pattern := strings.TrimSpace(m.PrioritizeInput.Value())
		m.PrioritizeInput.Blur()
		m.DismissOverlay()
		if pattern != "" {
			return m.PrioritizeRuleCmd(pattern)
		}
		return nil
	}
	var cmd tea.Cmd
	m.PrioritizeInput, cmd = m.PrioritizeInput.Update(msg)
	return cmd
}

func DetailsKeys(m *model.Model, msg tea.KeyPressMsg) tea.Cmd {
	action := m.Keys.Action(msg)
	if msg.String() == "esc" || action == config.ActionTogglePreview {
		return CloseDetails(m)
	}
	if action == config.ActionToggleHelp {
		m.OpenOverlay(model.StateHelp)
		return m.FlashHint(action)
	}
	if _, isTab := model.TabForAction(action); isTab {
		return SwitchTab(m, action)
	}
	if action == config.ActionToggleFocus {
		if render.DetailsSplit(m) {
			m.DetailsFocused = !m.DetailsFocused
		}
		return m.FlashHint(action)
	}
	if !m.DetailsFocused && render.DetailsSplit(m) {
		switch action {
		case config.ActionScrollUp:
			if m.SelectedIdx > 0 {
				m.SelectedIdx--
				render.RefreshAndScroll(m)
				return ReloadDetails(m)
			}
			return nil
		case config.ActionScrollDown:
			if m.SelectedIdx < m.GetTotalResults()-1 && m.SelectedIdx+1 != m.Limit {
				m.SelectedIdx++
				render.RefreshAndScroll(m)
				return ReloadDetails(m)
			}
			return nil
		}
	}
	switch action {
	case config.ActionCopyResult:
		return copySelectedURL(m)
	case config.ActionEditLabel:
		return OpenLabelEditor(m)
	case config.ActionOpenResult:
		if u := m.GetSelectedURL(); u != "" {
			if err := browser.OpenURL(u); err != nil {
				log.Warn().Err(err).Msg("failed to open URL in browser")
			}
			return m.PostHistoryCmd(u)
		}
	case config.ActionScrollUp:
		m.Details.ScrollUp(1)
		return nil
	case config.ActionScrollDown:
		m.Details.ScrollDown(1)
		return nil
	}
	var cmd tea.Cmd
	m.Details, cmd = m.Details.Update(msg)
	return cmd
}

func LabelInputKeys(m *model.Model, msg tea.KeyPressMsg) tea.Cmd {
	action := m.Keys.Action(msg)
	if len(msg.Text) > 0 && !msg.Mod.Contains(tea.ModAlt) {
		action = ""
	}
	if msg.String() == "esc" {
		m.LabelInput.Blur()
		return CloseOverlay(m)
	}
	if action == config.ActionOpenResult || msg.String() == "enter" {
		label := strings.TrimSpace(m.LabelInput.Value())
		url := m.LabelURL
		m.LabelInput.Blur()
		m.DismissOverlay()
		return m.UpdateLabelCmd(url, label)
	}
	var cmd tea.Cmd
	m.LabelInput, cmd = m.LabelInput.Update(msg)
	return cmd
}
