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

func vpBounds(m *model.Model) (top, bottom int) {
	return model.RowVPStart, model.RowVPEnd(m.Height)
}

func scrollToPercent(m *model.Model, mouseY int) {
	top, bottom := vpBounds(m)
	vpH := bottom - top + 1
	relY := max(0, min(mouseY-top, vpH-1))
	pct := float64(relY) / float64(vpH-1)
	maxScroll := m.TotalLines - m.Viewport.Height
	m.Viewport.SetYOffset(int(pct * float64(maxScroll)))
	contentY := m.Viewport.YOffset + m.Viewport.Height/2
	m.SelectedIdx = m.FindResultAtY(contentY)
	render.RefreshViewport(m)
}

func Mouse(m *model.Model, msg tea.MouseMsg) tea.Cmd {
	// Scrollbar drag handling
	if m.ScrollbarDragging {
		if msg.Action == tea.MouseActionMotion {
			scrollToPercent(m, msg.Y)
			return nil
		}
		if msg.Action == tea.MouseActionRelease {
			m.ScrollbarDragging = false
			return nil
		}
	}

	// A. Overlay states
	if m.State == model.StateHelp || m.State == model.StateDialog || m.State == model.StateThemePicker || m.State == model.StateSettings || m.State == model.StateContextMenu || m.State == model.StatePrioritizeInput {
		return mouseOverlay(m, msg)
	}

	// B. Normal states (input/results)

	// Non-search tabs
	if m.ActiveTab != model.TabSearch {
		return mouseNonSearchTab(m, msg)
	}

	switch msg.Button {
	case tea.MouseButtonWheelUp:
		if m.SelectedIdx > 0 {
			m.SelectedIdx--
			render.RefreshViewport(m)
			m.ScrollToSelected()
		}
		return nil
	case tea.MouseButtonWheelDown:
		if m.SelectedIdx < m.GetTotalResults()-1 {
			m.SelectedIdx++
			render.RefreshViewport(m)
			m.ScrollToSelected()
		}
		return nil
	}

	// Right-click → context menu
	if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonRight {
		top, bottom := vpBounds(m)
		if msg.Y >= top && msg.Y <= bottom && len(m.LineOffsets) > 0 {
			contentY := (msg.Y - top) + m.Viewport.YOffset
			idx := m.FindResultAtY(contentY)
			if idx >= 0 && idx < m.GetTotalResults() && idx != m.Limit {
				m.SelectedIdx = idx
				render.RefreshViewport(m)
				m.MenuX = msg.X
				m.MenuY = msg.Y
				m.MenuIdx = idx
				m.MenuSelIdx = 0
				m.PrevState = m.State
				m.State = model.StateContextMenu
				offX, offY := render.MenuOverlayOffset(m)
				m.OverlayOffX, m.OverlayOffY = offX, offY
				return nil
			}
		}
		return nil
	}

	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return nil
	}

	// Click on tab bar
	if msg.Y == model.RowTabBar {
		return mouseTabBar(m, msg)
	}

	// Click on input row
	if msg.Y == model.RowInput {
		m.State = model.StateInput
		pos := msg.X - 4
		if pos < 0 {
			pos = 0
		}
		val := m.TextInput.Value()
		if pos > len([]rune(val)) {
			pos = len([]rune(val))
		}
		m.TextInput.SetCursor(pos)
		return m.TextInput.Focus()
	}

	// Click on scrollbar column
	if m.TotalLines > m.Viewport.Height && m.Viewport.Height > 0 && msg.X >= m.Width-2 {
		top, bottom := vpBounds(m)
		if msg.Y >= top && msg.Y <= bottom {
			m.ScrollbarDragging = true
			scrollToPercent(m, msg.Y)
			return nil
		}
	}

	// Click in viewport
	top, bottom := vpBounds(m)
	if msg.Y >= top && msg.Y <= bottom && len(m.LineOffsets) > 0 {
		contentY := (msg.Y - top) + m.Viewport.YOffset
		if m.SuggestionHeight > 0 && contentY < m.SuggestionHeight && m.Results != nil && m.Results.QuerySuggestion != "" {
			m.TextInput.SetValue(m.Results.QuerySuggestion)
			m.TextInput.SetCursor(len([]rune(m.Results.QuerySuggestion)))
			m.SelectedIdx = -1
			m.Limit = 10
			cmds := []tea.Cmd{doSearch(m)}
			if m.WsReady {
				m.IsSearching = true
				cmds = append(cmds, m.Spinner.Tick)
			}
			return tea.Batch(cmds...)
		}
		idx := m.FindResultAtY(contentY)
		if idx >= 0 && idx < m.GetTotalResults() {
			if m.State == model.StateInput {
				m.State = model.StateResults
				m.TextInput.Blur()
			}
			if idx == m.SelectedIdx {
				if m.SelectedIdx == m.Limit {
					m.Limit += 10
					render.RefreshViewport(m)
					m.ScrollToSelected()
					cmds := []tea.Cmd{doSearch(m)}
					if m.WsReady {
						m.IsSearching = true
						cmds = append(cmds, m.Spinner.Tick)
					}
					return tea.Batch(cmds...)
				} else if u := m.GetSelectedURL(); u != "" {
					browser.OpenURL(u)
					baseURL := m.Cfg.BaseURL("")
					return network.PostHistory(baseURL, m.TextInput.Value(), u, m.GetSelectedTitle())
				}
			} else {
				m.SelectedIdx = idx
				render.RefreshViewport(m)
				m.ScrollToSelected()
			}
		}
		return nil
	}

	// Click on hints row
	if msg.Y == model.RowHints(m.Height) {
		regions := render.ComputeHintRegions(m)
		for _, r := range regions {
			if msg.X >= r.X0 && msg.X < r.X1 {
				return ExecuteAction(m, r.Action)
			}
		}
		return nil
	}

	return nil
}

