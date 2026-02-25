// SPDX-FileContributor: FlameFlag <github@flameflag.dev>
//
// SPDX-License-Identifier: AGPLv3+

package theme

import (
	"embed"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/asciimoo/hister/config"
	"gopkg.in/yaml.v3"
)

//go:embed themes/*.yaml
var embeddedThemes embed.FS

// Palette holds 16 base16 color hex strings that define a complete color scheme
type Palette struct {
	Name   string `yaml:"name"`
	Base00 string `yaml:"base00"` // Background
	Base01 string `yaml:"base01"` // Alt Background
	Base02 string `yaml:"base02"` // Selection BG
	Base03 string `yaml:"base03"` // Comments/muted
	Base04 string `yaml:"base04"` // Dark FG
	Base05 string `yaml:"base05"` // Default FG
	Base06 string `yaml:"base06"` // Light FG
	Base07 string `yaml:"base07"` // Light BG (rarely used)
	Base08 string `yaml:"base08"` // Red
	Base09 string `yaml:"base09"` // Orange/Peach
	Base0A string `yaml:"base0a"` // Yellow
	Base0B string `yaml:"base0b"` // Green
	Base0C string `yaml:"base0c"` // Teal/Cyan
	Base0D string `yaml:"base0d"` // Blue
	Base0E string `yaml:"base0e"` // Purple/Mauve
	Base0F string `yaml:"base0f"` // Pink/Flamingo
}

type themeFile struct {
	Name   string `yaml:"name"`
	Scheme string `yaml:"scheme"`
	Base00 string `yaml:"base00"`
	Base01 string `yaml:"base01"`
	Base02 string `yaml:"base02"`
	Base03 string `yaml:"base03"`
	Base04 string `yaml:"base04"`
	Base05 string `yaml:"base05"`
	Base06 string `yaml:"base06"`
	Base07 string `yaml:"base07"`
	Base08 string `yaml:"base08"`
	Base09 string `yaml:"base09"`
	Base0A string `yaml:"base0a"`
	Base0B string `yaml:"base0b"`
	Base0C string `yaml:"base0c"`
	Base0D string `yaml:"base0d"`
	Base0E string `yaml:"base0e"`
	Base0F string `yaml:"base0f"`
}

func (tf themeFile) toPalette() Palette {
	name := tf.Name
	if name == "" {
		name = tf.Scheme
	}
	p := Palette{
		Name:   name,
		Base00: tf.Base00, Base01: tf.Base01, Base02: tf.Base02, Base03: tf.Base03,
		Base04: tf.Base04, Base05: tf.Base05, Base06: tf.Base06, Base07: tf.Base07,
		Base08: tf.Base08, Base09: tf.Base09, Base0A: tf.Base0A, Base0B: tf.Base0B,
		Base0C: tf.Base0C, Base0D: tf.Base0D, Base0E: tf.Base0E, Base0F: tf.Base0F,
	}
	normalizePalette(&p)
	return p
}

var (
	themeRegistry = map[string]Palette{}
	themeOrder    []string
)

func init() {
	entries, err := embeddedThemes.ReadDir("themes")
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := embeddedThemes.ReadFile("themes/" + e.Name())
		if err != nil {
			continue
		}
		var tf themeFile
		if err := yaml.Unmarshal(data, &tf); err != nil {
			continue
		}
		p := tf.toPalette()
		if p.Name != "" {
			registerTheme(p)
		}
	}
}

func normalizeHex(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	if !strings.HasPrefix(s, "#") && len(s) == 6 {
		return "#" + s
	}
	return s
}

func normalizePalette(p *Palette) {
	p.Base00 = normalizeHex(p.Base00)
	p.Base01 = normalizeHex(p.Base01)
	p.Base02 = normalizeHex(p.Base02)
	p.Base03 = normalizeHex(p.Base03)
	p.Base04 = normalizeHex(p.Base04)
	p.Base05 = normalizeHex(p.Base05)
	p.Base06 = normalizeHex(p.Base06)
	p.Base07 = normalizeHex(p.Base07)
	p.Base08 = normalizeHex(p.Base08)
	p.Base09 = normalizeHex(p.Base09)
	p.Base0A = normalizeHex(p.Base0A)
	p.Base0B = normalizeHex(p.Base0B)
	p.Base0C = normalizeHex(p.Base0C)
	p.Base0D = normalizeHex(p.Base0D)
	p.Base0E = normalizeHex(p.Base0E)
	p.Base0F = normalizeHex(p.Base0F)
}

func registerTheme(p Palette) {
	if _, exists := themeRegistry[p.Name]; !exists {
		themeOrder = append(themeOrder, p.Name)
	}
	themeRegistry[p.Name] = p
}

func LoadUserThemes(dir string) {
	if dir == "" {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var tf themeFile
		if err := yaml.Unmarshal(data, &tf); err != nil {
			continue
		}
		p := tf.toPalette()
		if p.Name != "" {
			registerTheme(p)
		}
	}
}

// returns true if the Base00 background color has luminance > 0.5
func IsLightPalette(p Palette) bool {
	hex := strings.TrimPrefix(p.Base00, "#")
	if len(hex) != 6 {
		return false
	}
	r, err1 := strconv.ParseUint(hex[0:2], 16, 8)
	g, err2 := strconv.ParseUint(hex[2:4], 16, 8)
	b, err3 := strconv.ParseUint(hex[4:6], 16, 8)
	if err1 != nil || err2 != nil || err3 != nil {
		return false
	}
	lum := 0.299*float64(r)/255 + 0.587*float64(g)/255 + 0.114*float64(b)/255
	return lum > 0.5
}

func ThemeNames() []string {
	return themeOrder
}

func GetPalette(name string) (Palette, bool) {
	p, ok := themeRegistry[name]
	return p, ok
}

func ClassifyThemes() (darkNames, lightNames []string) {
	for _, name := range themeOrder {
		if p, ok := themeRegistry[name]; ok {
			if IsLightPalette(p) {
				lightNames = append(lightNames, name)
			} else {
				darkNames = append(darkNames, name)
			}
		}
	}
	return
}

func ResolvePalette(tui *config.TUI, isDark bool) (Palette, string) {
	if os.Getenv("NO_COLOR") != "" {
		return Palette{Name: "no-color"}, "no-color"
	}

	var chosen string
	switch tui.ColorScheme {
	case "dark":
		chosen = tui.DarkTheme
	case "light":
		chosen = tui.LightTheme
	default:
		if isDark {
			chosen = tui.DarkTheme
		} else {
			chosen = tui.LightTheme
		}
	}
	if chosen == "" {
		chosen = "catppuccin-mocha"
	}
	if p, ok := themeRegistry[chosen]; ok {
		return p, p.Name
	}
	if len(themeOrder) > 0 {
		p := themeRegistry[themeOrder[0]]
		return p, p.Name
	}
	return Palette{Name: "catppuccin-mocha"}, "catppuccin-mocha"
}
