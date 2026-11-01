// SPDX-FileContributor: 4evy <git@evy.pink>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package handle

import (
	"image/color"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/asciimoo/hister/client"
	"github.com/asciimoo/hister/cmd/tui/model"
	"github.com/asciimoo/hister/cmd/tui/theme"
	"github.com/asciimoo/hister/config"

	tea "charm.land/bubbletea/v2"
)

func handleTestModel(t *testing.T) *model.Model {
	t.Helper()
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("KITTY_WINDOW_ID", "")
	t.Setenv("GHOSTTY_RESOURCES_DIR", "")
	cfg := config.CreateDefaultConfig()
	cfg.TUI = config.DefaultTUIConfig
	cfg.Hotkeys.TUI = maps.Clone(config.DefaultTUIHotkeys)
	m := model.InitialModel(cfg)
	Update(m, tea.WindowSizeMsg{Width: 120, Height: 30})
	return m
}

func TestPasteReachesFocusedBubblesComponents(t *testing.T) {
	m := handleTestModel(t)
	if cmd := Update(m, tea.PasteMsg{Content: "pasted query"}); cmd == nil {
		t.Fatal("search paste did not schedule a search")
	}
	if got := m.TextInput.Value(); got != "pasted query" {
		t.Fatalf("search input after paste = %q", got)
	}

	m.ActiveTab = model.TabAdd
	m.State = model.StateResults
	m.AddFocusIdx = 2
	m.AddText.Focus()
	Update(m, tea.PasteMsg{Content: "first line\nsecond line"})
	if got := m.AddText.Value(); got != "first line\nsecond line" {
		t.Fatalf("textarea after paste = %q", got)
	}
}

func TestBackgroundColorSelectsMatchingAutomaticTheme(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	m := handleTestModel(t)
	m.Cfg.TUI.ColorScheme = "auto"

	if cmd := Update(m, tea.BackgroundColorMsg{Color: color.White}); cmd == nil {
		t.Fatal("background color update did not request a redraw")
	}
	if m.IsDarkBg {
		t.Fatal("white terminal background was classified as dark")
	}
	if m.ThemeName != m.Cfg.TUI.LightTheme {
		t.Fatalf("theme = %q, want light theme %q", m.ThemeName, m.Cfg.TUI.LightTheme)
	}

	Update(m, tea.BackgroundColorMsg{Color: color.Black})
	if !m.IsDarkBg {
		t.Fatal("black terminal background was classified as light")
	}
	if m.ThemeName != m.Cfg.TUI.DarkTheme {
		t.Fatalf("theme = %q, want dark theme %q", m.ThemeName, m.Cfg.TUI.DarkTheme)
	}
}

