// SPDX-FileContributor: 4evy <git@evy.pink>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package handle

import (
	"time"

	"github.com/asciimoo/hister/cmd/tui/handle/mouse"
	"github.com/asciimoo/hister/cmd/tui/model"
	"github.com/asciimoo/hister/cmd/tui/network"
	"github.com/asciimoo/hister/cmd/tui/render"
	"github.com/asciimoo/hister/cmd/tui/theme"
	"github.com/asciimoo/hister/config"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
)

var mouseHandler = mouse.New(mouse.Deps{
	ExecuteAction:              ExecuteAction,
	SwitchTab:                  SwitchTab,
	StartSearch:                startSearch,
	CloseOverlay:               CloseOverlay,
	SubmitAdd:                  submitAdd,
	CloseThemePickerWithRevert: CloseThemePickerWithRevert,
	PreviewTheme:               previewTheme,
	CycleAppearance:            cycleAppearanceMode,
	ExecuteContextMenuAction:   executeContextMenuAction,
	ReloadDetails:              ReloadDetails,
})

func Update(m *model.Model, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		changed := m.Width != msg.Width || m.Height != msg.Height
		m.Width, m.Height = msg.Width, msg.Height

		vpH := max(0, m.Height-model.FixedLayoutRows)
		m.TextInput.SetWidth(max(1, m.Width-6))
		formW := max(12, min(72, m.Width-12))
		for i := range m.AddInputs {
			m.AddInputs[i].SetWidth(formW)
		}
		m.AddText.SetWidth(formW)
		for i := range m.RulesPatternInputs {
			m.RulesPatternInputs[i].SetWidth(max(12, min(48, m.Width-24)))
		}
		m.LabelInput.SetWidth(max(12, min(64, m.Width-16)))
		m.Workspace.SetWidth(max(1, m.Width-1))
		m.Workspace.SetHeight(max(1, vpH+2))
		m.Help.SetWidth(max(1, m.Width-4))

		if !m.Ready {
			m.Viewport = viewport.New(viewport.WithWidth(1), viewport.WithHeight(vpH))
			m.Viewport.FillHeight = true
			m.Viewport.SetContent("")
			m.Ready = true
			render.ResizeSearchViewports(m)
			if render.DetailsVisible(m) {
				m.Details.SetContent(render.ResultDetailsContent(m))
			}
			return tea.ClearScreen
		}
		render.ResizeSearchViewports(m)
		if render.DetailsVisible(m) {
			m.Details.SetContent(render.ResultDetailsContent(m))
		}
		render.RefreshAndScroll(m)
		if changed {
			return tea.ClearScreen
		}
		return nil

	case tea.KeyPressMsg:
		if m.Keys.Action(msg) == config.ActionQuit {
			return tea.Quit
		}
		switch m.State {
		case model.StateDialog:
			return DialogKeys(m, msg)
		case model.StateInput:
			if m.ActiveTab != model.TabSearch {
				return TabKeys(m, msg)
			}
			return InputKeys(m, msg)
		case model.StateResults:
			if m.ActiveTab != model.TabSearch {
				return TabKeys(m, msg)
			}
			return ResultsKeys(m, msg)
		case model.StateHelp:
			m.DismissOverlay()
			if m.State == model.StateInput {
				return m.TextInput.Focus()
			}
			return nil
		case model.StateThemePicker:
			return ThemePickerKeys(m, msg)
		case model.StateContextMenu:
			return ContextMenuKeys(m, msg)
		case model.StateSettings:
			return SettingsKeys(m, msg)
		case model.StatePrioritizeInput:
			return PrioritizeInputKeys(m, msg)
		case model.StateDetails:
			return DetailsKeys(m, msg)
		case model.StateLabelInput:
			return LabelInputKeys(m, msg)
		}

	case tea.PasteMsg:
		return Paste(m, msg)

	case tea.MouseMsg:
		return mouseHandler.Handle(m, msg)

	case tea.BackgroundColorMsg:
		m.IsDarkBg = msg.IsDark()
		palette, _ := theme.ResolvePalette(&m.Cfg.TUI, m.IsDarkBg)
		m.ApplyTheme(palette)
		render.RefreshViewport(m)
		if m.DetailsURL != "" {
			m.Details.SetContent(render.ResultDetailsContent(m))
		}
		return tea.ClearScreen

	case spinner.TickMsg:
		if m.IsSearching || m.HistoryLoading || m.RulesLoading || m.DetailsLoading {
			var cmd tea.Cmd
			m.Spinner, cmd = m.Spinner.Update(msg)
			return cmd
		}

	case model.HintClearMsg:
		m.HintFlash = ""
	case model.SettingsErrClearMsg:
		m.SettingsEditErr = ""
	case model.NoticeClearMsg:
		if msg.ID == m.NoticeID {
			m.Notice = ""
		}
	case model.HistoryFetchedMsg:
		m.HistoryLoading = false
		if msg.Err != nil {
			return m.NotifyError("Could not load history: " + msg.Err.Error())
		}
		m.HistoryItems = msg.Items
		m.HistoryIdx = 0

	case model.RulesFetchedMsg:
		m.RulesLoading = false
		if msg.Err != nil {
			return m.NotifyError("Could not load rules: " + msg.Err.Error())
		}
		m.RulesData = msg.Data
		m.RulesIdx = 0

	case model.DeleteResultMsg:
		if msg.Err != nil {
			return m.NotifyError("Could not delete result: " + msg.Err.Error())
		}
		m.ResetDetails()
		m.SetBaseState(model.StateResults)
		render.ResizeSearchViewports(m)
		render.RefreshAndScroll(m)
		return tea.Batch(doSearch(m), m.NotifySuccess("Result deleted"))

	case model.AddResultMsg:
		if msg.Err != nil {
			m.AddStatus = "Could not add document: " + msg.Err.Error()
			m.AddStatusKind = model.NoticeError
		} else {
			m.AddStatus = "Document added"
			m.AddStatusKind = model.NoticeSuccess
			for i := range m.AddInputs {
				m.AddInputs[i].SetValue("")
				m.AddInputs[i].Blur()
			}
			m.AddText.SetValue("")
			m.AddText.Blur()
			m.AddFocusIdx = 0
			return m.AddInputs[0].Focus()
		}

	case model.RulesSavedMsg:
		if msg.Err == nil {
			m.RulesLoading = true
			return tea.Batch(m.FetchRulesCmd(), m.NotifySuccess("Rules saved"))
		}
		return m.NotifyError("Could not save rules: " + msg.Err.Error())

	case model.LabelSavedMsg:
		if msg.Err != nil {
			return m.NotifyError("Could not update label: " + msg.Err.Error())
		}
		if m.Results != nil {
			for _, doc := range m.Results.Documents {
				if doc.URL == msg.URL {
					doc.Label = msg.Label
				}
			}
			for _, hit := range m.Results.SemanticHits {
				if hit.Document != nil && hit.Document.URL == msg.URL {
					hit.Document.Label = msg.Label
				}
			}
		}
		render.RefreshViewport(m)
		if m.DetailsURL != "" {
			m.Details.SetContent(render.ResultDetailsContent(m))
		}
		if msg.Label == "" {
			return m.NotifySuccess("Label cleared")
		}
		return m.NotifySuccess("Label saved")

	case model.ResultsMsg:
		m.IsSearching = false
		m.Results = msg.Results
		if m.SelectedIdx >= m.GetTotalResults() {
			m.SelectedIdx = m.GetTotalResults() - 1
		}
		focusInput := false
		if m.GetTotalResults() == 0 {
			focusInput = m.ReplaceBaseState(model.StateResults, model.StateInput)
		}
		if m.SelectedIdx < 0 && m.GetTotalResults() > 0 && m.State != model.StateInput {
			m.SelectedIdx = 0
		}
		render.RefreshAndScroll(m)
		listen := network.ListenToWebSocket(m.WsChan, m.WsDone)
		if focusInput {
			return tea.Batch(m.TextInput.Focus(), listen)
		}
		return listen

	case model.ServerConfigFetchedMsg:
		if msg.Err != nil {
			return m.Notify("Could not load server capabilities: " + msg.Err.Error())
		}
		if msg.Config != nil {
			m.SemanticEnabled = msg.Config.SemanticEnabled
			m.SemanticThreshold = msg.Config.SimilarityThreshold
			m.SemanticWeight = msg.Config.SemanticWeight
			if !m.SemanticEnabled {
				m.SemanticOn = false
			}
		}

	case model.PreviewDebounceMsg:
		if msg.ID != m.DetailsRequestID || msg.URL != m.DetailsURL {
			return nil
		}
		m.DetailsPendingReady = true
		return m.StartPendingPreviewCmd()

	case model.PreviewFetchedMsg:
		m.DetailsFetching = false
		if msg.URL == m.DetailsURL {
			m.DetailsLoading = false
			m.DetailsErr = msg.Err
			m.DetailsPreview = msg.Preview
			m.Details.SetContent(render.ResultDetailsContent(m))
		}
		return m.StartPendingPreviewCmd()

	case model.WsConnectedMsg:
		if msg.Conn != nil {
			m.Conn = msg.Conn
			m.WsReady = true
			m.ConnError = nil
			render.RefreshViewport(m)
		}
		return network.ListenToWebSocket(m.WsChan, m.WsDone)

	case model.WsDisconnectedMsg:
		m.WsReady = false
		m.IsSearching = false
		if msg.Err != nil {
			m.ConnError = msg.Err
		}
		return tea.Tick(2*time.Second, func(_ time.Time) tea.Msg { return model.ReconnectMsg{} })

	case model.ReconnectMsg:
		return network.ConnectWebSocket(m.Cfg.WebSocketURL(), m.Cfg.BaseURL(""), m.Cfg.App.AccessToken, m.WsChan, m.WsDone)

	case model.ErrMsg:
		return tea.Batch(m.NotifyError(msg.Err.Error()), network.ListenToWebSocket(m.WsChan, m.WsDone))
	}
	return nil
}
