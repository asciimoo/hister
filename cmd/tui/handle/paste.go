// SPDX-FileContributor: 4evy <git@evy.pink>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package handle

import (
	"github.com/asciimoo/hister/cmd/tui/model"

	tea "charm.land/bubbletea/v2"
)

// Paste routes Bubble Tea v2's dedicated bracketed-paste message to the
// focused Bubbles component. Paste is intentionally separate from key action
// dispatch: pasted text must never be interpreted as a Nogon hotkey.
func Paste(m *model.Model, msg tea.PasteMsg) tea.Cmd {
	switch m.State {
	case model.StatePrioritizeInput:
		var cmd tea.Cmd
		m.PrioritizeInput, cmd = m.PrioritizeInput.Update(msg)
		return cmd
	case model.StateLabelInput:
		var cmd tea.Cmd
		m.LabelInput, cmd = m.LabelInput.Update(msg)
		return cmd
	}

	switch m.ActiveTab {
	case model.TabSearch:
		if m.State == model.StateInput {
			return updateSearchInput(m, msg)
		}
	case model.TabRules:
		if m.RulesFormFocus < model.RulesFocusList {
			return updateRulesInput(m, msg)
		}
	case model.TabAdd:
		return updateAddInput(m, msg)
	}
	return nil
}
