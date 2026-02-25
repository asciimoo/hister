// SPDX-FileContributor: FlameFlag <github@flameflag.dev>
//
// SPDX-License-Identifier: AGPLv3+

package handle

import (
	"time"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/ui/model"
	"github.com/asciimoo/hister/ui/network"
	"github.com/asciimoo/hister/ui/render"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

func Update(m *model.Model, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		changed := m.Width != msg.Width || m.Height != msg.Height
		m.Width, m.Height = msg.Width, msg.Height

		vpH := max(0, m.Height-model.FixedLayoutRows)
		vpW := max(1, m.Width-2)
		m.TextInput.Width = max(1, m.Width-6)

		if !m.Ready {
			m.Viewport = viewport.New(vpW, vpH)
			m.Viewport.SetContent("")
			m.Ready = true
			return tea.ClearScreen
		}
		m.Viewport.Width, m.Viewport.Height = vpW, vpH
		render.RefreshViewport(m)
		m.ScrollToSelected()
		if changed {
			return tea.ClearScreen
		}
		return nil

	case tea.KeyMsg:
		if config.Action(m.Cfg.Hotkeys.TUI[msg.String()]) == config.ActionQuit {
			return tea.Quit
		}
		switch m.State {
		case model.StateDialog:
			return DialogKeys(m, msg)
		case model.StateInput:
			if m.ActiveTab != model.TabSearch {
				return TabKeys(m, msg)
			}
			return InputKeys(m, msg)
		case model.StateResults:
			if m.ActiveTab != model.TabSearch {
				return TabKeys(m, msg)
			}
			return ResultsKeys(m, msg)
		case model.StateHelp:
			m.State = m.PrevState
			if m.State == model.StateInput {
				return m.TextInput.Focus()
			}
			return nil
		case model.StateThemePicker:
			return ThemePickerKeys(m, msg)
		case model.StateContextMenu:
			return ContextMenuKeys(m, msg)
		case model.StateSettings:
			return SettingsKeys(m, msg)
		case model.StatePrioritizeInput:
			return PrioritizeInputKeys(m, msg)
		}

	case tea.MouseMsg:
		return Mouse(m, msg)

	case spinner.TickMsg:
		if m.IsSearching || m.HistoryLoading || m.RulesLoading {
			var cmd tea.Cmd
			m.Spinner, cmd = m.Spinner.Update(msg)
			return cmd
		}

	case model.HintClearMsg:
		m.HintFlash = ""
	case model.SettingsErrClearMsg:
		m.SettingsEditErr = ""

	case model.HistoryFetchedMsg:
		m.HistoryLoading = false
		m.HistoryItems = msg.Items
		m.HistoryIdx = 0

	case model.RulesFetchedMsg:
		m.RulesLoading = false
		m.RulesData = msg.Data
		m.RulesIdx = 0

	case model.AddResultMsg:
		if msg.Err != nil {
			m.AddStatus = "Error: " + msg.Err.Error()
		} else {
			m.AddStatus = "Added successfully!"
			for i := range m.AddInputs {
				m.AddInputs[i].SetValue("")
			}
		}

	case model.RulesSavedMsg:
		if msg.Err == nil {
			m.RulesLoading = true
			return network.FetchRules(m.Cfg.BaseURL(""))
		}

	case model.ResultsMsg:
		m.IsSearching = false
		m.Results = msg.Results
		if m.SelectedIdx >= m.GetTotalResults() {
			m.SelectedIdx = m.GetTotalResults() - 1
		}
		if m.SelectedIdx < 0 && m.GetTotalResults() > 0 {
			m.SelectedIdx = 0
		}
		render.RefreshViewport(m)
		m.ScrollToSelected()
		return network.ListenToWebSocket(m.WsChan, m.WsDone)

	case model.WsConnectedMsg:
		if msg.Conn != nil {
			m.Conn = msg.Conn
			m.WsReady = true
		}
		return network.ListenToWebSocket(m.WsChan, m.WsDone)

	case model.WsDisconnectedMsg:
		m.WsReady = false
		m.IsSearching = false
		if msg.Err != nil {
			m.ConnError = msg.Err
		}
		return tea.Tick(2*time.Second, func(_ time.Time) tea.Msg { return model.ReconnectMsg{} })

	case model.ReconnectMsg:
		return network.ConnectWebSocket(m.Cfg.WebSocketURL(), m.Cfg.BaseURL(""), m.WsChan, m.WsDone)

	case model.ErrMsg:
		return network.ListenToWebSocket(m.WsChan, m.WsDone)
	}
	return nil
}