// handles mouse events when an overlay is active
func mouseOverlay(m *model.Model, msg tea.MouseMsg) tea.Cmd {
	// Continue drag
	if m.IsDragging && msg.Action == tea.MouseActionMotion {
		m.OverlayOffX = m.DragOffX0 + (msg.X - m.DragStartX)
		m.OverlayOffY = m.DragOffY0 + (msg.Y - m.DragStartY)
		if m.OverlayOffX > m.Width/2 {
			m.OverlayOffX = m.Width / 2
		}
		if m.OverlayOffX < -m.Width/2 {
			m.OverlayOffX = -m.Width / 2
		}
		if m.OverlayOffY > m.Height/2 {
			m.OverlayOffY = m.Height / 2
		}
		if m.OverlayOffY < -m.Height/2 {
			m.OverlayOffY = -m.Height / 2
		}
		return nil
	}
	// End drag
	if m.IsDragging && msg.Action == tea.MouseActionRelease {
		m.IsDragging = false
		return nil
	}

	if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
		ox, oy, ow, oh := render.OverlayBounds(m)
		if msg.Y == oy && msg.X >= ox && msg.X < ox+ow {
			// [x] close button
			closeX := ox + ow - 4
			if msg.X >= closeX && msg.X < closeX+3 {
				if m.State == model.StateThemePicker {
					return CloseThemePickerWithRevert(m)
				}
				return CloseOverlay(m)
			}
			// Click on top border → start drag
			m.IsDragging = true
			m.DragStartX = msg.X
			m.DragStartY = msg.Y
			m.DragOffX0 = m.OverlayOffX
			m.DragOffY0 = m.OverlayOffY
			return nil
		}
		if msg.X >= ox && msg.X < ox+ow && msg.Y >= oy && msg.Y < oy+oh {
			// Inside overlay
			if m.State == model.StateThemePicker {
				return mouseThemePickerInside(m, msg, ox, oy)
			}
			if m.State == model.StateContextMenu {
				relY := msg.Y - oy
				optStartY := model.OverlayBorderRows + model.OverlayPaddingRows
				optIdx := relY - optStartY
				if optIdx >= 0 && optIdx < model.MenuOptionCount {
					m.MenuSelIdx = optIdx
					return executeContextMenuAction(m)
				}
			}
			if m.State == model.StateDialog {
				relY := msg.Y - oy
				if relY == model.DialogBtnRowY() {
					relX := msg.X - ox
					if relX < ow/2 {
						m.DialogBtnIdx = 0
					} else {
						m.DialogBtnIdx = 1
					}
					// Confirm selection (same as pressing enter in DialogKeys)
					if m.DialogBtnIdx == 1 && m.DialogConfirm != nil {
						cmd := m.DialogConfirm()
						m.DialogConfirm = nil
						m.DismissDialog()
						return cmd
					}
					m.DialogConfirm = nil
					m.DismissDialog()
					return nil
				}
			}
			if m.State == model.StatePrioritizeInput {
				relY := msg.Y - oy
				if relY == model.PrioritizeBtnRowY() {
					relX := msg.X - ox
					// Cancel button starts around x=3 (padding), Confirm after gap
					if relX < ow/2 {
						m.PrioritizeBtnIdx = 0
						m.PrioritizeInput.Blur()
						m.State = m.PrevState
					} else {
						m.PrioritizeBtnIdx = 1
						pattern := strings.TrimSpace(m.PrioritizeInput.Value())
						m.PrioritizeInput.Blur()
						m.State = m.PrevState
						if pattern != "" {
							m.RulesData.Priority = append(m.RulesData.Priority, pattern)
							baseURL := m.Cfg.BaseURL("")
							return network.PostRules(baseURL, strings.Join(m.RulesData.Skip, "\n"), strings.Join(m.RulesData.Priority, "\n"))
						}
					}
					return nil
				}
			}
			return nil
		}
		// Outside overlay → close
		if m.State == model.StateThemePicker {
			return CloseThemePickerWithRevert(m)
		}
		return CloseOverlay(m)
	}

	// Scroll wheel in overlays
	if m.State == model.StateThemePicker {
		return mouseThemePickerScroll(m, msg)
	}
	if m.State == model.StateSettings {
		return mouseSettingsScroll(m, msg)
	}
	return nil
}

