// SPDX-FileContributor: 4evy <git@evy.pink>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package tui

import (
	"github.com/asciimoo/nogon/cmd/tui/handle"
	"github.com/asciimoo/nogon/cmd/tui/model"
	"github.com/asciimoo/nogon/cmd/tui/network"
	"github.com/asciimoo/nogon/cmd/tui/render"
	"github.com/asciimoo/nogon/cmd/tui/theme"
	"github.com/asciimoo/nogon/config"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

type app struct{ m *model.Model }

func (a *app) Init() tea.Cmd {
	cmds := []tea.Cmd{
		textinput.Blink,
		a.m.FetchServerConfigCmd(),
		tea.RequestBackgroundColor,
		network.ConnectWebSocket(a.m.Cfg.WebSocketURL(), a.m.Cfg.BaseURL(""), a.m.Cfg.App.AccessToken, a.m.WsChan, a.m.WsDone),
		network.ListenToWebSocket(a.m.WsChan, a.m.WsDone),
	}
	return tea.Batch(cmds...)
}

func (a *app) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return a, handle.Update(a.m, msg)
}

func (a *app) View() tea.View {
	v := tea.NewView(render.View(a.m))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	// A nil color tells Bubble Tea to retain the terminal's configured color.
	// Full themes opt in to painting the screen; terminal/no-color modes do not.
	if a.m.Cfg.TUI.ColorScheme != theme.TerminalName && a.m.ThemeName != "no-color" {
		v.BackgroundColor = a.m.BackgroundColor
		v.ForegroundColor = a.m.ForegroundColor
	}
	return v
}

func SearchTUI(cfg *config.Config) error {
	m := model.InitialModel(cfg)
	a := &app{m: m}
	p := tea.NewProgram(a)
	_, err := p.Run()
	m.Close()
	return err
}
