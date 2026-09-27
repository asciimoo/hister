// SPDX-FileContributor: 4evy <git@evy.pink>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package model

import (
	"slices"
	"time"

	"github.com/gorilla/websocket"

	"github.com/asciimoo/hister/client"
	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/indexer"

	tea "charm.land/bubbletea/v2"
)

// represents the current UI state
type ViewState int

const (
	StateInput ViewState = iota
	StateResults
	StateDialog
	StateHelp
	StateThemePicker
	StateContextMenu
	StateSettings
	StatePrioritizeInput
	StateDetails
	StateLabelInput
)

func (s ViewState) String() string {
	return []string{"INPUT", "RESULTS", "DIALOG", "HELP", "THEME_PICKER", "CONTEXT_MENU", "SETTINGS", "PRIORITIZE_INPUT", "DETAILS", "LABEL_INPUT"}[s]
}

// NoticeKind lets transient and inline messages communicate meaning with
// words/symbols as well as color. This matters in terminal and no-color modes,
// where hue alone is not a reliable status indicator.
type NoticeKind uint8

const (
	NoticeInfo NoticeKind = iota
	NoticeSuccess
	NoticeWarning
	NoticeError
)

// sent over WebSocket to the search server
type SearchQuery struct {
	Text              string  `json:"text"`
	Highlight         string  `json:"highlight"`
	Limit             int     `json:"limit"`
	Sort              string  `json:"sort,omitempty"`
	SemanticEnabled   bool    `json:"semantic_enabled,omitempty"`
	SemanticThreshold float64 `json:"semantic_threshold,omitempty"`
	SemanticWeight    float64 `json:"semantic_weight,omitempty"`
}

// Message types for bubbletea
type (
	ResultsMsg             struct{ Results *indexer.Results }
	ErrMsg                 struct{ Err error }
	WsConnectedMsg         struct{ Conn *websocket.Conn }
	WsDisconnectedMsg      struct{ Err error }
	ReconnectMsg           struct{}
	ServerConfigFetchedMsg struct {
		Config *client.ServerConfig
		Err    error
	}
	PreviewDebounceMsg struct {
		URL string
		ID  uint64
	}
	HintClearMsg        struct{}
	SettingsErrClearMsg struct{}
	NoticeClearMsg      struct{ ID uint64 }
	HistoryFetchedMsg   struct {
		Items []HistoryItem
		Err   error
	}
	RulesFetchedMsg struct {
		Data RulesResponse
		Err  error
	}
	AddResultMsg    struct{ Err error }
	RulesSavedMsg   struct{ Err error }
	DeleteResultMsg struct{ Err error }
	LabelSavedMsg   struct {
		URL   string
		Label string
		Err   error
	}
	PreviewFetchedMsg struct {
		URL     string
		Preview *client.PreviewResponse
		Err     error
	}
)

type HistoryItem = client.HistoryItem

type RulesResponse = client.RulesResponse

type HintRegion struct {
	X0, X1 int
	Action config.Action
}

type WorkspaceTargetKind uint8

const (
	WorkspaceHistoryItem WorkspaceTargetKind = iota
	WorkspaceRulesForm
	WorkspaceRulesItem
	WorkspaceAddField
	WorkspaceAddSubmit
)

// WorkspaceTarget describes one interactive block in a scrollable tab. Mouse
// hit testing consumes the same geometry the renderer produced, so layout
// changes do not require a second set of magic coordinates.
type WorkspaceTarget struct {
	Y, Height int
	Kind      WorkspaceTargetKind
	Section   int
	Index     int
}

// holds one key → action row for the settings panel
type SettingsItem struct {
	Key    string
	Action config.Action
}

const (
	TabSearch  = 0
	TabHistory = 1
	TabRules   = 2
	TabAdd     = 3
)

// TabDefinition is the single source of truth for tab identity, presentation,
// and navigation. Renderers and handlers consume the same ordered slice so a
// tab cannot acquire mismatched labels, hit targets, or shortcuts.
type TabDefinition struct {
	ID     int
	Name   string
	Action config.Action
}

var Tabs = []TabDefinition{
	{ID: TabSearch, Name: "Search", Action: config.ActionTabSearch},
	{ID: TabHistory, Name: "History", Action: config.ActionTabHistory},
	{ID: TabRules, Name: "Rules", Action: config.ActionTabRules},
	{ID: TabAdd, Name: "Add", Action: config.ActionTabAdd},
}

func TabForAction(action config.Action) (int, bool) {
	if i := slices.IndexFunc(Tabs, func(tab TabDefinition) bool { return tab.Action == action }); i >= 0 {
		return Tabs[i].ID, true
	}
	return 0, false
}

const (
	RulesSectionSkip = iota
	RulesSectionPriority
	RulesSectionVersioning
	RulesSectionAllow
	RulesSectionAliases
)

type RulesSectionDefinition struct {
	ID          int
	Title       string
	Placeholder string
	Aliases     bool
}

