// SPDX-FileContributor: 4evy <git@evy.pink>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package tui

import (
	"image/color"
	"maps"
	"testing"

	"github.com/asciimoo/nogon/cmd/tui/model"
	"github.com/asciimoo/nogon/cmd/tui/theme"
	"github.com/asciimoo/nogon/config"

	tea "charm.land/bubbletea/v2"
)

func testApp(t *testing.T) *app {
	t.Helper()
	cfg := config.CreateDefaultConfig()
	cfg.TUI = config.DefaultTUIConfig
	cfg.Hotkeys.TUI = maps.Clone(config.DefaultTUIHotkeys)
	return &app{m: model.InitialModel(cfg)}
}

func TestViewDeclaresTerminalFeatures(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	a := testApp(t)
	// Keep this test independent of the configured default appearance. The
	// View contract is that a named theme declares its resolved screen colors.
	a.m.Cfg.TUI.ColorScheme = "dark"
	a.m.ThemeName = "test-theme"
	a.m.BackgroundColor = color.Black
	a.m.ForegroundColor = color.White
	v := a.View()

	if !v.AltScreen {
		t.Fatal("view did not request the alternate screen")
	}
	if v.MouseMode != tea.MouseModeCellMotion {
		t.Fatalf("mouse mode = %v, want cell motion", v.MouseMode)
	}
	if v.BackgroundColor == nil || v.ForegroundColor == nil {
		t.Fatal("themed view did not declare terminal colors")
	}
}

func TestNoColorViewLeavesTerminalColorsUnset(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	a := testApp(t)
	v := a.View()

	if v.BackgroundColor != nil || v.ForegroundColor != nil {
		t.Fatalf("no-color view declared terminal colors: background=%v foreground=%v", v.BackgroundColor, v.ForegroundColor)
	}
}

func TestTerminalAppearanceLeavesTerminalColorsUnset(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	a := testApp(t)
	a.m.Ready = true
	a.m.Width, a.m.Height = 80, 24
	v := a.View()

	if a.m.ThemeName != theme.TerminalName {
		t.Fatalf("theme = %q, want terminal", a.m.ThemeName)
	}
	if v.BackgroundColor != nil || v.ForegroundColor != nil {
		t.Fatalf("terminal view declared screen colors: background=%v foreground=%v", v.BackgroundColor, v.ForegroundColor)
	}
	if v.WindowTitle != "" {
		t.Fatalf("TUI took ownership of shell-managed window title: %q", v.WindowTitle)
	}
}

func TestFullThemeNamedTerminalStillDeclaresScreenColors(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	a := testApp(t)
	a.m.Cfg.TUI.ColorScheme = "dark"
	a.m.ThemeName = theme.TerminalName
	a.m.BackgroundColor = color.Black
	a.m.ForegroundColor = color.White
	v := a.View()

	if v.BackgroundColor == nil || v.ForegroundColor == nil {
		t.Fatal("full theme named terminal did not declare screen colors")
	}
}
