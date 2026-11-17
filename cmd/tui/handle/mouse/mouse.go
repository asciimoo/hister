// SPDX-FileContributor: 4evy <git@evy.pink>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package mouse

import (
	"github.com/asciimoo/nogon/cmd/tui/model"
	"github.com/asciimoo/nogon/cmd/tui/render"
	"github.com/asciimoo/nogon/config"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/pkg/browser"
	"github.com/rs/zerolog/log"
)

type Deps struct {
	ExecuteAction              func(*model.Model, config.Action) tea.Cmd
	SwitchTab                  func(*model.Model, config.Action) tea.Cmd
	StartSearch                func(*model.Model, ...tea.Cmd) tea.Cmd
	CloseOverlay               func(*model.Model) tea.Cmd
	SubmitAdd                  func(*model.Model) tea.Cmd
	CloseThemePickerWithRevert func(*model.Model) tea.Cmd
	PreviewTheme               func(*model.Model)
	CycleAppearance            func(*model.Model) tea.Cmd
	ExecuteContextMenuAction   func(*model.Model) tea.Cmd
	ReloadDetails              func(*model.Model) tea.Cmd
}

type Handler struct{ Deps }

func New(d Deps) *Handler { return &Handler{d} }

type action uint8

const (
	actionClick action = iota
	actionRelease
	actionMotion
	actionWheel
)

// Event normalizes Bubble Tea v2's typed mouse messages for the layout hit
// testing shared by tabs, overlays, and the results viewport.
type Event struct {
	tea.Mouse
	Action action
}

func newEvent(msg tea.MouseMsg) Event {
	e := Event{Mouse: msg.Mouse()}
	switch msg.(type) {
	case tea.MouseReleaseMsg:
		e.Action = actionRelease
	case tea.MouseMotionMsg:
		e.Action = actionMotion
	case tea.MouseWheelMsg:
		e.Action = actionWheel
	default:
		e.Action = actionClick
	}
	return e
}

type Region struct{ X, Y, W, H int }

func (r Region) Contains(msg Event) bool {
	return msg.X >= r.X && msg.X < r.X+r.W && msg.Y >= r.Y && msg.Y < r.Y+r.H
}

func (r Region) ContainsY(y int) bool {
	return y >= r.Y && y < r.Y+r.H
}

// --- helpers ---

func vpRegion(m *model.Model) Region {
	top := model.RowVPStart
	bottom := model.RowVPEnd(m.Height)
	return Region{X: 0, Y: top, W: m.Width, H: bottom - top + 1}
}

func scrollToPercent(m *model.Model, mouseY int) {
	vp := vpRegion(m)
	if vp.H <= 1 {
		return
	}
	maxScroll := m.Viewport.TotalLineCount() - m.Viewport.Height()
	if maxScroll <= 0 {
		return
	}
	relY := max(0, min(mouseY-vp.Y, vp.H-1))
	pct := float64(relY) / float64(vp.H-1)
	m.Viewport.SetYOffset(int(pct * float64(maxScroll)))
	contentY := m.Viewport.YOffset() + m.Viewport.Height()/2
	if idx := m.FindResultAtY(contentY); idx >= 0 {
		m.SelectedIdx = idx
	}
	render.RefreshViewport(m)
}

func wheelDelta(msg Event) int {
	switch msg.Button {
	case tea.MouseWheelUp:
		return -1
	case tea.MouseWheelDown:
		return 1
	default:
		return 0
	}
}

func isLeftClick(msg Event) bool {
	return msg.Action == actionClick && msg.Button == tea.MouseLeft
}

func isOverlayState(s model.ViewState) bool {
	switch s {
	case model.StateHelp, model.StateDialog, model.StateThemePicker,
		model.StateSettings, model.StateContextMenu, model.StatePrioritizeInput,
		model.StateLabelInput:
		return true
	default:
		return false
	}
}

// handleScroll applies a wheel event to idx (clamped to [lo, hi]) and calls
// after when the index changes. Returns (nil, true) if a wheel event was
// consumed, (nil, false) otherwise.
func handleScroll(msg Event, idx *int, lo, hi int, after func()) (tea.Cmd, bool) {
	delta := wheelDelta(msg)
	if delta == 0 {
		return nil, false
	}
	if lo <= hi {
		if model.ScrollIdx(idx, delta, lo, hi) && after != nil {
			after()
		}
	}
	return nil, true
}

// --- Handler methods ---

