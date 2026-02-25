// SPDX-FileContributor: FlameFlag <github@flameflag.dev>
//
// SPDX-License-Identifier: AGPLv3+

package render

import (
	"strings"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/ui/model"
	"github.com/asciimoo/hister/ui/theme"

	"github.com/charmbracelet/lipgloss"
)

func ThemePicker(m *model.Model) string {
	darkNames, lightNames := theme.ClassifyThemes()

	maxNameW := 0
	for _, name := range append(darkNames, lightNames...) {
		if len(name) > maxNameW {
			maxNameW = len(name)
		}
	}

	modes := []string{"auto", "dark", "light"}
	var modeParts []string
	for _, mode := range modes {
		if mode == m.ThemePickerMode {
			modeParts = append(modeParts, m.Styles.ThemePickerSelected.Render("["+mode+"]"))
		} else {
			modeParts = append(modeParts, m.Styles.Gray.Render(" "+mode+" "))
		}
	}
	modeRow := "Mode: " + strings.Join(modeParts, " ")

	renderSection := func(names []string, cursorIdx int, configuredName string, focused bool) []string {
		var slines []string
		for i, name := range names {
			marker := "  "
			if name == configuredName {
				marker = "● "
			}
			paddedName := name + strings.Repeat(" ", maxNameW-len(name))
			swatch := ""
			if p, ok := theme.GetPalette(name); ok {
				swatch = renderSwatch(p)
			}
			content := marker + paddedName + "  " + swatch
			if focused && i == cursorIdx {
				slines = append(slines, m.Styles.ThemePickerSelected.Render(content))
			} else {
				slines = append(slines, m.Styles.ThemePickerItem.Render(content))
			}
		}
		return slines
	}

	var lines []string
	lines = append(lines, modeRow, "")

	darkFocused := m.ThemePickerSection == 0
	headerStyle := m.Styles.Gray
	if darkFocused {
		headerStyle = m.Styles.SelTitle
	}
	lines = append(lines, headerStyle.Render("Dark Themes"))
	lines = append(lines, renderSection(darkNames, m.DarkThemeIdx, m.Cfg.TUI.DarkTheme, darkFocused)...)
	lines = append(lines, "")

	lightFocused := m.ThemePickerSection == 1
	headerStyle = m.Styles.Gray
	if lightFocused {
		headerStyle = m.Styles.SelTitle
	}
	lines = append(lines, headerStyle.Render("Light Themes"))
	lines = append(lines, renderSection(lightNames, m.LightThemeIdx, m.Cfg.TUI.LightTheme, lightFocused)...)

	lines = append(lines, "")
	nav := BestKey(m.Cfg.Hotkeys.TUI, config.ActionScrollDown)
	mode := BestKey(m.Cfg.Hotkeys.TUI, config.ActionToggleTheme)
	confirm := BestKey(m.Cfg.Hotkeys.TUI, config.ActionOpenResult)
	themeHints := nav + " navigate  ⇥ section  " + mode + " mode  " + confirm + " confirm  ⎋ cancel"
	lines = append(lines, m.Styles.Hint.Render(themeHints))
	return m.Styles.ThemePicker.Render(strings.Join(lines, "\n"))
}

// ContextMenu renders a small context menu box.
func ContextMenu(m *model.Model) string {
	var lines []string
	for i, opt := range model.MenuOptionLabels {
		if i == m.MenuSelIdx {
			lines = append(lines, m.Styles.ThemePickerSelected.Render("▸ "+opt))
		} else {
			lines = append(lines, m.Styles.ThemePickerItem.Render("  "+opt))
		}
	}
	return m.Styles.Dialog.Render(strings.Join(lines, "\n"))
}

