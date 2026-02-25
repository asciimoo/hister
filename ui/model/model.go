// SPDX-FileContributor: FlameFlag <github@flameflag.dev>
//
// SPDX-License-Identifier: AGPLv3+

package model

import (
	"fmt"
	"math/rand"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/indexer"
	"github.com/asciimoo/hister/ui/theme"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/gorilla/websocket"
	"github.com/muesli/termenv"
)

type Model struct {
	// Core UI components
	TextInput textinput.Model
	Viewport  viewport.Model
	Spinner   spinner.Model
	State     ViewState
	PrevState ViewState
	Cfg       *config.Config
	Results   *indexer.Results

	// Dimensions and readiness
	Width, Height int
	Ready         bool

	// Viewport line tracking
	LineOffsets []int
	TotalLines  int

	// WebSocket communication
	Conn    *websocket.Conn
	WsMu    sync.Mutex
	WsChan  chan tea.Msg
	WsDone  chan struct{}
	WsReady bool

	// Selection and search state
	SelectedIdx int
	Limit       int
	IsSearching bool
	SortMode    string // "" for relevance, "domain" for domain

	// Rendering
	Styles theme.Styles

	// Dialogs and overlays
	DialogMsg       string
	DialogConfirm   func() tea.Cmd
	DialogBtnIdx    int // 0=Cancel, 1=Delete
	DialogURL       string
	DialogReturnTab int // -1 = return to results/search, >=0 = stay on that tab

	// Connection state
	ConnError error
	HintFlash config.Action

	// Scrollbar interaction
	ScrollbarDragging bool

	// Mouse/overlay drag state
	OverlayOffX int
	OverlayOffY int
	IsDragging  bool
	DragStartX  int
	DragStartY  int
	DragOffX0   int
	DragOffY0   int

	// Terminal background
	IsDarkBg bool
	BgSet    bool

	// Clickable suggestion
	SuggestionHeight int

	// Theme picker state
	ThemeName          string
	ThemePickerIdx     int
	OrigThemeName      string
	ThemePickerMode    string // "auto", "dark", "light"
	ThemePickerSection int    // 0=dark, 1=light
	DarkThemeIdx       int
	LightThemeIdx      int
	OrigDarkTheme      string
	OrigLightTheme     string
	OrigColorScheme    string

	// Context menu
	MenuX, MenuY int
	MenuIdx      int // result index the menu targets
	MenuSelIdx   int // selected menu option

	// Settings panel
	SettingsIdx      int
	SettingsEditMode bool
	SettingsEditErr  string

	// Tab bar
	ActiveTab int // 0=Search, 1=History, 2=Rules, 3=Add

	// History tab
	HistoryItems   []HistoryEntry
	HistoryIdx     int
	HistoryLoading bool

	// Rules tab (form-based UI)
	RulesData           RulesResponse
	RulesIdx            int
	RulesSection        int // 0=skip, 1=priority, 2=aliases
	RulesLoading        bool
	RulesSkipInput      textinput.Model
	RulesPriorityInput  textinput.Model
	RulesAliasKeyInput  textinput.Model
	RulesAliasValInput  textinput.Model
	RulesFormFocus      int // 0=skip input, 1=priority input, 2=alias key, 3=alias val, 4=list
	RulesEditingIdx     int // -1 = adding new, >=0 = editing existing item
	RulesEditingSection int // which section is being edited (0/1/2)

	// Add tab
	AddInputs   [3]textinput.Model // url, title, text
	AddFocusIdx int
	AddStatus   string

	// Prioritize dialog
	PrioritizeURL    string
	PrioritizeInput  textinput.Model
	PrioritizeBtnIdx int // 0=Cancel, 1=Confirm

	// Tips rotation
	TipIdx int
}

