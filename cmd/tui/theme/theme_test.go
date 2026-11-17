// SPDX-FileContributor: 4evy <git@evy.pink>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package theme

import (
	"reflect"
	"strings"
	"testing"

	"github.com/asciimoo/nogon/config"

	"charm.land/lipgloss/v2"
)

func TestTerminalPaletteInheritsBaseColorsAndBackgrounds(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	p, name := ResolvePalette(&config.TUI{ColorScheme: TerminalName}, true)
	if name != TerminalName || p.Name != TerminalName {
		t.Fatalf("resolved palette = %q/%q, want terminal", name, p.Name)
	}

	styles := BuildStyles(p)
	styleType := reflect.TypeFor[lipgloss.Style]()
	stylesValue := reflect.ValueOf(styles)
	stylesType := stylesValue.Type()
	for i := range stylesValue.NumField() {
		if stylesType.Field(i).Type != styleType {
			continue
		}
		style := stylesValue.Field(i).Interface().(lipgloss.Style)
		if background := style.GetBackground(); !isNoColor(background) {
			t.Errorf("%s background = %T, want lipgloss.NoColor", stylesType.Field(i).Name, background)
		}
	}
	if foreground := styles.Title.GetForeground(); !isNoColor(foreground) {
		t.Errorf("normal title foreground = %T, want lipgloss.NoColor", foreground)
	}

	// Semantic accents remain ANSI colors, which means the terminal profile
	// chooses their actual RGB values.
	accent := styles.SelTitle.Render("selected")
	if !strings.Contains(accent, "\x1b[") {
		t.Fatalf("selected style emitted no ANSI accent: %q", accent)
	}
}

func isNoColor(value any) bool {
	_, ok := value.(lipgloss.NoColor)
	return ok
}

func TestNoColorOverridesTerminalMode(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	_, name := ResolvePalette(&config.TUI{ColorScheme: TerminalName}, true)
	if name != "no-color" {
		t.Fatalf("resolved palette = %q, want no-color", name)
	}
}

func TestColorSchemeModeCycleIncludesTerminal(t *testing.T) {
	want := []string{"terminal", "auto", "dark", "light", "terminal"}
	got := []string{ColorSchemeModes[0]}
	for range 4 {
		got = append(got, NextColorSchemeMode(got[len(got)-1]))
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("mode cycle = %v, want %v", got, want)
	}
}
