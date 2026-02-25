// SPDX-FileContributor: FlameFlag <github@flameflag.dev>
//
// SPDX-License-Identifier: AGPLv3+

package ui

import (
	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/ui/handle"
	"github.com/asciimoo/hister/ui/model"
	"github.com/asciimoo/hister/ui/network"
	"github.com/asciimoo/hister/ui/render"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type app struct{ m *model.Model }

func (a *app) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		network.ConnectWebSocket(a.m.Cfg.WebSocketURL(), a.m.Cfg.BaseURL(""), a.m.WsChan, a.m.WsDone),
		network.ListenToWebSocket(a.m.WsChan, a.m.WsDone),
	)
}

func (a *app) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return a, handle.Update(a.m, msg)
}

func (a *app) View() string {
	return render.View(a.m)
}

func SearchTUI(cfg *config.Config) error {
	m := model.InitialModel(cfg)
	a := &app{m: m}
	p := tea.NewProgram(a, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	m.Close()
	return err
}