func InitialModel(cfg *config.Config) *Model {
	theme.LoadUserThemes(cfg.TUI.ThemesDir)
	isDarkBg := termenv.HasDarkBackground()
	palette, name := theme.ResolvePalette(&cfg.TUI, isDarkBg)
	st := theme.BuildStyles(palette)

	ti := newInput("Search...", 200, 50, st)
	ti.Focus()

	// Add tab text inputs
	var addInputs [3]textinput.Model
	for i, ph := range []string{"URL", "Title", "Text content"} {
		addInputs[i] = newInput(ph, 500, 40, st)
	}

	// Spinner
	s := spinner.New()
	s.Spinner = spinner.Spinner{
		Frames: []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
		FPS:    80 * time.Millisecond,
	}
	s.Style = st.Spin

	m := &Model{
		TextInput:          ti,
		Spinner:            s,
		IsDarkBg:           isDarkBg,
		State:              StateInput,
		PrevState:          StateInput,
		Cfg:                cfg,
		SelectedIdx:        -1,
		DialogReturnTab:    -1,
		Limit:              10,
		WsChan:             make(chan tea.Msg, 10),
		WsDone:             make(chan struct{}),
		Styles:             st,
		ThemeName:          name,
		ThemePickerMode:    cfg.TUI.ColorScheme,
		AddInputs:          addInputs,
		RulesSkipInput:     newInput("skip pattern...", 200, 40, st),
		RulesPriorityInput: newInput("priority pattern...", 200, 40, st),
		RulesAliasKeyInput: newInput("keyword...", 200, 40, st),
		RulesAliasValInput: newInput("value...", 200, 40, st),
		RulesFormFocus:     RulesFieldList, // start on list
		RulesEditingIdx:    -1,
		PrioritizeInput:    newInput("URL pattern...", 500, 40, st),
		TipIdx:             rand.Intn(len(SearchTips)),
	}
	if m.ThemePickerMode == "" {
		m.ThemePickerMode = "auto"
	}
	m.SetTerminalBg(palette.Base00)
	return m
}

func (m *Model) ApplyTheme(p theme.Palette) {
	m.Styles = theme.BuildStyles(p)
	m.ThemeName = p.Name
	m.TextInput.PlaceholderStyle = m.Styles.Placeholder
	m.Spinner.Style = m.Styles.Spin
	m.SetTerminalBg(p.Base00)
}

func (m *Model) ScrollToSelected() {
	if m.SelectedIdx < 0 || m.SelectedIdx >= len(m.LineOffsets) {
		return
	}
	target := m.LineOffsets[m.SelectedIdx]
	vpH := m.Viewport.Height
	curY := m.Viewport.YOffset
	if target < curY {
		m.Viewport.SetYOffset(target)
	}
	if target >= curY+vpH {
		m.Viewport.SetYOffset(target - vpH + 3)
	}
}

func (m *Model) GetTotalResults() int {
	if m.Results == nil {
		return 0
	}
	c := len(m.Results.History) + len(m.Results.Documents)
	if c > m.Limit {
		return m.Limit + 1
	}
	return c
}

func (m *Model) GetSelectedURL() string {
	if m.Results == nil || m.SelectedIdx < 0 || m.SelectedIdx == m.Limit {
		return ""
	}
	if m.SelectedIdx < len(m.Results.History) {
		return m.Results.History[m.SelectedIdx].URL
	}
	docIdx := m.SelectedIdx - len(m.Results.History)
	if docIdx < len(m.Results.Documents) {
		return m.Results.Documents[docIdx].URL
	}
	return ""
}

func (m *Model) GetSelectedTitle() string {
	if m.Results == nil || m.SelectedIdx < 0 || m.SelectedIdx == m.Limit {
		return ""
	}
	if m.SelectedIdx < len(m.Results.History) {
		return m.Results.History[m.SelectedIdx].Title
	}
	docIdx := m.SelectedIdx - len(m.Results.History)
	if docIdx < len(m.Results.Documents) {
		return m.Results.Documents[docIdx].Title
	}
	return ""
}

func (m *Model) SortedSettingsItems() []SettingsItem {
	items := make([]SettingsItem, 0, len(m.Cfg.Hotkeys.TUI))
	for k, v := range m.Cfg.Hotkeys.TUI {
		items = append(items, SettingsItem{Key: k, Action: config.Action(v)})
	}
	slices.SortFunc(items, func(a, b SettingsItem) int {
		return strings.Compare(a.Key, b.Key)
	})
	return items
}