func (h *Handler) closeOverlayForState(m *model.Model) tea.Cmd {
	if m.State == model.StateThemePicker {
		return h.CloseThemePickerWithRevert(m)
	}
	return h.CloseOverlay(m)
}

func (h *Handler) hintRegions(m *model.Model, msg Event) tea.Cmd {
	regions := render.ComputeHintRegions(m)
	for _, r := range regions {
		if msg.X >= r.X0 && msg.X < r.X1 {
			return h.ExecuteAction(m, r.Action)
		}
	}
	return nil
}

// Handle is the main entry point for mouse events.
func (h *Handler) Handle(m *model.Model, msg tea.MouseMsg) tea.Cmd {
	event := newEvent(msg)
	if m.ScrollbarDragging {
		if event.Action == actionMotion {
			oldIdx := m.SelectedIdx
			scrollToPercent(m, event.Y)
			if m.State == model.StateDetails && oldIdx != m.SelectedIdx {
				m.DetailsFocused = false
				return h.ReloadDetails(m)
			}
			return nil
		}
		if event.Action == actionRelease {
			m.ScrollbarDragging = false
			return nil
		}
	}

	if isOverlayState(m.State) {
		return h.overlay(m, event)
	}
	if m.State == model.StateDetails {
		return h.details(m, event)
	}

	if m.ActiveTab != model.TabSearch {
		return h.nonSearchTab(m, event)
	}

	// Scrolling the results is a focus-changing interaction: selection and
	// keyboard routing must never disagree. Ignore wheel events over the input,
	// header, and hints instead of silently moving the result selection.
	if wheelDelta(event) != 0 && vpRegion(m).Contains(event) {
		m.FocusSearchResults()
		if cmd, ok := handleScroll(event, &m.SelectedIdx, 0, m.GetTotalResults()-1, func() {
			render.RefreshAndScroll(m)
		}); ok {
			return cmd
		}
	}

	if event.Action == actionClick && event.Button == tea.MouseRight {
		return rightClick(m, event)
	}

	if !isLeftClick(event) {
		return nil
	}

	if event.Y == model.RowTabBar {
		return h.tabBar(m, event)
	}
	if event.Y == model.RowInput {
		return inputRow(m, event)
	}
	if event.Y == model.RowHints(m.Height) {
		return h.hintRegions(m, event)
	}
	if m.Viewport.TotalLineCount() > m.Viewport.Height() && m.Viewport.Height() > 0 && event.X >= m.Width-model.ScrollbarWidth {
		return scrollbarClick(m, event)
	}
	return h.viewportClick(m, event)
}

func (h *Handler) details(m *model.Model, event Event) tea.Cmd {
	paneX, paneY, paneW, _ := render.DetailsPaneBounds(m)
	inPane := event.X >= paneX && event.X < paneX+paneW
	if delta := wheelDelta(event); delta != 0 {
		if !vpRegion(m).ContainsY(event.Y) {
			return nil
		}
		if inPane || !render.DetailsSplit(m) {
			m.DetailsFocused = true
			if delta < 0 {
				m.Details.ScrollUp(3)
			} else {
				m.Details.ScrollDown(3)
			}
			return nil
		}
		m.DetailsFocused = false
		maxIdx := m.GetTotalResults() - 1
		if maxIdx == m.Limit {
			maxIdx--
		}
		if model.ScrollIdx(&m.SelectedIdx, delta, 0, maxIdx) {
			render.RefreshAndScroll(m)
			return h.ReloadDetails(m)
		}
		return nil
	}
	if event.Action == actionClick && event.Button == tea.MouseRight && render.DetailsSplit(m) && !inPane {
		oldIdx := m.SelectedIdx
		cmd := rightClick(m, event)
		if oldIdx != m.SelectedIdx {
			m.DetailsFocused = false
			return tea.Batch(cmd, h.ReloadDetails(m))
		}
		return cmd
	}
	if !isLeftClick(event) {
		return nil
	}
	if event.Y == model.RowTabBar {
		return h.tabBar(m, event)
	}
	if event.Y == model.RowInput {
		closeCmd := h.CloseOverlay(m)
		focusCmd := inputRow(m, event)
		return tea.Batch(closeCmd, focusCmd)
	}
	if event.Y == model.RowHints(m.Height) {
		return h.hintRegions(m, event)
	}
	if inPane {
		if event.Y == paneY && event.X == paneX+paneW-1 {
			return h.CloseOverlay(m)
		}
		m.DetailsFocused = true
		return nil
	}
	if !render.DetailsSplit(m) {
		return nil
	}
	if m.Viewport.TotalLineCount() > m.Viewport.Height() && m.Viewport.Height() > 0 && event.X == paneX-1 {
		oldIdx := m.SelectedIdx
		cmd := scrollbarClick(m, event)
		if oldIdx != m.SelectedIdx {
			m.DetailsFocused = false
			return tea.Batch(cmd, h.ReloadDetails(m))
		}
		return cmd
	}
	return h.detailsResultClick(m, event)
}

