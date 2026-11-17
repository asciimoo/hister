// SPDX-FileContributor: 4evy <git@evy.pink>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package handle

import (
	"github.com/asciimoo/nogon/cmd/tui/model"
	"github.com/asciimoo/nogon/cmd/tui/render"
	"github.com/asciimoo/nogon/config"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/pkg/browser"
	"github.com/rs/zerolog/log"
)

func InputKeys(m *model.Model, msg tea.KeyPressMsg) tea.Cmd {
	action := m.Keys.Action(msg)
	if len(msg.Text) > 0 && !msg.Mod.Contains(tea.ModAlt) {
		action = ""
	}

	// Try common actions first
	if cmd, handled := DispatchCommonAction(m, action); handled {
		return cmd
	}

	switch action {
	case config.ActionToggleFocus:
		if m.FocusSearchResults() {
			if m.SelectedIdx < 0 {
				m.SelectedIdx = 0
			}
			render.RefreshAndScroll(m)
		}
		return m.FlashHint(config.ActionToggleFocus)
	case config.ActionOpenResult:
		if m.Results != nil && m.Results.QuerySuggestion != "" {
			suggestion := m.Results.QuerySuggestion
			m.TextInput.SetValue(suggestion)
			m.TextInput.SetCursor(len([]rune(suggestion)))
			m.Limit = model.ResultsPageSize
			m.SelectedIdx = -1
			return startSearch(m, m.FlashHint(config.ActionOpenResult))
		}
		return m.FlashHint(config.ActionOpenResult)
	}

	return updateSearchInput(m, msg)
}

func updateSearchInput(m *model.Model, msg tea.Msg) tea.Cmd {
	oldVal := m.TextInput.Value()
	var cmd tea.Cmd
	m.TextInput, cmd = m.TextInput.Update(msg)
	if m.TextInput.Value() != oldVal {
		m.Limit = model.ResultsPageSize
		m.SelectedIdx = -1
		return startSearch(m, cmd)
	}
	return cmd
}

func ResultsKeys(m *model.Model, msg tea.KeyPressMsg) tea.Cmd {
	action := m.Keys.Action(msg)

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
			m.Limit += model.ResultsPageSize
			render.RefreshAndScroll(m)
			return startSearch(m, m.FlashHint(config.ActionOpenResult))
		} else if u := m.GetSelectedURL(); u != "" {
			if err := browser.OpenURL(u); err != nil {
				log.Warn().Err(err).Msg("failed to open URL in browser")
			}
			return tea.Batch(m.FlashHint(config.ActionOpenResult), m.PostHistoryCmd(u))
		}
		return m.FlashHint(config.ActionOpenResult)
	}

	var cmd tea.Cmd
	m.Viewport, cmd = m.Viewport.Update(msg)
	return cmd
}
