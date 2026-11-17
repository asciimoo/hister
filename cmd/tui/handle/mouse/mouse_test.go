// SPDX-FileContributor: 4evy <git@evy.pink>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package mouse

import (
	"maps"
	"testing"

	"github.com/asciimoo/nogon/cmd/tui/model"
	"github.com/asciimoo/nogon/cmd/tui/theme"
	"github.com/asciimoo/nogon/config"

	tea "charm.land/bubbletea/v2"
)

func TestNewEventPreservesTypedMouseMessages(t *testing.T) {
	tests := []struct {
		name       string
		msg        tea.MouseMsg
		wantAction action
		wantButton tea.MouseButton
	}{
		{name: "click", msg: tea.MouseClickMsg{X: 3, Y: 4, Button: tea.MouseLeft}, wantAction: actionClick, wantButton: tea.MouseLeft},
		{name: "release", msg: tea.MouseReleaseMsg{X: 3, Y: 4, Button: tea.MouseLeft}, wantAction: actionRelease, wantButton: tea.MouseLeft},
		{name: "motion", msg: tea.MouseMotionMsg{X: 3, Y: 4, Button: tea.MouseLeft}, wantAction: actionMotion, wantButton: tea.MouseLeft},
		{name: "wheel", msg: tea.MouseWheelMsg{X: 3, Y: 4, Button: tea.MouseWheelDown}, wantAction: actionWheel, wantButton: tea.MouseWheelDown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := newEvent(tt.msg)
			if event.X != 3 || event.Y != 4 || event.Button != tt.wantButton || event.Action != tt.wantAction {
				t.Fatalf("newEvent() = %#v, want position (3,4), button %v, action %v", event, tt.wantButton, tt.wantAction)
			}
		})
	}
}

func mouseTestModel(t *testing.T) *model.Model {
	t.Helper()
	t.Setenv("NO_COLOR", "")
	cfg := config.CreateDefaultConfig()
	cfg.TUI = config.DefaultTUIConfig
	cfg.Hotkeys.TUI = maps.Clone(config.DefaultTUIHotkeys)
	return model.InitialModel(cfg)
}

func TestThemePickerWheelLeavesTerminalModeAtListEdge(t *testing.T) {
	m := mouseTestModel(t)
	darkNames, _ := theme.ClassifyThemes()
	if len(darkNames) == 0 {
		t.Fatal("no dark themes available")
	}
	m.ThemePickerMode = theme.TerminalName
	m.ThemePickerSection = 0
	m.DarkThemeIdx = len(darkNames) - 1
	previews := 0
	h := &Handler{Deps: Deps{PreviewTheme: func(*model.Model) { previews++ }}}

	h.themePickerScroll(m, Event{
		Mouse:  tea.Mouse{Button: tea.MouseWheelDown},
		Action: actionWheel,
	})

	if m.ThemePickerMode != "dark" || m.Cfg.TUI.ColorScheme != "dark" {
		t.Fatalf("wheel left mode = %q/%q, want dark", m.ThemePickerMode, m.Cfg.TUI.ColorScheme)
	}
	if previews != 1 {
		t.Fatalf("wheel preview count = %d, want 1", previews)
	}
}

func TestThemePickerMouseSelectsTerminalMode(t *testing.T) {
	m := mouseTestModel(t)
	m.ThemePickerMode = "auto"
	m.Cfg.TUI.ColorScheme = "auto"

	themePickerInside(m, Event{
		Mouse: tea.Mouse{
			X: themeModeLeftPad + themeModeLabelStartX,
			Y: themeModeRowY,
		},
		Action: actionClick,
	}, 0, 0)

	if m.ThemePickerMode != theme.TerminalName || m.ThemeName != theme.TerminalName {
		t.Fatalf("mouse selected mode = %q/%q, want terminal", m.ThemePickerMode, m.ThemeName)
	}
}

func TestSettingsAppearanceRowCyclesWhenSelected(t *testing.T) {
	m := mouseTestModel(t)
	m.SettingsIdx = 0
	cycles := 0
	h := &Handler{Deps: Deps{CycleAppearance: func(*model.Model) tea.Cmd {
		cycles++
		return nil
	}}}

	h.settingsInside(m, Event{Mouse: tea.Mouse{Y: settingsAppearanceRowY}, Action: actionClick}, 0)

	if cycles != 1 {
		t.Fatalf("appearance cycle count = %d, want 1", cycles)
	}
}
