// SPDX-FileContributor: FlameFlag <github@flameflag.dev>
//
// SPDX-License-Identifier: AGPLv3+

package render

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/ui/model"

	"github.com/charmbracelet/lipgloss"
)

type overlayDef struct {
	content func(*model.Model) string
	border  func(*model.Model) lipgloss.Color
	offset  func(*model.Model) (int, int) // nil = use OverlayOff
}

var overlayDefs = map[model.ViewState]overlayDef{
	model.StateHelp:            {func(m *model.Model) string { return m.Styles.Help.Render(GenerateHelpText(m)) }, func(m *model.Model) lipgloss.Color { return m.Styles.HelpBorder }, nil},
	model.StateDialog:          {func(m *model.Model) string { return DeleteDialog(m) }, func(m *model.Model) lipgloss.Color { return m.Styles.DialogBorder }, nil},
	model.StateThemePicker:     {func(m *model.Model) string { return ThemePicker(m) }, func(m *model.Model) lipgloss.Color { return m.Styles.ThemeBorder }, nil},
	model.StateSettings:        {func(m *model.Model) string { return Settings(m) }, func(m *model.Model) lipgloss.Color { return m.Styles.HelpBorder }, nil},
	model.StateContextMenu:     {func(m *model.Model) string { return ContextMenu(m) }, func(m *model.Model) lipgloss.Color { return m.Styles.DialogBorder }, MenuOverlayOffset},
	model.StatePrioritizeInput: {func(m *model.Model) string { return PrioritizeInput(m) }, func(m *model.Model) lipgloss.Color { return m.Styles.DialogBorder }, nil},
}

func View(m *model.Model) string {
	if !m.Ready {
		return "Loading..."
	}
	if m.Width < 20 || m.Height < 10 {
		return "Terminal too small"
	}

	main := MainView(m)

	if def, ok := overlayDefs[m.State]; ok {
		maxW := overlayMaxWidth(m)
		fg := renderOverlayBox(def.content(m), def.border(m), maxW)
		offX, offY := m.OverlayOffX, m.OverlayOffY
		if def.offset != nil {
			offX, offY = def.offset(m)
		}
		return renderOverlay(main, fg, m.Width-1, m.Height, offX, offY)
	}
	return main
}

var tabRenderers = map[int]func(*model.Model) string{
	model.TabHistory: HistoryTab,
	model.TabRules:   RulesTab,
	model.TabAdd:     AddTab,
}

func MainView(m *model.Model) string {
	w := max(0, m.Width-1)
	div := m.Styles.Div.Render(strings.Repeat("─", w))

	header := Header(m)

	if renderer, ok := tabRenderers[m.ActiveTab]; ok {
		content := renderer(m)
		contentH := m.Viewport.Height + 2
		contentLines := strings.Split(content, "\n")
		for len(contentLines) < contentH {
			contentLines = append(contentLines, "")
		}
		if len(contentLines) > contentH {
			contentLines = contentLines[:contentH]
		}
		content = strings.Join(contentLines, "\n")
		hints := Hints(m)
		return strings.Join([]string{header, div, content, div, hints}, "\n")
	}

	pStyle := m.Styles.PromptActive
	if m.State != model.StateInput {
		pStyle = m.Styles.PromptBlur
	}
	inputLine := "  " + pStyle.Render("❯") + " " + m.TextInput.View()

	vp := m.Viewport.View()
	vpLines := strings.Split(vp, "\n")
	if len(vpLines) > m.Viewport.Height {
		vpLines = vpLines[:m.Viewport.Height]
	}
	for len(vpLines) < m.Viewport.Height {
		vpLines = append(vpLines, "")
	}
	vp = strings.Join(vpLines, "\n")

	if m.TotalLines > m.Viewport.Height && m.Viewport.Height > 0 {
		vp = lipgloss.JoinHorizontal(lipgloss.Top, vp, " ", Scrollbar(m))
	}

	hints := Hints(m)

	return strings.Join([]string{header, div, inputLine, div, vp, div, hints}, "\n")
}

