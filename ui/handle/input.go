// SPDX-FileContributor: FlameFlag <github@flameflag.dev>
//
// SPDX-License-Identifier: AGPLv3+

package handle

import (
	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/ui/model"
	"github.com/asciimoo/hister/ui/network"
	"github.com/asciimoo/hister/ui/render"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/pkg/browser"
)

func InputKeys(m *model.Model, msg tea.KeyMsg) tea.Cmd {
	action := config.Action(m.Cfg.Hotkeys.TUI[msg.String()])
	if msg.Type == tea.KeyRunes && !msg.Alt {
		action = ""
	}

	// Try common actions first
	if cmd, handled := DispatchCommonAction(m, action); handled {
		return cmd
	}

	switch action {
	case config.ActionToggleFocus:
		if m.GetTotalResults() > 0 {
			m.State = model.StateResults
			m.TextInput.Blur()
			if m.SelectedIdx < 0 {
				m.SelectedIdx = 0
			}
			render.RefreshViewport(m)
			m.ScrollToSelected()
		}
		return m.FlashHint(config.ActionToggleFocus)
	case config.ActionOpenResult:
		if m.SelectedIdx >= 0 {
			if m.SelectedIdx == m.Limit {
				m.Limit += 10
				render.RefreshViewport(m)
				m.ScrollToSelected()
				cmds := []tea.Cmd{m.FlashHint(config.ActionOpenResult), doSearch(m)}
				if m.WsReady {
					m.IsSearching = true
					cmds = append(cmds, m.Spinner.Tick)
				}
				return tea.Batch(cmds...)
			} else if u := m.GetSelectedURL(); u != "" {
				browser.OpenURL(u)
				baseURL := m.Cfg.BaseURL("")
				return tea.Batch(m.FlashHint(config.ActionOpenResult), network.PostHistory(baseURL, m.TextInput.Value(), u, m.GetSelectedTitle()))
			}
		}
		return m.FlashHint(config.ActionOpenResult)
	}

	var cmd tea.Cmd
	oldVal := m.TextInput.Value()
	m.TextInput, cmd = m.TextInput.Update(msg)
	if m.TextInput.Value() != oldVal {
		m.Limit = 10
		m.SelectedIdx = -1
		cmds := []tea.Cmd{cmd, doSearch(m)}
		if m.WsReady {
			m.IsSearching = true
			cmds = append(cmds, m.Spinner.Tick)
		}
		return tea.Batch(cmds...)
	}
	return cmd
}

func ResultsKeys(m *model.Model, msg tea.KeyMsg) tea.Cmd {
	action := config.Action(m.Cfg.Hotkeys.TUI[msg.String()])

	if cmd, handled := DispatchCommonAction(m, action); handled {
		return cmd
	}

	switch action {
	case config.ActionToggleFocus:
		m.State = model.StateInput
		m.TextInput.Focus()
		render.RefreshViewport(m)
		return tea.Batch(textinput.Blink, m.FlashHint(config.ActionToggleFocus))
	case config.ActionOpenResult:
		if m.SelectedIdx == m.Limit {
			m.Limit += 10
			render.RefreshViewport(m)
			m.ScrollToSelected()
			cmds := []tea.Cmd{m.FlashHint(config.ActionOpenResult), doSearch(m)}
			if m.WsReady {
				m.IsSearching = true
				cmds = append(cmds, m.Spinner.Tick)
			}
			return tea.Batch(cmds...)
		} else if u := m.GetSelectedURL(); u != "" {
			browser.OpenURL(u)
			baseURL := m.Cfg.BaseURL("")
			return tea.Batch(m.FlashHint(config.ActionOpenResult), network.PostHistory(baseURL, m.TextInput.Value(), u, m.GetSelectedTitle()))
		}
		return m.FlashHint(config.ActionOpenResult)
	}

	var cmd tea.Cmd
	m.Viewport, cmd = m.Viewport.Update(msg)
	return cmd
}
