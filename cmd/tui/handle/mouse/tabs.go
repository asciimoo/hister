// SPDX-FileContributor: 4evy <git@evy.pink>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package mouse

import (
	"github.com/asciimoo/nogon/cmd/tui/model"

	tea "charm.land/bubbletea/v2"
	"github.com/pkg/browser"
	"github.com/rs/zerolog/log"
)

func focusAddField(m *model.Model, idx int) {
	if m.AddFocusIdx < len(m.AddInputs) {
		m.AddInputs[m.AddFocusIdx].Blur()
	} else if m.AddFocusIdx == 2 {
		m.AddText.Blur()
	}
	m.AddFocusIdx = idx
	if idx < len(m.AddInputs) {
		m.AddInputs[idx].Focus()
	} else if idx == 2 {
		m.AddText.Focus()
	}
}

func focusRulesInput(m *model.Model, section, field int) {
	m.RulesSection = section
	m.RulesEditingIdx = -1
	m.RulesEditingSection = section
	m.BlurAllRulesInputs()
	m.RulesFormFocus = field
	if inp := m.FocusedRulesInput(); inp != nil {
		inp.Focus()
	}
}

// --- non-search tab handling ---

func workspaceTargetAt(m *model.Model, msg Event) (model.WorkspaceTarget, bool) {
	y := msg.Y - model.RowWorkspaceStart + m.Workspace.YOffset()
	for _, target := range m.WorkspaceTargets {
		if y >= target.Y && y < target.Y+target.Height {
			return target, true
		}
	}
	return model.WorkspaceTarget{}, false
}

func (h *Handler) nonSearchTab(m *model.Model, msg Event) tea.Cmd {
	if isLeftClick(msg) {
		if msg.Y == model.RowTabBar {
			return h.tabBar(m, msg)
		}
		if msg.Y == model.RowHints(m.Height) {
			return h.hintRegions(m, msg)
		}
		switch m.ActiveTab {
		case model.TabHistory:
			return historyClick(m, msg)
		case model.TabRules:
			return rulesClick(m, msg)
		case model.TabAdd:
			return h.addClick(m, msg)
		}
		return nil
	}
	switch m.ActiveTab {
	case model.TabHistory:
		if len(m.HistoryItems) > 0 {
			handleScroll(msg, &m.HistoryIdx, 0, len(m.HistoryItems)-1, nil)
		}
	case model.TabRules:
		if !m.RulesLoading && m.RulesFormFocus == model.RulesFocusList {
			if n := m.RulesSectionLen(m.RulesSection); n > 0 {
				handleScroll(msg, &m.RulesIdx, 0, n-1, nil)
			}
		}
	case model.TabAdd:
		if delta := wheelDelta(msg); delta < 0 {
			m.Workspace.ScrollUp(3)
		} else if delta > 0 {
			m.Workspace.ScrollDown(3)
		}
	}
	return nil
}

// --- tab click handlers ---

func historyClick(m *model.Model, msg Event) tea.Cmd {
	target, ok := workspaceTargetAt(m, msg)
	if !ok || target.Kind != model.WorkspaceHistoryItem || target.Index >= len(m.HistoryItems) {
		return nil
	}
	if target.Index == m.HistoryIdx {
		if err := browser.OpenURL(m.HistoryItems[target.Index].URL); err != nil {
			log.Warn().Err(err).Msg("failed to open URL in browser")
		}
	} else {
		m.HistoryIdx = target.Index
	}
	return nil
}

func rulesClick(m *model.Model, msg Event) tea.Cmd {
	if m.RulesLoading {
		return nil
	}

	target, ok := workspaceTargetAt(m, msg)
	if !ok {
		return nil
	}
	switch target.Kind {
	case model.WorkspaceRulesForm:
		focus := model.RulesFocusPattern
		if target.Section == model.RulesSectionAliases {
			focus = model.RulesFocusAliasKey
		}
		focusRulesInput(m, target.Section, focus)
	case model.WorkspaceRulesItem:
		m.BlurAllRulesInputs()
		m.RulesFormFocus = model.RulesFocusList
		m.RulesSection = target.Section
		m.RulesIdx = target.Index
	}
	return nil
}

func (h *Handler) addClick(m *model.Model, msg Event) tea.Cmd {
	target, ok := workspaceTargetAt(m, msg)
	if !ok {
		return nil
	}
	if target.Kind == model.WorkspaceAddField {
		focusAddField(m, target.Index)
		return nil
	}
	if target.Kind == model.WorkspaceAddSubmit {
		focusAddField(m, model.AddSubmitFieldIdx)
		return h.SubmitAdd(m)
	}
	return nil
}
