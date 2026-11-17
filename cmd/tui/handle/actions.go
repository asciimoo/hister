// SPDX-FileContributor: 4evy <git@evy.pink>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package handle

import (
	"strings"

	"github.com/asciimoo/nogon/cmd/tui/model"
	"github.com/asciimoo/nogon/cmd/tui/network"
	"github.com/asciimoo/nogon/cmd/tui/render"
	"github.com/asciimoo/nogon/cmd/tui/theme"
	"github.com/asciimoo/nogon/config"

	tea "charm.land/bubbletea/v2"
	"github.com/pkg/browser"
	"github.com/rs/zerolog/log"
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
		return startSearch(m, m.FlashHint(config.ActionToggleSort)), true
	case config.ActionToggleSemantic:
		if !m.SemanticEnabled {
			return m.NotifyWarning("Semantic search is not enabled on this server"), true
		}
		m.SemanticOn = !m.SemanticOn
		if m.SemanticOn {
			return startSearch(m, m.Notify("Semantic search on"), m.FlashHint(action)), true
		}
		return startSearch(m, m.Notify("Semantic search off"), m.FlashHint(action)), true
	case config.ActionScrollUp:
		if m.State == model.StateDetails {
			if m.DetailsFocused || !render.DetailsSplit(m) {
				m.Details.ScrollUp(1)
				return m.FlashHint(action), true
			}
			if m.SelectedIdx > 0 {
				m.SelectedIdx--
				render.RefreshAndScroll(m)
				return tea.Batch(ReloadDetails(m), m.FlashHint(action)), true
			}
			return nil, true
		}
		enteredResults := m.FocusSearchResults()
		if enteredResults && m.SelectedIdx < 0 {
			m.SelectedIdx = 0
			render.RefreshAndScroll(m)
			return m.FlashHint(config.ActionScrollUp), true
		}
		if m.SelectedIdx > 0 {
			m.SelectedIdx--
			render.RefreshAndScroll(m)
		}
		return m.FlashHint(config.ActionScrollUp), true
	case config.ActionScrollDown:
		if m.State == model.StateDetails {
			if m.DetailsFocused || !render.DetailsSplit(m) {
				m.Details.ScrollDown(1)
				return m.FlashHint(action), true
			}
			if m.SelectedIdx < m.GetTotalResults()-1 && m.SelectedIdx+1 != m.Limit {
				m.SelectedIdx++
				render.RefreshAndScroll(m)
				return tea.Batch(ReloadDetails(m), m.FlashHint(action)), true
			}
			return nil, true
		}
		enteredResults := m.FocusSearchResults()
		if enteredResults && m.SelectedIdx < 0 {
			m.SelectedIdx = 0
			render.RefreshAndScroll(m)
			return m.FlashHint(config.ActionScrollDown), true
		}
		if m.SelectedIdx < m.GetTotalResults()-1 {
			m.SelectedIdx++
			render.RefreshAndScroll(m)
		}
		return m.FlashHint(config.ActionScrollDown), true
	case config.ActionDeleteResult:
		if u := m.GetSelectedURL(); u != "" {
			m.OpenDeleteDialog("Delete Result", u, -1, func() tea.Cmd {
				return m.DeleteURLCmd(u)
			})
		}
		return m.FlashHint(config.ActionDeleteResult), true
	case config.ActionCopyResult:
		return copySelectedURL(m), true
	case config.ActionTogglePreview:
		if m.State == model.StateDetails {
			return CloseDetails(m), true
		}
		return OpenDetails(m), true
	case config.ActionEditLabel:
		return OpenLabelEditor(m), true
	case config.ActionTabSearch, config.ActionTabHistory, config.ActionTabRules, config.ActionTabAdd:
		return SwitchTab(m, action), true
	}
	return nil, false
}

func copySelectedURL(m *model.Model) tea.Cmd {
	u := m.GetSelectedURL()
	if m.ActiveTab == model.TabHistory && m.HistoryIdx >= 0 && m.HistoryIdx < len(m.HistoryItems) {
		u = m.HistoryItems[m.HistoryIdx].URL
	}
	if u == "" {
		return m.NotifyWarning("Nothing selected")
	}
	return tea.Batch(tea.SetClipboard(u), m.NotifySuccess("URL copied"), m.FlashHint(config.ActionCopyResult))
}

func OpenDetails(m *model.Model) tea.Cmd {
	return loadDetails(m, true, true)
}

// ReloadDetails updates an already-open pane after its result selection
// changes while retaining result-list focus.
func ReloadDetails(m *model.Model) tea.Cmd {
	return loadDetails(m, false, false)
}

func loadDetails(m *model.Model, focusPreview, activate bool) tea.Cmd {
	u := m.GetSelectedURL()
	if u == "" {
		return m.NotifyWarning("Nothing selected")
	}
	title := m.GetSelectedTitle()
	startSpinner := !m.DetailsLoading
	m.ResetDetails()
	m.DetailsURL = u
	m.DetailsHintTitle = title
	m.DetailsLoading = true
	m.DetailsFocused = focusPreview
	if activate && m.State != model.StateDetails {
		m.OpenOverlay(model.StateDetails)
	}
	render.ResizeSearchViewports(m)
	m.Details.SetContent(render.ResultDetailsContent(m))
	m.Details.GotoTop()
	render.RefreshAndScroll(m)
	cmds := []tea.Cmd{m.QueuePreviewCmd(u)}
	if startSpinner {
		cmds = append(cmds, m.Spinner.Tick)
	}
	return tea.Batch(cmds...)
}