func Header(m *model.Model) string {
	var tabs []string
	for i, name := range model.TabNames {
		if i == m.ActiveTab {
			tabs = append(tabs, m.Styles.TabActive.Render("["+name+"]"))
		} else {
			tabs = append(tabs, m.Styles.TabInactive.Render(" "+name+" "))
		}
	}
	tabBar := " " + strings.Join(tabs, " ")
	if m.SortMode == "domain" {
		tabBar += "  " + m.Styles.Conn.Render("[domain]")
	}

	cs := m.Styles.Disc.Render("● disconnected")
	if m.WsReady {
		cs = m.Styles.Conn.Render("● connected")
	}

	var right string
	if m.ConnError != nil && !m.WsReady {
		right = m.Styles.Disc.Render(m.ConnError.Error())
	} else if m.IsSearching {
		right = cs + "  " + m.Styles.Spin.Render(m.Spinner.View()+" searching…")
	} else {
		countStr := "0 results"
		if m.Results != nil {
			countStr = fmt.Sprintf("%d results", int(m.Results.Total))
			if m.Results.SearchDuration != "" {
				countStr += "  " + m.Results.SearchDuration
			}
		}
		right = cs + "  " + m.Styles.Status.Render(countStr)
	}

	w := max(1, m.Width-1)
	leftW := lipgloss.Width(tabBar)
	rightW := lipgloss.Width(right)
	pad := max(0, w-leftW-rightW)
	return tabBar + strings.Repeat(" ", pad) + right
}

type hintEntry struct {
	act config.Action // action name (or "" for fixed keys)
	key string        // pre-resolved key symbol (for fixed entries)
	lbl string
}

func hintEntries(m *model.Model) []hintEntry {
	bestKey := func(action config.Action) string {
		return BestKey(m.Cfg.Hotkeys.TUI, action)
	}
	switch m.ActiveTab {
	case model.TabHistory:
		var entries []hintEntry
		if bestKey(config.ActionScrollDown) != "" {
			entries = append(entries, hintEntry{act: config.ActionScrollDown, lbl: "navigate"})
		}
		if bestKey(config.ActionOpenResult) != "" {
			entries = append(entries, hintEntry{act: config.ActionOpenResult, lbl: "open"})
		}
		if bestKey(config.ActionDeleteResult) != "" {
			entries = append(entries, hintEntry{act: config.ActionDeleteResult, lbl: "delete"})
		}
		return append(entries, hintEntry{key: "⎋", lbl: "back"})
	case model.TabRules:
		entries := []hintEntry{{key: "⇥", lbl: "next"}}
		if bestKey(config.ActionOpenResult) != "" {
			entries = append(entries, hintEntry{act: config.ActionOpenResult, lbl: "add/save"})
		}
		if bestKey(config.ActionScrollDown) != "" {
			entries = append(entries, hintEntry{act: config.ActionScrollDown, lbl: "navigate"})
		}
		if bestKey(config.ActionDeleteResult) != "" {
			entries = append(entries, hintEntry{act: config.ActionDeleteResult, lbl: "delete"})
		}
		return append(entries, hintEntry{key: "⎋", lbl: "back"})
	case model.TabAdd:
		return []hintEntry{
			{key: "⇥", lbl: "next"},
			{key: "↵", lbl: "submit"},
			{key: "⎋", lbl: "back"},
		}
	default: // Search
		isNoColor := m.ThemeName == "no-color"
		var entries []hintEntry
		for _, a := range []struct {
			act config.Action
			lbl string
		}{
			{config.ActionScrollUp, "up"}, {config.ActionScrollDown, "down"},
			{config.ActionOpenResult, "open"}, {config.ActionDeleteResult, "delete"},
			{config.ActionToggleSort, "sort"}, {config.ActionToggleTheme, "theme"},
			{config.ActionToggleSettings, "settings"},
			{config.ActionToggleHelp, "help"}, {config.ActionQuit, "quit"},
		} {
			if isNoColor && a.act == config.ActionToggleTheme {
				continue
			}
			if bestKey(a.act) != "" {
				entries = append(entries, hintEntry{act: a.act, lbl: a.lbl})
			}
		}
		return entries
	}
}

func Hints(m *model.Model) string {
	entries := hintEntries(m)
	isNoColor := m.ThemeName == "no-color"
	sep := m.Styles.Hint.Render("  ·  ")
	if isNoColor {
		sep = "  |  "
	}
	var parts []string
	for _, e := range entries {
		k := e.key
		if k == "" {
			k = BestKey(m.Cfg.Hotkeys.TUI, e.act)
		}
		if k == "" {
			continue
		}
		isFlash := e.act != "" && m.HintFlash == e.act
		if isNoColor {
			if isFlash {
				parts = append(parts, "["+k+"] "+strings.ToUpper(e.lbl))
			} else {
				parts = append(parts, k+" "+e.lbl)
			}
		} else {
			keyRender := m.Styles.HintKey
			lblRender := m.Styles.Hint
			if isFlash {
				keyRender = m.Styles.HintKeyFlash
				lblRender = m.Styles.HintFlash
			}
			parts = append(parts, keyRender.Render(k)+lblRender.Render(" "+e.lbl))
		}
	}
	return "  " + strings.Join(parts, sep)
}