var RulesSections = []RulesSectionDefinition{
	{ID: RulesSectionSkip, Title: "Skip Patterns", Placeholder: "skip pattern..."},
	{ID: RulesSectionPriority, Title: "Priority Patterns", Placeholder: "priority pattern..."},
	{ID: RulesSectionVersioning, Title: "Versioning Patterns", Placeholder: "versioning pattern..."},
	{ID: RulesSectionAllow, Title: "Allow Patterns", Placeholder: "allow pattern..."},
	{ID: RulesSectionAliases, Title: "Aliases", Aliases: true},
}

const (
	RulesFocusPattern = iota
	RulesFocusAliasKey
	RulesFocusAliasValue
	RulesFocusList
)

// Layout constants shared across packages (mouse handlers, render, model init).
const (
	ResultsPageSize   = 10 // results per page
	ScrollbarWidth    = 2  // columns reserved for scrollbar
	TabBarLeftPad     = 1  // leading space before first tab
	TabLabelPad       = 2  // brackets/spaces around tab name
	TabGap            = 1  // space between tab labels
	AddSubmitFieldIdx = 3  // focus index for submit button
	InputLeadingPad   = 2  // spaces before prompt ("  ")
	InputTrailingPad  = 1  // space after prompt (" ")
)

const (
	RowTabBar         = 0 // tab bar header
	RowInput          = 2 // search input line
	RowVPStart        = 4 // first viewport row
	RowWorkspaceStart = 2
)

// returns the hints row Y position for the given terminal height.
func RowHints(height int) int { return height - 1 }

// returns the last viewport row Y position for the given terminal height.
func RowVPEnd(height int) int { return height - 3 }

// Fixed layout overhead (header + dividers + input + hints)
const FixedLayoutRows = 6

const (
	// DetailsSplitMinWidth is the smallest terminal that can show useful
	// result and preview columns side by side. Narrower terminals use the same
	// preview pane full-width.
	DetailsSplitMinWidth = 88
	DetailsPaneMinWidth  = 38
	DetailsPaneMaxWidth  = 68
)

const (
	MenuOpen int = iota
	MenuCopy
	MenuDetails
	MenuLabel
	MenuPrioritize
	MenuDelete
)

type MenuOptionDefinition struct {
	ID    int
	Label string
}

var MenuOptions = []MenuOptionDefinition{
	{ID: MenuOpen, Label: "Open"},
	{ID: MenuCopy, Label: "Copy URL"},
	{ID: MenuDetails, Label: "Details"},
	{ID: MenuLabel, Label: "Edit label"},
	{ID: MenuPrioritize, Label: "Prioritize"},
	{ID: MenuDelete, Label: "Delete"},
}

func MenuOptionAt(index int) (MenuOptionDefinition, bool) {
	if index < 0 || index >= len(MenuOptions) {
		return MenuOptionDefinition{}, false
	}
	return MenuOptions[index], true
}

// Dialog/overlay layout: border(1) + padding(1) + content rows
const (
	OverlayBorderRows  = 1 // top border row
	OverlayPaddingRows = 1 // padding inside border
)

// DialogBtnRowY returns the relative Y of the button row inside a dialog overlay.
// Layout: border(1) + padding(1) + title(1) + blank(1) + label(1) + blank(1) + buttons(1)
func DialogBtnRowY() int { return 7 }

// PrioritizeBtnRowY returns the relative Y of the button row inside prioritize dialog.
// Layout: border(1) + padding(1) + title(1) + blank(1) + label(1) + input(1) + blank(1) + buttons(1)
func PrioritizeBtnRowY() int { return 7 }

const AddTextHeight = 5

func (m *Model) FlashHint(action config.Action) tea.Cmd {
	m.HintFlash = action
	return ClearHintAfter()
}

func ClearHintAfter() tea.Cmd {
	return tea.Tick(350*time.Millisecond, func(_ time.Time) tea.Msg {
		return HintClearMsg{}
	})
}

func (m *Model) Notify(message string) tea.Cmd {
	return m.notify(message, NoticeInfo)
}

func (m *Model) NotifySuccess(message string) tea.Cmd {
	return m.notify(message, NoticeSuccess)
}

func (m *Model) NotifyWarning(message string) tea.Cmd {
	return m.notify(message, NoticeWarning)
}

func (m *Model) NotifyError(message string) tea.Cmd {
	return m.notify(message, NoticeError)
}

func (m *Model) notify(message string, kind NoticeKind) tea.Cmd {
	m.Notice = message
	m.NoticeKind = kind
	m.NoticeID++
	id := m.NoticeID
	duration := 2500 * time.Millisecond
	switch kind {
	case NoticeWarning:
		duration = 4 * time.Second
	case NoticeError:
		duration = 6 * time.Second
	}
	return tea.Tick(duration, func(_ time.Time) tea.Msg {
		return NoticeClearMsg{ID: id}
	})
}