func OpenLabelEditor(m *model.Model) tea.Cmd {
	doc := m.GetSelectedDocument()
	if doc == nil {
		return m.NotifyWarning("Labels are available for indexed documents")
	}
	m.LabelURL = doc.URL
	m.LabelInput.SetValue(doc.Label)
	m.LabelInput.SetCursor(len([]rune(doc.Label)))
	m.LabelInput.Focus()
	m.OpenOverlay(model.StateLabelInput)
	return nil
}

func ExecuteAction(m *model.Model, action config.Action) tea.Cmd {
	if cmd, handled := DispatchCommonAction(m, action); handled {
		return cmd
	}
	switch action {
	case config.ActionOpenResult:
		if m.SelectedIdx >= 0 {
			if u := m.GetSelectedURL(); u != "" {
				if err := browser.OpenURL(u); err != nil {
					log.Warn().Err(err).Msg("failed to open URL in browser")
				}
				return tea.Batch(m.FlashHint(config.ActionOpenResult), m.PostHistoryCmd(u))
			}
		}
		return m.FlashHint(config.ActionOpenResult)
	case config.ActionToggleFocus:
		if m.State == model.StateDetails {
			if render.DetailsSplit(m) {
				m.DetailsFocused = !m.DetailsFocused
			}
			return m.FlashHint(action)
		}
		if m.State == model.StateInput {
			if m.GetTotalResults() > 0 {
				m.State = model.StateResults
				m.TextInput.Blur()
				if m.SelectedIdx < 0 {
					m.SelectedIdx = 0
				}
				render.RefreshAndScroll(m)
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
	if tab, ok := model.TabForAction(action); ok {
		m.ActiveTab = tab
	}
	if m.ActiveTab == prevTab {
		return nil
	}
	m.ResetDetails()
	m.SetBaseState(model.StateResults)
	m.Workspace.GotoTop()
	m.TextInput.Blur()
	var cmd tea.Cmd
	switch m.ActiveTab {
	case model.TabSearch:
		m.State = model.StateInput
		cmd = m.TextInput.Focus()
	case model.TabHistory:
		m.HistoryLoading = true
		cmd = m.FetchHistoryCmd()
	case model.TabRules:
		m.RulesLoading = true
		m.RulesFormFocus = model.RulesFocusList
		m.RulesEditingIdx = -1
		m.BlurAllRulesInputs()
		cmd = m.FetchRulesCmd()
	case model.TabAdd:
		m.AddInputs[0].Focus()
		m.AddFocusIdx = 0
	}
	return cmd
}

func startSearch(m *model.Model, extra ...tea.Cmd) tea.Cmd {
	cmds := append([]tea.Cmd{doSearch(m)}, extra...)
	if m.WsReady {
		m.IsSearching = true
		cmds = append(cmds, m.Spinner.Tick)
	}
	return tea.Batch(cmds...)
}

func doSearch(m *model.Model) tea.Cmd {
	q := m.TextInput.Value()
	if strings.TrimSpace(q) == "" {
		return func() tea.Msg {
			return model.ResultsMsg{Results: nil}
		}
	}
	return network.Search(m.Conn, &m.WsMu, m.WsReady, model.SearchQuery{
		Text:              strings.TrimSpace(q),
		Highlight:         "tui",
		Limit:             m.Limit + 1,
		Sort:              m.SortMode,
		SemanticEnabled:   m.SemanticOn,
		SemanticThreshold: m.SemanticThreshold,
		SemanticWeight:    m.SemanticWeight,
	})
}

func CloseOverlay(m *model.Model) tea.Cmd {
	if m.State == model.StateDetails {
		return CloseDetails(m)
	}
	m.DismissOverlay()
	if m.State == model.StateInput {
		return m.TextInput.Focus()
	}
	return nil
}

func CloseDetails(m *model.Model) tea.Cmd {
	m.ResetDetails()
	m.DismissOverlay()
	render.ResizeSearchViewports(m)
	render.RefreshAndScroll(m)
	if m.State == model.StateInput {
		return m.TextInput.Focus()
	}
	return nil
}

func submitAdd(m *model.Model) tea.Cmd {
	u := strings.TrimSpace(m.AddInputs[0].Value())
	if u == "" {
		m.AddStatus = "URL is required"
		m.AddStatusKind = model.NoticeError
		m.AddFocusIdx = 0
		m.AddInputs[0].Focus()
		return nil
	}
	if !strings.Contains(u, "://") {
		u = "https://" + u
		m.AddInputs[0].SetValue(u)
	}
	title := strings.TrimSpace(m.AddInputs[1].Value())
	text := strings.TrimSpace(m.AddText.Value())
	m.AddStatus = "Adding..."
	m.AddStatusKind = model.NoticeInfo
	return m.AddPageCmd(u, title, text)
}

func CloseThemePickerWithRevert(m *model.Model) tea.Cmd {
	m.Cfg.TUI.DarkTheme = m.OrigDarkTheme
	m.Cfg.TUI.LightTheme = m.OrigLightTheme
	m.Cfg.TUI.ColorScheme = m.OrigColorScheme
	m.ThemePickerMode = m.OrigColorScheme
	p, _ := theme.ResolvePalette(&m.Cfg.TUI, m.IsDarkBg)
	m.ApplyTheme(p)
	render.RefreshViewport(m)
	return CloseOverlay(m)
}
