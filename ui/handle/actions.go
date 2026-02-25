// SPDX-FileContributor: FlameFlag <github@flameflag.dev>
//
// SPDX-License-Identifier: AGPLv3+

package handle

import (
	"strings"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/ui/model"
	"github.com/asciimoo/hister/ui/network"
	"github.com/asciimoo/hister/ui/render"
	"github.com/asciimoo/hister/ui/theme"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pkg/browser"
)

func DispatchCommonAction(m *model.Model, action config.Action) (tea.Cmd, bool) {
	switch action {
	case config.ActionQuit:
		return tea.Batch(m.FlashHint(config.ActionQuit), tea.Quit), true
	case config.ActionToggleHelp:
		m.OpenOverlay(model.StateHelp)
		return m.FlashHint(config.ActionToggleHelp), true
	case config.ActionToggleTheme:
		if m.ThemeName == "no-color" {
			return nil, true
		}
		m.OpenThemePicker()
		return nil, true
	case config.ActionToggleSettings:
		m.SettingsIdx = 0
		m.OpenOverlay(model.StateSettings)
		return nil, true
	case config.ActionToggleSort:
		if m.SortMode == "" {
			m.SortMode = "domain"
		} else {
			m.SortMode = ""
		}
		cmds := []tea.Cmd{m.FlashHint(config.ActionToggleSort), doSearch(m)}
		if m.WsReady {
			m.IsSearching = true
			cmds = append(cmds, m.Spinner.Tick)
		}
		return tea.Batch(cmds...), true
	case config.ActionScrollUp:
		if m.SelectedIdx > 0 {
			m.SelectedIdx--
			render.RefreshViewport(m)
			m.ScrollToSelected()
		}
		return m.FlashHint(config.ActionScrollUp), true
	case config.ActionScrollDown:
		if m.SelectedIdx < m.GetTotalResults()-1 {
			m.SelectedIdx++
			render.RefreshViewport(m)
			m.ScrollToSelected()
		}
		return m.FlashHint(config.ActionScrollDown), true
	case config.ActionDeleteResult:
		if u := m.GetSelectedURL(); u != "" {
			m.OpenDeleteDialog("Delete Result", u, -1, func() tea.Cmd {
				return tea.Batch(
					network.DeleteURL(m.Cfg.BaseURL(""), u),
					doSearch(m),
				)
			})
		}
		return m.FlashHint(config.ActionDeleteResult), true
	case config.ActionTabSearch, config.ActionTabHistory, config.ActionTabRules, config.ActionTabAdd:
		return SwitchTab(m, action), true
	}
	return nil, false
}

func ExecuteAction(m *model.Model, action config.Action) tea.Cmd {
	if cmd, handled := DispatchCommonAction(m, action); handled {
		return cmd
	}
	switch action {
	case config.ActionOpenResult:
		if m.SelectedIdx >= 0 {
			if u := m.GetSelectedURL(); u != "" {
				browser.OpenURL(u)
				baseURL := m.Cfg.BaseURL("")
				return tea.Batch(m.FlashHint(config.ActionOpenResult), network.PostHistory(baseURL, m.TextInput.Value(), u, m.GetSelectedTitle()))
			}
		}
		return m.FlashHint(config.ActionOpenResult)
	case config.ActionToggleFocus:
		if m.State == model.StateInput {
			if m.GetTotalResults() > 0 {
				m.State = model.StateResults
				m.TextInput.Blur()
				if m.SelectedIdx < 0 {
					m.SelectedIdx = 0
				}
				render.RefreshViewport(m)
				m.ScrollToSelected()
			}
		} else {
			m.State = model.StateInput
			return m.TextInput.Focus()
		}
		return nil
	}
	return nil
}

func SwitchTab(m *model.Model, action config.Action) tea.Cmd {
	prevTab := m.ActiveTab
	if tab, ok := model.ActionToTab[action]; ok {
		m.ActiveTab = tab
	}
	if m.ActiveTab == prevTab {
		return nil
	}
	m.TextInput.Blur()
	m.State = model.StateResults
	baseURL := m.Cfg.BaseURL("")
	var cmd tea.Cmd
	switch m.ActiveTab {
	case model.TabSearch:
		m.State = model.StateInput
		cmd = m.TextInput.Focus()
	case model.TabHistory:
		m.HistoryLoading = true
		cmd = network.FetchHistory(baseURL)
	case model.TabRules:
		m.RulesLoading = true
		m.RulesFormFocus = model.RulesFieldList
		m.RulesEditingIdx = -1
		m.BlurAllRulesInputs()
		cmd = network.FetchRules(baseURL)
	case model.TabAdd:
		m.AddInputs[0].Focus()
		m.AddFocusIdx = 0
	}
	return cmd
}

func doSearch(m *model.Model) tea.Cmd {
	q := m.TextInput.Value()
	if strings.TrimSpace(q) == "" {
		return func() tea.Msg {
			return model.ResultsMsg{Results: nil}
		}
	}
	return network.Search(m.Conn, &m.WsMu, m.WsReady, model.SearchQuery{
		Text:      strings.TrimSpace(q),
		Highlight: "tui",
		Limit:     m.Limit + 1,
		Sort:      m.SortMode,
	})
}

func CloseOverlay(m *model.Model) tea.Cmd {
	m.OverlayOffX, m.OverlayOffY = 0, 0
	m.IsDragging = false
	m.State = m.PrevState
	if m.State == model.StateInput {
		return m.TextInput.Focus()
	}
	return nil
}

func CloseThemePickerWithRevert(m *model.Model) tea.Cmd {
	m.Cfg.TUI.DarkTheme = m.OrigDarkTheme
	m.Cfg.TUI.LightTheme = m.OrigLightTheme
	m.Cfg.TUI.ColorScheme = m.OrigColorScheme
	m.ThemePickerMode = m.OrigColorScheme
	if p, ok := theme.GetPalette(m.OrigThemeName); ok {
		m.ApplyTheme(p)
		render.RefreshViewport(m)
	}
	return CloseOverlay(m)
}