func mouseThemePickerInside(m *model.Model, msg tea.MouseMsg, ox, oy int) tea.Cmd {
	relY := msg.Y - oy
	modeRowY := 2

	if relY == modeRowY {
		relX := msg.X - ox - 3
		modeStartX := 6
		modes := []string{"auto", "dark", "light"}
		cur := modeStartX
		for _, mode := range modes {
			labelW := len(mode) + 2
			if relX >= cur && relX < cur+labelW {
				m.ThemePickerMode = mode
				m.Cfg.TUI.ColorScheme = mode
				p, _ := theme.ResolvePalette(&m.Cfg.TUI, m.IsDarkBg)
				m.ApplyTheme(p)
				render.RefreshViewport(m)
				return nil
			}
			cur += labelW + 1
		}
	} else {
		darkNames, lightNames := theme.ClassifyThemes()
		darkHeaderY := modeRowY + 2
		darkListStartY := darkHeaderY + 1
		lightHeaderY := darkListStartY + len(darkNames) + 1
		lightListStartY := lightHeaderY + 1

		if relY >= darkListStartY && relY < darkListStartY+len(darkNames) {
			idx := relY - darkListStartY
			m.ThemePickerSection = 0
			m.DarkThemeIdx = idx
			if p, ok := theme.GetPalette(darkNames[idx]); ok {
				m.ApplyTheme(p)
				render.RefreshViewport(m)
			}
		} else if relY >= lightListStartY && relY < lightListStartY+len(lightNames) {
			idx := relY - lightListStartY
			m.ThemePickerSection = 1
			m.LightThemeIdx = idx
			if p, ok := theme.GetPalette(lightNames[idx]); ok {
				m.ApplyTheme(p)
				render.RefreshViewport(m)
			}
		}
	}
	return nil
}

func mouseThemePickerScroll(m *model.Model, msg tea.MouseMsg) tea.Cmd {
	darkNames, lightNames := theme.ClassifyThemes()
	delta := 0
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		delta = -1
	case tea.MouseButtonWheelDown:
		delta = 1
	default:
		return nil
	}
	if m.ThemePickerSection == 0 {
		model.ScrollIdx(&m.DarkThemeIdx, delta, 0, len(darkNames)-1)
	} else {
		model.ScrollIdx(&m.LightThemeIdx, delta, 0, len(lightNames)-1)
	}
	previewTheme(m)
	return nil
}

func mouseSettingsScroll(m *model.Model, msg tea.MouseMsg) tea.Cmd {
	totalItems := len(m.Cfg.Hotkeys.TUI)
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		model.ScrollIdx(&m.SettingsIdx, -1, 0, totalItems-1)
	case tea.MouseButtonWheelDown:
		model.ScrollIdx(&m.SettingsIdx, 1, 0, totalItems-1)
	}
	return nil
}