func (h *Handler) detailsResultClick(m *model.Model, event Event) tea.Cmd {
	vp := vpRegion(m)
	if !vp.ContainsY(event.Y) || len(m.LineOffsets) == 0 {
		return nil
	}
	contentY := event.Y - vp.Y + m.Viewport.YOffset()
	idx := m.FindResultAtY(contentY)
	if idx < 0 || idx >= m.GetTotalResults() || idx == m.Limit {
		return nil
	}
	m.DetailsFocused = false
	if idx == m.SelectedIdx {
		return nil
	}
	m.SelectedIdx = idx
	render.RefreshAndScroll(m)
	return h.ReloadDetails(m)
}

// --- search-tab handlers ---

func rightClick(m *model.Model, msg Event) tea.Cmd {
	vp := vpRegion(m)
	if !vp.ContainsY(msg.Y) || len(m.LineOffsets) == 0 {
		return nil
	}
	contentY := (msg.Y - vp.Y) + m.Viewport.YOffset()
	idx := m.FindResultAtY(contentY)
	if idx < 0 || idx >= m.GetTotalResults() || idx == m.Limit {
		return nil
	}
	m.FocusSearchResults()
	m.SelectedIdx = idx
	render.RefreshViewport(m)
	offX, offY := render.MenuOverlayOffset(m)
	m.OpenContextMenu(idx, msg.X, msg.Y, offX, offY)
	return nil
}

func inputRow(m *model.Model, msg Event) tea.Cmd {
	m.State = model.StateInput
	prefixW := model.InputLeadingPad + lipgloss.Width("❯") + model.InputTrailingPad
	pos := min(max(msg.X-prefixW, 0), len([]rune(m.TextInput.Value())))
	m.TextInput.SetCursor(pos)
	return m.TextInput.Focus()
}

func scrollbarClick(m *model.Model, msg Event) tea.Cmd {
	vp := vpRegion(m)
	if vp.ContainsY(msg.Y) {
		m.FocusSearchResults()
		m.ScrollbarDragging = true
		scrollToPercent(m, msg.Y)
	}
	return nil
}

func (h *Handler) viewportClick(m *model.Model, msg Event) tea.Cmd {
	vp := vpRegion(m)
	if !vp.ContainsY(msg.Y) || len(m.LineOffsets) == 0 {
		return nil
	}
	contentY := (msg.Y - vp.Y) + m.Viewport.YOffset()
	if m.SuggestionHeight > 0 && contentY < m.SuggestionHeight && m.Results != nil && m.Results.QuerySuggestion != "" {
		m.TextInput.SetValue(m.Results.QuerySuggestion)
		m.TextInput.SetCursor(len([]rune(m.Results.QuerySuggestion)))
		m.SelectedIdx = -1
		m.Limit = model.ResultsPageSize
		return h.StartSearch(m)
	}
	idx := m.FindResultAtY(contentY)
	if idx < 0 || idx >= m.GetTotalResults() {
		return nil
	}
	m.FocusSearchResults()
	if idx == m.SelectedIdx {
		if m.SelectedIdx == m.Limit {
			m.Limit += model.ResultsPageSize
			render.RefreshAndScroll(m)
			return h.StartSearch(m)
		} else if u := m.GetSelectedURL(); u != "" {
			if err := browser.OpenURL(u); err != nil {
				log.Warn().Err(err).Msg("failed to open URL in browser")
			}
			return m.PostHistoryCmd(u)
		}
	} else {
		m.SelectedIdx = idx
		render.RefreshAndScroll(m)
	}
	return nil
}

// --- shared ---

func (h *Handler) tabBar(m *model.Model, msg Event) tea.Cmd {
	for _, target := range m.TabTargets {
		if msg.X >= target.X0 && msg.X < target.X1 {
			return h.SwitchTab(m, target.Action)
		}
	}
	return nil
}