func overlayMaxWidth(m *model.Model) int {
	return max(20, m.Width-5)
}

// wraps content in a rounded border with drag handle and close button.
func renderOverlayBox(content string, borderColor lipgloss.Color, maxWidth int) string {
	lines := strings.Split(content, "\n")
	maxW := 0
	for _, l := range lines {
		if w := lipgloss.Width(l); w > maxW {
			maxW = w
		}
	}
	if maxWidth > 0 && maxW > maxWidth {
		maxW = maxWidth
		for i, l := range lines {
			if lipgloss.Width(l) > maxW {
				lines[i] = truncateAnsi(l, maxW)
			}
		}
	}

	bc := lipgloss.NewStyle().Foreground(borderColor)
	closeSt := lipgloss.NewStyle().Foreground(borderColor).Bold(true)

	handle := " ≡ "
	closeBtn := closeSt.Render("[x]")
	closeBtnW := lipgloss.Width(closeBtn)
	handleW := len([]rune(handle))
	barW := maxW
	leftHandleW := (barW - handleW) / 2
	rightHandleW := barW - handleW - leftHandleW - closeBtnW
	if rightHandleW < 0 {
		rightHandleW = 0
	}
	topBar := bc.Render("╭"+strings.Repeat("─", leftHandleW)+handle+strings.Repeat("─", rightHandleW)) + closeBtn + bc.Render("╮")
	bottomBar := bc.Render("╰" + strings.Repeat("─", maxW) + "╯")

	var sb strings.Builder
	sb.WriteString(topBar)
	for _, l := range lines {
		sb.WriteByte('\n')
		pad := max(0, maxW-lipgloss.Width(l))
		sb.WriteString(bc.Render("│") + l + strings.Repeat(" ", pad) + bc.Render("│"))
	}
	sb.WriteByte('\n')
	sb.WriteString(bottomBar)
	return sb.String()
}

// wraps a rendered line with faint ANSI codes.
// Returns the string unchanged in NO_COLOR mode.
func applyDim(s string) string {
	if s == "" || os.Getenv("NO_COLOR") != "" {
		return s
	}
	const dim = "\x1b[2m"
	const reset = "\x1b[0m"
	result := dim + strings.ReplaceAll(s, reset, reset+dim)
	return result + reset
}

func renderOverlay(bg, fg string, bgW, bgH, offX, offY int) string {
	bgLines := strings.Split(bg, "\n")
	for len(bgLines) < bgH {
		bgLines = append(bgLines, "")
	}

	fgLines := strings.Split(fg, "\n")
	fgH := len(fgLines)
	fgW := 0
	for _, l := range fgLines {
		if w := lipgloss.Width(l); w > fgW {
			fgW = w
		}
	}

	startY := max(0, min(bgH-fgH, (bgH-fgH)/2+offY))
	startX := max(0, min(bgW-fgW, (bgW-fgW)/2+offX))

	// Available width for overlay content
	availW := bgW - startX

	var sb strings.Builder
	for i := 0; i < bgH; i++ {
		if i > 0 {
			sb.WriteByte('\n')
		}
		bgLine := ""
		if i < len(bgLines) {
			bgLine = bgLines[i]
		}

		fgIdx := i - startY
		if fgIdx >= 0 && fgIdx < fgH {
			fgLine := fgLines[fgIdx]
			fgLineW := lipgloss.Width(fgLine)
			// Pad to full overlay width so right edge aligns consistently
			if fgLineW < fgW {
				fgLine += strings.Repeat(" ", fgW-fgLineW)
			}
			// Truncate if overlay exceeds available terminal width
			if fgW > availW {
				fgLine = truncateAnsi(fgLine, availW)
			}
			left := truncateAnsi(bgLine, startX)
			right := sliceAnsiFrom(bgLine, startX+fgW)
			if os.Getenv("NO_COLOR") != "" {
				sb.WriteString(left + fgLine + right)
			} else {
				sb.WriteString("\x1b[0m" + applyDim(left) + fgLine + applyDim(right))
			}
		} else {
			sb.WriteString(applyDim(bgLine))
		}
	}
	return sb.String()
}