func mouseNonSearchTab(m *model.Model, msg tea.MouseMsg) tea.Cmd {
	if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
		if msg.Y == model.RowTabBar {
			return mouseTabBar(m, msg)
		}
		// Click on hints row
		if msg.Y == model.RowHints(m.Height) {
			regions := render.ComputeHintRegions(m)
			for _, r := range regions {
				if msg.X >= r.X0 && msg.X < r.X1 {
					return ExecuteAction(m, r.Action)
				}
			}
			return nil
		}
		// History tab clicks
		if m.ActiveTab == model.TabHistory && len(m.HistoryItems) > 0 && msg.Y >= model.RowVPStart {
			idx := (msg.Y - model.RowVPStart) / 3
			if idx >= 0 && idx < len(m.HistoryItems) && (msg.Y-model.RowVPStart)%3 < 2 {
				if idx == m.HistoryIdx {
					browser.OpenURL(m.HistoryItems[idx].URL)
				} else {
					m.HistoryIdx = idx
				}
			}
			return nil
		}
		// Rules tab clicks
		if m.ActiveTab == model.TabRules && !m.RulesLoading {
			s := max(len(m.RulesData.Skip), 1)
			p := max(len(m.RulesData.Priority), 1)

			sec0InputY := 4
			sec1InputY := 7 + s
			sec2InputY := 10 + s + p

			sec0ItemsStart := 5
			sec1ItemsStart := 8 + s
			sec2ItemsStart := 11 + s + p

			y := msg.Y

			switch {
			case y == sec0InputY:
				m.RulesSection = 0
				m.RulesEditingIdx = -1
				m.RulesEditingSection = 0
				m.BlurAllRulesInputs()
				m.RulesFormFocus = model.RulesFieldSkip
				m.RulesSkipInput.Focus()
			case y == sec1InputY:
				m.RulesSection = 1
				m.RulesEditingIdx = -1
				m.RulesEditingSection = 1
				m.BlurAllRulesInputs()
				m.RulesFormFocus = model.RulesFieldPriority
				m.RulesPriorityInput.Focus()
			case y == sec2InputY:
				m.RulesSection = 2
				m.RulesEditingIdx = -1
				m.RulesEditingSection = 2
				m.BlurAllRulesInputs()
				m.RulesFormFocus = model.RulesFieldAliasKey
				m.RulesAliasKeyInput.Focus()
			case y >= sec0ItemsStart && y < sec0ItemsStart+len(m.RulesData.Skip) && len(m.RulesData.Skip) > 0:
				m.BlurAllRulesInputs()
				m.RulesFormFocus = model.RulesFieldList
				m.RulesSection = 0
				m.RulesIdx = y - sec0ItemsStart
			case y >= sec1ItemsStart && y < sec1ItemsStart+len(m.RulesData.Priority) && len(m.RulesData.Priority) > 0:
				m.BlurAllRulesInputs()
				m.RulesFormFocus = model.RulesFieldList
				m.RulesSection = 1
				m.RulesIdx = y - sec1ItemsStart
			case y >= sec2ItemsStart && y < sec2ItemsStart+len(m.RulesData.Aliases) && len(m.RulesData.Aliases) > 0:
				idx := y - sec2ItemsStart
				keys := m.SortedAliasKeys()
				if idx < len(keys) {
					m.BlurAllRulesInputs()
					m.RulesFormFocus = model.RulesFieldList
					m.RulesSection = 2
					m.RulesIdx = idx
				}
			}
			return nil
		}
		// Add tab clicks
		if m.ActiveTab == model.TabAdd {
			switch {
			case msg.Y == model.AddURLLabelY || msg.Y == model.AddURLInputY:
				if m.AddFocusIdx < len(m.AddInputs) {
					m.AddInputs[m.AddFocusIdx].Blur()
				}
				m.AddFocusIdx = 0
				m.AddInputs[0].Focus()
			case msg.Y == model.AddTitleLabelY || msg.Y == model.AddTitleInputY:
				if m.AddFocusIdx < len(m.AddInputs) {
					m.AddInputs[m.AddFocusIdx].Blur()
				}
				m.AddFocusIdx = 1
				m.AddInputs[1].Focus()
			case msg.Y == model.AddTextLabelY || msg.Y == model.AddTextInputY:
				if m.AddFocusIdx < len(m.AddInputs) {
					m.AddInputs[m.AddFocusIdx].Blur()
				}
				m.AddFocusIdx = 2
				m.AddInputs[2].Focus()
			case msg.Y == model.AddSubmitY:
				if m.AddFocusIdx < len(m.AddInputs) {
					m.AddInputs[m.AddFocusIdx].Blur()
				}
				m.AddFocusIdx = 3
				return submitAdd(m)
			}
			return nil
		}
	}
	// Scroll wheel in history tab
	if m.ActiveTab == model.TabHistory && len(m.HistoryItems) > 0 {
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			model.ScrollIdx(&m.HistoryIdx, -1, 0, len(m.HistoryItems)-1)
			return nil
		case tea.MouseButtonWheelDown:
			model.ScrollIdx(&m.HistoryIdx, 1, 0, len(m.HistoryItems)-1)
			return nil
		}
	}
	return nil
}

func submitAdd(m *model.Model) tea.Cmd {
	baseURL := m.Cfg.BaseURL("")
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

func mouseTabBar(m *model.Model, msg tea.MouseMsg) tea.Cmd {
	x := 1
	tabActions := []config.Action{config.ActionTabSearch, config.ActionTabHistory, config.ActionTabRules, config.ActionTabAdd}
	for i, name := range model.TabNames {
		labelW := len(name) + 2
		if msg.X >= x && msg.X < x+labelW {
			return SwitchTab(m, tabActions[i])
		}
		x += labelW + 1
	}
	return nil
}
