// SPDX-FileContributor: FlameFlag <github@flameflag.dev>
//
// SPDX-License-Identifier: AGPLv3+

package theme

import "github.com/charmbracelet/lipgloss"

type Styles struct {
	// Layout chrome
	Brand        lipgloss.Style
	Div          lipgloss.Style
	PromptActive lipgloss.Style
	PromptBlur   lipgloss.Style

	// Result items
	Title    lipgloss.Style
	SelTitle lipgloss.Style
	URL      lipgloss.Style
	URLPath  lipgloss.Style
	URLLocal lipgloss.Style
	Hist     lipgloss.Style
	Gray     lipgloss.Style
	SecText  lipgloss.Style
	Time     lipgloss.Style
	Count    lipgloss.Style

	// Section divider
	Section lipgloss.Style

	// Query suggestion
	SuggLabel lipgloss.Style
	SuggTerm  lipgloss.Style

	// Overlays
	Dialog lipgloss.Style
	Help   lipgloss.Style

	// Overlay border colors
	DialogBorder lipgloss.Color
	HelpBorder   lipgloss.Color
	ThemeBorder  lipgloss.Color

	// Status / hints
	Conn         lipgloss.Style
	Disc         lipgloss.Style
	Status       lipgloss.Style
	Spin         lipgloss.Style
	HintKey      lipgloss.Style
	Hint         lipgloss.Style
	HintKeyFlash lipgloss.Style
	HintFlash    lipgloss.Style

	// Scrollbar
	Thumb lipgloss.Style
	Track lipgloss.Style

	// List items
	Item             lipgloss.Style
	SelectedItem     lipgloss.Style
	LoadMore         lipgloss.Style
	LoadMoreSelected lipgloss.Style

	// Theme picker overlay
	ThemePicker         lipgloss.Style
	ThemePickerItem     lipgloss.Style
	ThemePickerSelected lipgloss.Style

	// Help overlay
	HelpHeader lipgloss.Style
	HelpAction lipgloss.Style

	// Tab bar
	TabActive   lipgloss.Style
	TabInactive lipgloss.Style

	// Dialog buttons
	DeleteBtn     lipgloss.Style
	DeleteBtnSel  lipgloss.Style
	CancelBtn     lipgloss.Style
	CancelBtnSel  lipgloss.Style
	ConfirmBtn    lipgloss.Style
	ConfirmBtnSel lipgloss.Style

	// Text input placeholder
	Placeholder lipgloss.Style

	// Domain display
	DomainLabel  lipgloss.Style
	DomainHeader lipgloss.Style
}

func BuildStyles(p Palette) Styles {
	c := func(hex string) lipgloss.Color { return lipgloss.Color(hex) }

	return Styles{
		Brand:        lipgloss.NewStyle().Foreground(c(p.Base0D)).Bold(true),
		Div:          lipgloss.NewStyle().Foreground(c(p.Base03)),
		PromptActive: lipgloss.NewStyle().Foreground(c(p.Base09)).Bold(true),
		PromptBlur:   lipgloss.NewStyle().Foreground(c(p.Base03)),

		Title:    lipgloss.NewStyle().Bold(true).Foreground(c(p.Base05)),
		SelTitle: lipgloss.NewStyle().Bold(true).Foreground(c(p.Base0D)),
		URL:      lipgloss.NewStyle().Foreground(c(p.Base0D)),
		URLPath:  lipgloss.NewStyle().Foreground(c(p.Base03)),
		URLLocal: lipgloss.NewStyle().Foreground(c(p.Base0C)),
		Hist:     lipgloss.NewStyle().Foreground(c(p.Base09)),
		Gray:     lipgloss.NewStyle().Foreground(c(p.Base04)),
		SecText:  lipgloss.NewStyle().Foreground(c(p.Base04)).Faint(true).Italic(true),
		Time:     lipgloss.NewStyle().Foreground(c(p.Base03)),
		Count:    lipgloss.NewStyle().Foreground(c(p.Base09)).Bold(true),

		Section:   lipgloss.NewStyle().Foreground(c(p.Base03)),
		SuggLabel: lipgloss.NewStyle().Foreground(c(p.Base03)).Italic(true),
		SuggTerm:  lipgloss.NewStyle().Foreground(c(p.Base09)).Italic(true).Bold(true),

		Dialog: lipgloss.NewStyle().Padding(1, 2),
		Help:   lipgloss.NewStyle().Padding(1, 2),

		DialogBorder: c(p.Base08),
		HelpBorder:   c(p.Base0D),
		ThemeBorder:  c(p.Base0E),

		Conn:         lipgloss.NewStyle().Foreground(c(p.Base0B)).Bold(true),
		Disc:         lipgloss.NewStyle().Foreground(c(p.Base08)).Bold(true),
		Status:       lipgloss.NewStyle().Foreground(c(p.Base04)),
		Spin:         lipgloss.NewStyle().Foreground(c(p.Base09)),
		HintKey:      lipgloss.NewStyle().Foreground(c(p.Base04)).Bold(true),
		Hint:         lipgloss.NewStyle().Foreground(c(p.Base03)),
		HintKeyFlash: lipgloss.NewStyle().Foreground(c(p.Base0D)).Bold(true).Reverse(true),
		HintFlash:    lipgloss.NewStyle().Foreground(c(p.Base06)),

		Thumb: lipgloss.NewStyle().Foreground(c(p.Base0E)).Bold(true),
		Track: lipgloss.NewStyle().Foreground(c(p.Base03)),

		Item: lipgloss.NewStyle().PaddingLeft(2),
		SelectedItem: lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderLeft(true).
			BorderForeground(c(p.Base0D)).
			PaddingLeft(1),
		LoadMore: lipgloss.NewStyle().Foreground(c(p.Base09)).Bold(true),
		LoadMoreSelected: lipgloss.NewStyle().
			Foreground(c(p.Base09)).
			Bold(true).
			Background(c(p.Base02)),

		ThemePicker:         lipgloss.NewStyle().Padding(1, 2),
		ThemePickerItem:     lipgloss.NewStyle().Foreground(c(p.Base04)),
		ThemePickerSelected: lipgloss.NewStyle().Foreground(c(p.Base0D)).Bold(true),

		HelpHeader: lipgloss.NewStyle().Foreground(c(p.Base0D)).Bold(true),
		HelpAction: lipgloss.NewStyle().Foreground(c(p.Base04)),

		TabActive:   lipgloss.NewStyle().Foreground(c(p.Base0D)).Bold(true),
		TabInactive: lipgloss.NewStyle().Foreground(c(p.Base04)),

		DeleteBtn:     lipgloss.NewStyle().Foreground(c(p.Base08)).Bold(true),
		DeleteBtnSel:  lipgloss.NewStyle().Foreground(c(p.Base08)).Bold(true).Reverse(true),
		CancelBtn:     lipgloss.NewStyle().Foreground(c(p.Base05)),
		CancelBtnSel:  lipgloss.NewStyle().Foreground(c(p.Base05)).Reverse(true),
		ConfirmBtn:    lipgloss.NewStyle().Foreground(c(p.Base0B)).Bold(true),
		ConfirmBtnSel: lipgloss.NewStyle().Foreground(c(p.Base0B)).Bold(true).Reverse(true),

		Placeholder: lipgloss.NewStyle().Foreground(c(p.Base03)),

		DomainLabel:  lipgloss.NewStyle().Foreground(c(p.Base0C)).Faint(true),
		DomainHeader: lipgloss.NewStyle().Foreground(c(p.Base0D)).Bold(true).Background(c(p.Base01)),
	}
}
