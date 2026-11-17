// SPDX-FileContributor: 4evy <git@evy.pink>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package mouse

import (
	"strings"

	"github.com/asciimoo/nogon/cmd/tui/model"
	"github.com/asciimoo/nogon/cmd/tui/render"
	"github.com/asciimoo/nogon/cmd/tui/theme"

	tea "charm.land/bubbletea/v2"
)

// Overlay close button layout
const (
	closeBtnWidth  = 3 // width of "[x]" button
	closeBtnOffset = 4 // right edge to close btn start (btn + border)
)

const (
	settingsAppearanceRowY = 3
	settingsListStartY     = 6
)

// Theme picker layout (relative to overlay origin)
const (
	themeModeRowY        = 2 // relative Y of mode row
	themeModeLeftPad     = 3 // overlay border + content indent
	themeModeLabelStartX = 6 // X where mode labels begin (len("Mode: "))
	themeModeLabelPad    = 2 // padding around each mode label
	themeModeGap         = 1 // space between mode labels
	themeSectionOffset   = 2 // gap from mode row to first theme list header
	themeListGap         = 1 // blank line between dark and light lists
)

func (h *Handler) overlay(m *model.Model, msg Event) tea.Cmd {
	if m.IsDragging && msg.Action == actionMotion {
		m.OverlayOffX = min(m.Width/2, max(-m.Width/2, m.DragOffX0+(msg.X-m.DragStartX)))
		m.OverlayOffY = min(m.Height/2, max(-m.Height/2, m.DragOffY0+(msg.Y-m.DragStartY)))
		return nil
	}
	if m.IsDragging && msg.Action == actionRelease {
		m.IsDragging = false
		return nil
	}
	if isLeftClick(msg) {
		return h.overlayClick(m, msg)
	}
	switch m.State {
	case model.StateThemePicker:
		return h.themePickerScroll(m, msg)
	case model.StateSettings:
		return settingsScroll(m, msg)
	}
	return nil
}

func (h *Handler) overlayClick(m *model.Model, msg Event) tea.Cmd {
	ox, oy, ow, oh := render.OverlayBounds(m)

	titleBar := Region{ox, oy, ow, 1}
	if titleBar.Contains(msg) {
		closeBtn := Region{ox + ow - closeBtnOffset, oy, closeBtnWidth, 1}
		if closeBtn.Contains(msg) {
			return h.closeOverlayForState(m)
		}
		m.StartDrag(msg.X, msg.Y)
		return nil
	}

	body := Region{ox, oy, ow, oh}
	if body.Contains(msg) {
		switch m.State {
		case model.StateThemePicker:
			return themePickerInside(m, msg, ox, oy)
		case model.StateContextMenu:
			return h.overlayContextMenu(m, msg, oy)
		case model.StateDialog:
			return overlayDialog(m, msg, ox, oy, ow)
		case model.StatePrioritizeInput:
			return overlayPrioritize(m, msg, ox, oy, ow)
		case model.StateSettings:
			return h.settingsInside(m, msg, oy)
		}
		return nil
	}

	return h.closeOverlayForState(m)
}

func (h *Handler) settingsInside(m *model.Model, msg Event, oy int) tea.Cmd {
	relY := msg.Y - oy
	if relY == settingsAppearanceRowY {
		if m.SettingsIdx == 0 {
			return h.CycleAppearance(m)
		}
		m.SettingsIdx = 0
		return nil
	}
	if relY >= settingsListStartY && relY < settingsListStartY+m.SettingsCount {
		idx := 1 + m.SettingsStart + relY - settingsListStartY
		if idx == m.SettingsIdx {
			m.SettingsEditMode = true
			m.SettingsEditErr = ""
		} else {
			m.SettingsIdx = idx
		}
	}
	return nil
}

// --- overlay click handlers ---

func (h *Handler) overlayContextMenu(m *model.Model, msg Event, oy int) tea.Cmd {
	relY := msg.Y - oy
	optStartY := model.OverlayBorderRows + model.OverlayPaddingRows
	optIdx := relY - optStartY
	if optIdx >= 0 && optIdx < len(model.MenuOptions) {
		m.MenuSelIdx = optIdx
		return h.ExecuteContextMenuAction(m)
	}
	return nil
}