func MenuOverlayOffset(m *model.Model) (int, int) {
	fg := renderOverlayBox(ContextMenu(m), m.Styles.DialogBorder, overlayMaxWidth(m))
	fgLines := strings.Split(fg, "\n")
	fgH := len(fgLines)
	fgW := 0
	for _, l := range fgLines {
		if w := lipgloss.Width(l); w > fgW {
			fgW = w
		}
	}
	bgW, bgH := m.Width-1, m.Height
	offX := m.MenuX - (bgW-fgW)/2
	offY := m.MenuY - (bgH-fgH)/2
	return offX, offY
}

func OverlayBounds(m *model.Model) (x, y, w, h int) {
	def, ok := overlayDefs[m.State]
	if !ok {
		return
	}
	fg := renderOverlayBox(def.content(m), def.border(m), overlayMaxWidth(m))
	fgLines := strings.Split(fg, "\n")
	h = len(fgLines)
	for _, l := range fgLines {
		if lw := lipgloss.Width(l); lw > w {
			w = lw
		}
	}
	bgW, bgH := m.Width-1, m.Height
	y = max(0, min(bgH-h, (bgH-h)/2+m.OverlayOffY))
	x = max(0, min(bgW-w, (bgW-w)/2+m.OverlayOffX))
	return
}

func GenerateHelpText(m *model.Model) string {
	bindings := make(map[string][]string)
	for k, v := range m.Cfg.Hotkeys.TUI {
		bindings[v] = append(bindings[v], k)
	}
	const helpKeyColW = 20
	fmtAct := func(action, label string) string {
		keys := bindings[action]
		if len(keys) == 0 {
			return ""
		}
		slices.Sort(keys)
		var formatted []string
		for _, k := range keys {
			formatted = append(formatted, FormatKey(k))
		}
		keyPart := m.Styles.HintKey.Render(strings.Join(formatted, ", "))
		lblPart := m.Styles.HelpAction.Render(label)
		padW := max(0, helpKeyColW-lipgloss.Width(keyPart))
		return "  " + keyPart + strings.Repeat(" ", padW) + " " + lblPart
	}
	lines := []string{m.Styles.HelpHeader.Render("Shortcuts:"), ""}
	sections := []struct {
		title string
		items []struct{ act, lbl string }
	}{
		{"General:", []struct{ act, lbl string }{
			{"quit", "Quit"}, {"toggle_help", "Toggle help"}, {"toggle_theme", "Toggle theme picker"},
			{"toggle_settings", "Toggle settings"}, {"toggle_sort", "Toggle sort mode"},
			{"tab_search", "Search tab"}, {"tab_history", "History tab"},
			{"tab_rules", "Rules tab"}, {"tab_add", "Add tab"},
		}},
		{"Search input:", []struct{ act, lbl string }{
			{"toggle_focus", "Focus results list"}, {"scroll_up", "Previous result"},
			{"scroll_down", "Next result"}, {"open_result", "Open result"},
			{"delete_result", "Delete result"},
		}},
		{"Results list:", []struct{ act, lbl string }{
			{"toggle_focus", "Back to input"}, {"scroll_up", "Navigate up"},
			{"scroll_down", "Navigate down"}, {"open_result", "Open selected"},
			{"delete_result", "Delete selected"},
		}},
	}
	for i, sec := range sections {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, m.Styles.HelpHeader.Render(sec.title))
		for _, a := range sec.items {
			if s := fmtAct(a.act, a.lbl); s != "" {
				lines = append(lines, s)
			}
		}
	}
	return strings.Join(lines, "\n")
}

func ComputeHintRegions(m *model.Model) []model.HintRegion {
	entries := hintEntries(m)
	isNoColor := m.ThemeName == "no-color"
	sep := m.Styles.Hint.Render("  ·  ")
	if isNoColor {
		sep = "  |  "
	}
	sepW := lipgloss.Width(sep)

	var regions []model.HintRegion
	x := 2
	first := true
	for _, e := range entries {
		k := e.key
		if k == "" {
			k = BestKey(m.Cfg.Hotkeys.TUI, e.act)
		}
		if k == "" {
			continue
		}
		if !first {
			x += sepW
		}
		first = false
		var hintW int
		if isNoColor {
			hintW = lipgloss.Width(k + " " + e.lbl)
		} else {
			keyRender := m.Styles.HintKey.Render(k)
			lblRender := m.Styles.Hint.Render(" " + e.lbl)
			hintW = lipgloss.Width(keyRender) + lipgloss.Width(lblRender)
		}
		action := e.act
		if action == "" {
			action = config.Action(e.lbl) // use label as action id for fixed keys
		}
		regions = append(regions, model.HintRegion{X0: x, X1: x + hintW, Action: action})
		x += hintW
	}
	return regions
}