func (m *Model) SortedAliasKeys() []string {
	keys := make([]string, 0, len(m.RulesData.Aliases))
	for k := range m.RulesData.Aliases {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func (m *Model) RulesSectionLen(section int) int {
	switch section {
	case 0:
		return len(m.RulesData.Skip)
	case 1:
		return len(m.RulesData.Priority)
	case 2:
		return len(m.RulesData.Aliases)
	}
	return 0
}

func (m *Model) OpenDeleteDialog(title, label string, returnTab int, confirm func() tea.Cmd) {
	m.OverlayOffX, m.OverlayOffY = 0, 0
	m.State = StateDialog
	m.DialogMsg = title
	m.DialogURL = label
	m.DialogBtnIdx = 0
	m.DialogReturnTab = returnTab
	m.DialogConfirm = confirm
}

func (m *Model) OpenThemePicker() {
	m.OrigThemeName = m.ThemeName
	m.OrigDarkTheme = m.Cfg.TUI.DarkTheme
	m.OrigLightTheme = m.Cfg.TUI.LightTheme
	m.OrigColorScheme = m.Cfg.TUI.ColorScheme
	darkNames, lightNames := theme.ClassifyThemes()
	m.DarkThemeIdx = 0
	for i, name := range darkNames {
		if name == m.Cfg.TUI.DarkTheme {
			m.DarkThemeIdx = i
			break
		}
	}
	m.LightThemeIdx = 0
	for i, name := range lightNames {
		if name == m.Cfg.TUI.LightTheme {
			m.LightThemeIdx = i
			break
		}
	}
	m.ThemePickerSection = 0
	for i, name := range theme.ThemeNames() {
		if name == m.ThemeName {
			m.ThemePickerIdx = i
			break
		}
	}
	m.OpenOverlay(StateThemePicker)
}

// DismissDialog returns to the correct state after closing a dialog.
func (m *Model) DismissDialog() {
	if m.DialogReturnTab >= 0 {
		m.ActiveTab = m.DialogReturnTab
		m.State = StateResults
		m.DialogReturnTab = -1
		return
	}
	m.State = StateResults
}

func (m *Model) SetTerminalBg(hex string) {
	if hex == "" {
		return
	}
	// sets the terminal background via OSC 11
	fmt.Fprintf(os.Stderr, "\033]11;%s\a", hex)
	m.BgSet = true
}

func (m *Model) ResetTerminalBg() {
	if m.BgSet {
		// restores the terminal's original background via OSC 111
		fmt.Fprint(os.Stderr, "\033]111\a")
		m.BgSet = false
	}
}

func (m *Model) Close() {
	m.ResetTerminalBg()
	close(m.WsDone)
}

func (m *Model) FocusedRulesInput() *textinput.Model {
	switch m.RulesFormFocus {
	case RulesFieldSkip:
		return &m.RulesSkipInput
	case RulesFieldPriority:
		return &m.RulesPriorityInput
	case RulesFieldAliasKey:
		return &m.RulesAliasKeyInput
	case RulesFieldAliasVal:
		return &m.RulesAliasValInput
	}
	return nil
}

// removes focus from all rules form inputs
func (m *Model) BlurAllRulesInputs() {
	m.RulesSkipInput.Blur()
	m.RulesPriorityInput.Blur()
	m.RulesAliasKeyInput.Blur()
	m.RulesAliasValInput.Blur()
}

func ScrollIdx(idx *int, delta, minVal, maxVal int) bool {
	n := max(minVal, min(maxVal, *idx+delta))
	if n == *idx {
		return false
	}
	*idx = n
	return true
}

// OpenOverlay sets up common overlay state.
func (m *Model) OpenOverlay(state ViewState) {
	m.OverlayOffX, m.OverlayOffY = 0, 0
	m.PrevState, m.State = m.State, state
	m.TextInput.Blur()
}

// returns the result index at the given content Y offset,
// or -1 if no result is found.
func (m *Model) FindResultAtY(contentY int) int {
	for i := len(m.LineOffsets) - 1; i >= 0; i-- {
		if m.LineOffsets[i] <= contentY {
			return i
		}
	}
	return -1
}

func newInput(placeholder string, charLimit int, width int, st theme.Styles) textinput.Model {
	inp := textinput.New()
	inp.Placeholder = placeholder
	inp.CharLimit = charLimit
	inp.Width = width
	inp.Prompt = ""
	inp.PlaceholderStyle = st.Placeholder
	return inp
}