func DeleteDialog(m *model.Model) string {
	var lines []string
	lines = append(lines, m.Styles.Title.Render(m.DialogMsg))
	lines = append(lines, "")
	urlDisplay := m.DialogURL
	if len([]rune(urlDisplay)) > 35 {
		urlDisplay = string([]rune(urlDisplay)[:34]) + "…"
	}
	lines = append(lines, m.Styles.URL.Render(urlDisplay))
	lines = append(lines, "")
	cancelLabel := " Cancel "
	deleteLabel := " Delete "
	var cancelBtn, deleteBtn string
	if m.DialogBtnIdx == 0 {
		cancelBtn = m.Styles.CancelBtnSel.Render(cancelLabel)
	} else {
		cancelBtn = m.Styles.CancelBtn.Render(cancelLabel)
	}
	if m.DialogBtnIdx == 1 {
		deleteBtn = m.Styles.DeleteBtnSel.Render(deleteLabel)
	} else {
		deleteBtn = m.Styles.DeleteBtn.Render(deleteLabel)
	}
	lines = append(lines, cancelBtn+"   "+deleteBtn)
	lines = append(lines, "")
	lines = append(lines, m.Styles.Hint.Render("←/→ select  ↵ confirm  esc cancel"))
	return m.Styles.Dialog.Render(strings.Join(lines, "\n"))
}

func Settings(m *model.Model) string {
	items := m.SortedSettingsItems()

	maxKeyW := 0
	for _, it := range items {
		fk := FormatKey(it.Key)
		if len([]rune(fk)) > maxKeyW {
			maxKeyW = len([]rune(fk))
		}
	}

	var lines []string
	lines = append(lines, m.Styles.Title.Render("Keybindings"))
	lines = append(lines, "")
	for i, it := range items {
		if i == m.SettingsIdx && m.SettingsEditMode {
			lines = append(lines, m.Styles.ThemePickerSelected.Render("  Press a key...  →  "+string(it.Action)))
		} else {
			fk := FormatKey(it.Key)
			padded := fk + strings.Repeat(" ", maxKeyW-len([]rune(fk)))
			row := "  " + padded + "  →  " + string(it.Action)
			if i == m.SettingsIdx {
				lines = append(lines, m.Styles.ThemePickerSelected.Render(row))
			} else {
				lines = append(lines, m.Styles.ThemePickerItem.Render(row))
			}
		}
	}
	if m.SettingsEditErr != "" {
		lines = append(lines, "")
		lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color(m.Styles.DialogBorder)).Render("  "+m.SettingsEditErr))
	}
	lines = append(lines, "")
	if m.SettingsEditMode {
		lines = append(lines, m.Styles.Hint.Render("press any key to bind  esc cancel"))
	} else {
		sNav := BestKey(m.Cfg.Hotkeys.TUI, config.ActionScrollDown)
		sEdit := BestKey(m.Cfg.Hotkeys.TUI, config.ActionOpenResult)
		lines = append(lines, m.Styles.Hint.Render(sNav+" navigate  "+sEdit+" edit  ⎋ close"))
	}
	return m.Styles.Help.Render(strings.Join(lines, "\n"))
}

func PrioritizeInput(m *model.Model) string {
	var lines []string
	lines = append(lines, m.Styles.Title.Render("Add Priority Pattern"))
	lines = append(lines, "")
	lines = append(lines, m.Styles.Gray.Render("Pattern:"))
	lines = append(lines, "  "+m.PrioritizeInput.View())
	lines = append(lines, "")
	cancelLabel := " Cancel "
	confirmLabel := " Confirm "
	var cancelBtn, confirmBtn string
	if m.PrioritizeBtnIdx == 0 {
		cancelBtn = m.Styles.CancelBtnSel.Render(cancelLabel)
	} else {
		cancelBtn = m.Styles.CancelBtn.Render(cancelLabel)
	}
	if m.PrioritizeBtnIdx == 1 {
		confirmBtn = m.Styles.ConfirmBtnSel.Render(confirmLabel)
	} else {
		confirmBtn = m.Styles.ConfirmBtn.Render(confirmLabel)
	}
	lines = append(lines, cancelBtn+"   "+confirmBtn)
	lines = append(lines, "")
	lines = append(lines, m.Styles.Hint.Render("←/→ select  ↵ confirm  esc cancel"))
	return m.Styles.Dialog.Render(strings.Join(lines, "\n"))
}

func renderSwatch(p theme.Palette) string {
	colors := []string{p.Base01, p.Base08, p.Base09, p.Base0A, p.Base0B, p.Base0C, p.Base0D, p.Base0E}
	var sb strings.Builder
	for _, hex := range colors {
		sb.WriteString(lipgloss.NewStyle().Background(lipgloss.Color(hex)).Render("  "))
	}
	return sb.String()
}

func RefreshViewport(m *model.Model) {
	if m.Ready {
		m.Viewport.SetContent(Results(m))
	}
}
