// SPDX-FileContributor: FlameFlag <github@flameflag.dev>
//
// SPDX-License-Identifier: AGPLv3+

package model

import (
	"time"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/indexer"
	"github.com/gorilla/websocket"

	tea "github.com/charmbracelet/bubbletea"
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
)

func (s ViewState) String() string {
	return []string{"INPUT", "RESULTS", "DIALOG", "HELP", "THEME_PICKER", "CONTEXT_MENU", "SETTINGS", "PRIORITIZE_INPUT"}[s]
}

// sent over WebSocket to the search server
type SearchQuery struct {
	Text      string `json:"text"`
	Highlight string `json:"highlight"`
	Limit     int    `json:"limit"`
	Sort      string `json:"sort,omitempty"`
}

// Message types for bubbletea
type ResultsMsg struct{ Results *indexer.Results }
type ErrMsg struct{ Err error }
type WsConnectedMsg struct{ Conn *websocket.Conn }
type WsDisconnectedMsg struct{ Err error }
type ReconnectMsg struct{}
type HintClearMsg struct{}
type SettingsErrClearMsg struct{}
type HistoryFetchedMsg struct{ Items []HistoryEntry }
type RulesFetchedMsg struct{ Data RulesResponse }
type AddResultMsg struct{ Err error }
type RulesSavedMsg struct{ Err error }

// is a single item from the history API
type HistoryEntry struct {
	Query string `json:"query"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

// holds skip/priority patterns and aliases from the rules API
type RulesResponse struct {
	Skip     []string          `json:"skip"`
	Priority []string          `json:"priority"`
	Aliases  map[string]string `json:"aliases"`
}

// represents a clickable region in the hints bar
type HintRegion struct {
	X0, X1 int
	Action config.Action
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

const (
	RulesFieldSkip     = 0
	RulesFieldPriority = 1
	RulesFieldAliasKey = 2
	RulesFieldAliasVal = 3
	RulesFieldList     = 4
)

var ActionToTab = map[config.Action]int{
	config.ActionTabSearch:  TabSearch,
	config.ActionTabHistory: TabHistory,
	config.ActionTabRules:   TabRules,
	config.ActionTabAdd:     TabAdd,
}

var TabNames = []string{"Search", "History", "Rules", "Add"}

const (
	RowTabBar  = 0 // tab bar header
	RowInput   = 2 // search input line
	RowVPStart = 4 // first viewport row
)

// returns the hints row Y position for the given terminal height.
func RowHints(height int) int { return height - 1 }

// returns the last viewport row Y position for the given terminal height.
func RowVPEnd(height int) int { return height - 3 }

// Fixed layout overhead (header + dividers + input + hints)
const FixedLayoutRows = 6

const (
	MenuOpen        = 0
	MenuDelete      = 1
	MenuPrioritize  = 2
	MenuOptionCount = 3
)

var MenuOptionLabels = []string{"Open", "Delete", "Prioritize"}

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

// Add tab row positions (relative to content area, after tab bar + dividers)
const (
	AddURLLabelY   = 5
	AddURLInputY   = 6
	AddTitleLabelY = 8
	AddTitleInputY = 9
	AddTextLabelY  = 11
	AddTextInputY  = 12
	AddSubmitY     = 14
)

var SearchTips = []string{
	"Type to search...",
	"Tip: Use quotes for exact phrases",
	"Tip: Press Tab to focus results",
	"Tip: Press Ctrl+D to delete a result",
	"Tip: Sort by domain with Ctrl+O",
	"Tip: Press Ctrl+T to change theme",
	"Tip: Press Ctrl+S to edit keybindings",
	"Tip: Press F1 for help",
	"Tip: Right-click for context menu",
	"Tip: Press Alt+2/3/4 for History/Rules/Add",
}

func (m *Model) FlashHint(action config.Action) tea.Cmd {
	m.HintFlash = action
	return ClearHintAfter()
}

func ClearHintAfter() tea.Cmd {
	return tea.Tick(350*time.Millisecond, func(_ time.Time) tea.Msg {
		return HintClearMsg{}
	})
}