func TestThemePickerCyclesThroughTerminalMode(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	m := handleTestModel(t)
	m.OpenThemePicker()

	ThemePickerKeys(m, tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	if m.ThemePickerMode != "auto" || m.Cfg.TUI.ColorScheme != "auto" {
		t.Fatalf("first mode cycle = %q/%q, want auto", m.ThemePickerMode, m.Cfg.TUI.ColorScheme)
	}

	m.ThemePickerMode = "light"
	m.Cfg.TUI.ColorScheme = "light"
	ThemePickerKeys(m, tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	if m.ThemePickerMode != theme.TerminalName || m.ThemeName != theme.TerminalName {
		t.Fatalf("wrapped mode cycle = %q/%q, want terminal", m.ThemePickerMode, m.ThemeName)
	}
}

func TestThemeListNavigationLeavesTerminalMode(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	m := handleTestModel(t)
	m.OpenThemePicker()

	ThemePickerKeys(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.ThemePickerMode != "dark" || m.Cfg.TUI.ColorScheme != "dark" {
		t.Fatalf("theme navigation left mode = %q/%q, want dark", m.ThemePickerMode, m.Cfg.TUI.ColorScheme)
	}
	if m.ThemeName == theme.TerminalName {
		t.Fatal("theme navigation did not preview the dark theme")
	}
}

func TestThemePickerCancelRestoresTerminalAppearance(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	m := handleTestModel(t)
	m.OpenThemePicker()

	p, ok := theme.GetPalette(m.Cfg.TUI.DarkTheme)
	if !ok {
		t.Fatalf("dark theme %q is unavailable", m.Cfg.TUI.DarkTheme)
	}
	m.ApplyTheme(p)
	CloseThemePickerWithRevert(m)

	if m.ThemeName != theme.TerminalName || m.Cfg.TUI.ColorScheme != theme.TerminalName {
		t.Fatalf("cancel restored %q/%q, want terminal", m.ThemeName, m.Cfg.TUI.ColorScheme)
	}
}

func TestSettingsCycleAndPersistAppearance(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("LOCALAPPDATA", configHome)
	m := handleTestModel(t)
	m.State = model.StateSettings
	m.SettingsIdx = 0

	for _, want := range []string{"auto", "dark", "light", theme.TerminalName} {
		SettingsKeys(m, tea.KeyPressMsg{Code: tea.KeyEnter})
		if m.Cfg.TUI.ColorScheme != want {
			t.Fatalf("appearance cycle = %q, want %q", m.Cfg.TUI.ColorScheme, want)
		}
	}

	data, err := os.ReadFile(filepath.Join(configHome, "hister", "tui.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "color_scheme: terminal") {
		t.Fatalf("saved appearance is not terminal:\n%s", data)
	}
}

func TestSettingsOpenThemePickerAndReturn(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	m := handleTestModel(t)
	m.OpenOverlay(model.StateSettings)

	SettingsKeys(m, tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	if m.State != model.StateThemePicker {
		t.Fatalf("theme shortcut opened %s, want theme picker", m.State)
	}
	CloseThemePickerWithRevert(m)
	if m.State != model.StateSettings {
		t.Fatalf("theme picker returned to %s, want settings", m.State)
	}
}

func TestSettingsKeybindingEditUsesBindingIndex(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("LOCALAPPDATA", configHome)
	m := handleTestModel(t)
	m.State = model.StateSettings
	items := m.SortedSettingsItems()
	if len(items) == 0 {
		t.Fatal("no keybindings available")
	}
	oldKey, action := items[0].Key, items[0].Action
	m.SettingsIdx = 1

	SettingsKeys(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.SettingsEditMode {
		t.Fatal("binding row did not enter edit mode")
	}
	SettingsKeys(m, tea.KeyPressMsg{Code: tea.KeyF12})

	if m.SettingsEditMode {
		t.Fatal("binding edit mode did not close")
	}
	if _, exists := m.Cfg.Hotkeys.TUI[oldKey]; exists {
		t.Fatalf("old key %q remains bound", oldKey)
	}
	if got := m.Cfg.Hotkeys.TUI["f12"]; got != string(action) {
		t.Fatalf("f12 binding = %q, want %q", got, action)
	}
}

func TestArrowNavigationTransfersSearchFocusToResults(t *testing.T) {
	m := handleTestModel(t)
	m.Results = &client.SearchResults{Documents: []*client.Document{
		{URL: "https://example.com/first", Title: "First"},
		{URL: "https://example.com/second", Title: "Second"},
	}}
	m.SelectedIdx = -1

	Update(m, tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))

	if m.State != model.StateResults || m.TextInput.Focused() {
		t.Fatalf("navigation left focus on search input: state=%s focused=%v", m.State, m.TextInput.Focused())
	}
	if m.SelectedIdx != 0 {
		t.Fatalf("first navigation selected index %d, want 0", m.SelectedIdx)
	}
}

func TestSearchResponseDoesNotStealSelectionWhileTyping(t *testing.T) {
	m := handleTestModel(t)
	m.TextInput.SetValue("query")
	m.SelectedIdx = -1

	Update(m, model.ResultsMsg{Results: &client.SearchResults{Documents: []*client.Document{{
		URL: "https://example.com/first", Title: "First",
	}}}})

	if m.State != model.StateInput || m.SelectedIdx != -1 || !m.TextInput.Focused() {
		t.Fatalf("search response created false result focus: state=%s selected=%d focused=%v",
			m.State, m.SelectedIdx, m.TextInput.Focused())
	}
}

func TestEmptyResponseReturnsOrphanedResultFocusToSearch(t *testing.T) {
	m := handleTestModel(t)
	m.State = model.StateResults
	m.TextInput.Blur()
	m.SelectedIdx = 0

	if cmd := Update(m, model.ResultsMsg{Results: &client.SearchResults{}}); cmd == nil {
		t.Fatal("empty response did not resume input and websocket commands")
	}
	if m.State != model.StateInput || m.SelectedIdx != -1 || !m.TextInput.Focused() {
		t.Fatalf("empty response orphaned focus: state=%s selected=%d focused=%v",
			m.State, m.SelectedIdx, m.TextInput.Focused())
	}
}

func TestEmptyResponseUpdatesResultFocusUnderNestedOverlays(t *testing.T) {
	m := handleTestModel(t)
	m.Results = &client.SearchResults{Documents: []*client.Document{{
		URL: "https://example.com/first", Title: "First",
	}}}
	m.State = model.StateResults
	m.SelectedIdx = 0
	m.TextInput.Blur()
	m.OpenOverlay(model.StateDetails)
	m.OpenOverlay(model.StateHelp)

	Update(m, model.ResultsMsg{Results: &client.SearchResults{}})
	if m.State != model.StateHelp {
		t.Fatalf("empty response dismissed visible overlay: state=%s", m.State)
	}

	_ = CloseOverlay(m)
	if m.State != model.StateDetails {
		t.Fatalf("help returned to state=%s, want details", m.State)
	}
	_ = CloseOverlay(m)
	if m.State != model.StateInput || m.SelectedIdx != -1 || !m.TextInput.Focused() {
		t.Fatalf("details returned to orphaned focus: state=%s selected=%d focused=%v",
			m.State, m.SelectedIdx, m.TextInput.Focused())
	}
}

func TestEnterAcceptsNoResultQuerySuggestion(t *testing.T) {
	m := handleTestModel(t)
	m.TextInput.SetValue("wrod")
	m.Results = &client.SearchResults{QuerySuggestion: "word"}

	if cmd := Update(m, tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})); cmd == nil {
		t.Fatal("Enter did not schedule the suggested search")
	}
	if got := m.TextInput.Value(); got != "word" {
		t.Fatalf("suggested query = %q, want word", got)
	}
	if m.State != model.StateInput || m.SelectedIdx != -1 {
		t.Fatalf("suggestion changed focus state=%s selected=%d", m.State, m.SelectedIdx)
	}
}

func TestEnterAcceptsSuggestionInsteadOfRetainedResult(t *testing.T) {
	m := handleTestModel(t)
	m.TextInput.SetValue("wrod")
	m.Results = &client.SearchResults{
		Documents:       []*client.Document{{URL: "https://example.com/result", Title: "Result"}},
		QuerySuggestion: "word",
	}
	m.SelectedIdx = 0

	if cmd := Update(m, tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})); cmd == nil {
		t.Fatal("Enter did not schedule the suggested search")
	}
	if got := m.TextInput.Value(); got != "word" {
		t.Fatalf("suggested query = %q, want word", got)
	}
	if m.State != model.StateInput || m.SelectedIdx != -1 {
		t.Fatalf("suggestion changed focus state=%s selected=%d", m.State, m.SelectedIdx)
	}
}

func TestAddValidationReturnsFocusToRequiredURL(t *testing.T) {
	m := handleTestModel(t)
	m.ActiveTab = model.TabAdd
	m.State = model.StateResults
	m.AddFocusIdx = model.AddSubmitFieldIdx

	if cmd := submitAdd(m); cmd != nil {
		t.Fatal("invalid add unexpectedly called the server")
	}
	if m.AddFocusIdx != 0 || !m.AddInputs[0].Focused() {
		t.Fatalf("URL validation left focus at %d", m.AddFocusIdx)
	}
	if m.AddStatus != "URL is required" || m.AddStatusKind != model.NoticeError {
		t.Fatalf("URL validation status = %q (%d)", m.AddStatus, m.AddStatusKind)
	}
}

func TestResultWheelTransfersFocusAndRoutesPrintableActions(t *testing.T) {
	m := handleTestModel(t)
	m.TextInput.SetValue("query")
	m.Results = &client.SearchResults{Documents: []*client.Document{
		{URL: "https://example.com/first", Title: "First"},
		{URL: "https://example.com/second", Title: "Second"},
	}}
	m.SelectedIdx = -1

	// A wheel event over the input must not silently alter the result selection.
	Update(m, tea.MouseWheelMsg(tea.Mouse{X: 10, Y: model.RowInput, Button: tea.MouseWheelDown}))
	if m.State != model.StateInput || m.SelectedIdx != -1 || !m.TextInput.Focused() {
		t.Fatalf("input wheel changed result focus: state=%s selected=%d focused=%v", m.State, m.SelectedIdx, m.TextInput.Focused())
	}

	Update(m, tea.MouseWheelMsg(tea.Mouse{X: 10, Y: model.RowVPStart, Button: tea.MouseWheelDown}))
	if m.State != model.StateResults || m.TextInput.Focused() || m.SelectedIdx != 0 {
		t.Fatalf("result wheel did not transfer focus: state=%s selected=%d focused=%v", m.State, m.SelectedIdx, m.TextInput.Focused())
	}

	before := m.TextInput.Value()
	if cmd := Update(m, tea.KeyPressMsg(tea.Key{Text: "y", Code: 'y'})); cmd == nil {
		t.Fatal("y did not dispatch the selected-result copy action")
	}
	if got := m.TextInput.Value(); got != before {
		t.Fatalf("y was inserted into search input: got %q, want %q", got, before)
	}

	if cmd := Update(m, tea.KeyPressMsg(tea.Key{Text: "v", Code: 'v'})); cmd == nil {
		t.Fatal("v did not dispatch the selected-result preview action")
	}
	if m.State != model.StateDetails || m.TextInput.Value() != before {
		t.Fatalf("v did not open preview without typing: state=%s query=%q", m.State, m.TextInput.Value())
	}
}

func TestSuccessfulDeleteClosesPreviewAndClearsTerminalResources(t *testing.T) {
	m := handleTestModel(t)
	m.State = model.StateDetails
	m.PrevState = model.StateResults
	m.DetailsURL = "https://example.com/article"

	if cmd := Update(m, model.DeleteResultMsg{}); cmd == nil {
		t.Fatal("successful delete returned no follow-up commands")
	}
	if m.State != model.StateResults || m.PrevState != model.StateResults {
		t.Fatalf("delete left state=%s previous=%s", m.State, m.PrevState)
	}
	if m.DetailsURL != "" {
		t.Fatalf("delete left stale preview url=%q", m.DetailsURL)
	}
}

func TestReloadDetailsPreservesOverlayState(t *testing.T) {
	m := handleTestModel(t)
	doc := &client.Document{URL: "https://example.com/new", Title: "New result"}
	m.Results = &client.SearchResults{Documents: []*client.Document{doc}}
	m.SelectedIdx = 0
	m.DetailsURL = "https://example.com/old"
	m.State = model.StateContextMenu
	m.PrevState = model.StateDetails

	if cmd := ReloadDetails(m); cmd == nil {
		t.Fatal("reload returned no preview command")
	}
	if m.State != model.StateContextMenu || m.PrevState != model.StateDetails {
		t.Fatalf("reload changed overlay state=%s previous=%s", m.State, m.PrevState)
	}
	if m.DetailsURL != doc.URL || m.DetailsFocused {
		t.Fatalf("reload url=%q focused=%v", m.DetailsURL, m.DetailsFocused)
	}
}

func TestHelpOverDetailsClosesBackToResults(t *testing.T) {
	m := handleTestModel(t)
	doc := &client.Document{URL: "https://example.com/article", Title: "Article"}
	m.Results = &client.SearchResults{Documents: []*client.Document{doc}}
	m.SelectedIdx = 0
	m.State = model.StateResults

	_ = OpenDetails(m)
	m.OpenOverlay(model.StateHelp)
	_ = CloseOverlay(m)
	if m.State != model.StateDetails || m.PrevState != model.StateResults {
		t.Fatalf("help returned to state=%s previous=%s", m.State, m.PrevState)
	}
	_ = CloseOverlay(m)
	if m.State != model.StateResults || m.DetailsURL != "" {
		t.Fatalf("details close left state=%s url=%q", m.State, m.DetailsURL)
	}
}

func TestPreviewRequestsAreDebouncedAndSerialized(t *testing.T) {
	m := handleTestModel(t)
	m.DetailsLoading = true
	m.DetailsURL = "https://example.com/one"
	_ = m.QueuePreviewCmd(m.DetailsURL)
	firstID := m.DetailsRequestID

	firstCmd := Update(m, model.PreviewDebounceMsg{URL: m.DetailsURL, ID: firstID})
	if firstCmd == nil || !m.DetailsFetching {
		t.Fatal("first debounced preview did not start")
	}

	m.DetailsURL = "https://example.com/two"
	_ = m.QueuePreviewCmd(m.DetailsURL)
	secondID := m.DetailsRequestID
	m.DetailsURL = "https://example.com/three"
	_ = m.QueuePreviewCmd(m.DetailsURL)
	thirdID := m.DetailsRequestID

	if cmd := Update(m, model.PreviewDebounceMsg{URL: "https://example.com/two", ID: secondID}); cmd != nil {
		t.Fatal("superseded debounce started a request")
	}
	if cmd := Update(m, model.PreviewDebounceMsg{URL: m.DetailsURL, ID: thirdID}); cmd != nil {
		t.Fatal("latest preview started while an earlier request was in flight")
	}
	if m.DetailsPendingURL != m.DetailsURL || !m.DetailsPendingReady {
		t.Fatalf("pending preview = %q ready=%v", m.DetailsPendingURL, m.DetailsPendingReady)
	}

	nextCmd := Update(m, model.PreviewFetchedMsg{URL: "https://example.com/one"})
	if nextCmd == nil || !m.DetailsFetching || m.DetailsPendingURL != "" {
		t.Fatal("latest pending preview did not start after the first request completed")
	}
	preview := &client.PreviewResponse{Title: "Latest"}
	if cmd := Update(m, model.PreviewFetchedMsg{URL: m.DetailsURL, Preview: preview}); cmd != nil {
		t.Fatal("completed latest preview unexpectedly started another request")
	}
	if m.DetailsLoading || m.DetailsFetching || m.DetailsPreview != preview {
		t.Fatalf("latest preview was not settled: loading=%v fetching=%v preview=%#v", m.DetailsLoading, m.DetailsFetching, m.DetailsPreview)
	}
}

func TestSemanticSettingsComeFromServerConfig(t *testing.T) {
	m := handleTestModel(t)
	m.Cfg.SemanticSearch.Enable = true
	m.Cfg.SemanticSearch.SemanticWeight = 0.1

	_, _ = DispatchCommonAction(m, config.ActionToggleSemantic)
	if m.SemanticOn {
		t.Fatal("local semantic configuration enabled remote search capability")
	}

	Update(m, model.ServerConfigFetchedMsg{Config: &client.ServerConfig{
		SemanticEnabled:     true,
		SemanticWeight:      0.7,
		SimilarityThreshold: 0.8,
	}})
	_, _ = DispatchCommonAction(m, config.ActionToggleSemantic)
	if !m.SemanticOn || m.SemanticWeight != 0.7 || m.SemanticThreshold != 0.8 {
		t.Fatalf("server semantic settings not applied: on=%v weight=%v threshold=%v", m.SemanticOn, m.SemanticWeight, m.SemanticThreshold)
	}
}