func overlayDialog(m *model.Model, msg Event, ox, oy, ow int) tea.Cmd {
	if msg.Y-oy != model.DialogBtnRowY() {
		return nil
	}
	var cmd tea.Cmd
	if msg.X-ox >= ow/2 && m.DialogConfirm != nil {
		m.DialogBtnIdx = 1
		cmd = m.DialogConfirm()
	}
	m.DialogConfirm = nil
	m.DismissDialog()
	return cmd
}

func overlayPrioritize(m *model.Model, msg Event, ox, oy, ow int) tea.Cmd {
	if msg.Y-oy != model.PrioritizeBtnRowY() {
		return nil
	}
	m.PrioritizeInput.Blur()
	m.DismissOverlay()
	if msg.X-ox >= ow/2 {
		m.PrioritizeBtnIdx = 1
		if pattern := strings.TrimSpace(m.PrioritizeInput.Value()); pattern != "" {
			return m.PrioritizeRuleCmd(pattern)
		}
	}
	return nil
}

// --- theme picker / settings internals ---

func themePickerInside(m *model.Model, msg Event, ox, oy int) tea.Cmd {
	relY := msg.Y - oy

	if relY == themeModeRowY {
		relX := msg.X - ox - themeModeLeftPad
		cur := themeModeLabelStartX
		for _, mode := range theme.ColorSchemeModes {
			labelW := len(mode) + themeModeLabelPad
			if relX >= cur && relX < cur+labelW {
				m.ThemePickerMode = mode
				m.Cfg.TUI.ColorScheme = mode
				p, _ := theme.ResolvePalette(&m.Cfg.TUI, m.IsDarkBg)
				m.ApplyTheme(p)
				render.RefreshViewport(m)
				return nil
			}
			cur += labelW + themeModeGap
		}
	} else {
		darkNames, lightNames := theme.ClassifyThemes()
		darkHeaderY := themeModeRowY + themeSectionOffset
		darkListStartY := darkHeaderY + 1
		lightHeaderY := darkListStartY + m.ThemeDarkCount + themeListGap
		lightListStartY := lightHeaderY + 1

		type themeList struct {
			items   Region
			names   []string
			start   int
			section int
			idx     *int
		}
		lists := [...]themeList{
			{Region{Y: darkListStartY, H: m.ThemeDarkCount}, darkNames, m.ThemeDarkStart, 0, &m.DarkThemeIdx},
			{Region{Y: lightListStartY, H: m.ThemeLightCount}, lightNames, m.ThemeLightStart, 1, &m.LightThemeIdx},
		}
		for _, l := range lists {
			if l.items.ContainsY(relY) {
				idx := l.start + relY - l.items.Y
				m.ThemePickerSection = l.section
				*l.idx = idx
				leaveTerminalModeForThemeList(m)
				if p, ok := theme.GetPalette(l.names[idx]); ok {
					m.ApplyTheme(p)
					render.RefreshViewport(m)
				}
				return nil
			}
		}
	}
	return nil
}

func (h *Handler) themePickerScroll(m *model.Model, msg Event) tea.Cmd {
	if wheelDelta(msg) == 0 {
		return nil
	}
	darkNames, lightNames := theme.ClassifyThemes()
	wasTerminal := m.ThemePickerMode == theme.TerminalName
	leaveTerminalModeForThemeList(m)
	if m.ThemePickerSection == 0 {
		oldIdx := m.DarkThemeIdx
		handleScroll(msg, &m.DarkThemeIdx, 0, len(darkNames)-1, func() { h.PreviewTheme(m) })
		if wasTerminal && oldIdx == m.DarkThemeIdx {
			h.PreviewTheme(m)
		}
	} else {
		oldIdx := m.LightThemeIdx
		handleScroll(msg, &m.LightThemeIdx, 0, len(lightNames)-1, func() { h.PreviewTheme(m) })
		if wasTerminal && oldIdx == m.LightThemeIdx {
			h.PreviewTheme(m)
		}
	}
	return nil
}

func settingsScroll(m *model.Model, msg Event) tea.Cmd {
	handleScroll(msg, &m.SettingsIdx, 0, len(m.Cfg.Hotkeys.TUI), nil)
	return nil
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
