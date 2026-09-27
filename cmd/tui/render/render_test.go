// SPDX-FileContributor: 4evy <git@evy.pink>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package render

import (
	"maps"
	"strings"
	"testing"

	"github.com/asciimoo/hister/client"
	"github.com/asciimoo/hister/cmd/tui/model"
	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/document"
	"github.com/asciimoo/hister/server/indexer"

	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

func renderModel() *model.Model {
	cfg := config.CreateDefaultConfig()
	cfg.TUI = config.DefaultTUIConfig
	cfg.Hotkeys.TUI = maps.Clone(config.DefaultTUIHotkeys)
	m := model.InitialModel(cfg)
	m.Width = 90
	m.Height = 30
	return m
}

func TestLocalHostRecognizesStandardLoopbackForms(t *testing.T) {
	for _, host := range []string{"localhost", "LOCALHOST:8080", "127.0.0.1:8080", "[::1]:8080", "[::1%lo0]:8080", "::1"} {
		if !isLocalHost(host) {
			t.Errorf("isLocalHost(%q) = false", host)
		}
	}
	for _, host := range []string{"example.com", "192.0.2.1", "[2001:db8::1]:8080"} {
		if isLocalHost(host) {
			t.Errorf("isLocalHost(%q) = true", host)
		}
	}
}

func prepareSearchRender(m *model.Model) {
	m.Ready = true
	m.Viewport = viewport.New(viewport.WithWidth(1), viewport.WithHeight(1))
	m.Viewport.FillHeight = true
	ResizeSearchViewports(m)
	RefreshViewport(m)
	if m.DetailsURL != "" {
		m.Details.SetContent(ResultDetailsContent(m))
	}
}

func TestDetailsRenderAsRightSideSearchPane(t *testing.T) {
	m := renderModel()
	m.Width = 120
	doc := &document.Document{URL: "https://example.com/article", Title: "Left result", Text: "excerpt"}
	m.Results = &indexer.Results{Documents: []*document.Document{doc}}
	m.SelectedIdx = 0
	m.State = model.StateDetails
	m.DetailsURL = doc.URL
	m.DetailsHintTitle = doc.Title
	m.DetailsFocused = true
	m.DetailsPreview = &client.PreviewResponse{Title: "Readable preview", Content: "Full article body"}
	prepareSearchRender(m)

	view := MainView(m)
	if !strings.Contains(view, "Left result") || !strings.Contains(view, "Readable preview") || !strings.Contains(view, "Preview") {
		t.Fatalf("split view is missing results or preview:\n%s", view)
	}
	if got := lipgloss.Width(view); got != m.Width-1 {
		t.Fatalf("split view width = %d, want %d", got, m.Width-1)
	}
	if got := lipgloss.Height(view); got != m.Height {
		t.Fatalf("split view height = %d, want %d", got, m.Height)
	}
	x, _, width, _ := DetailsPaneBounds(m)
	if x <= 0 || x+width != m.Width-1 {
		t.Fatalf("details pane bounds = x%d width%d for terminal width %d", x, width, m.Width)
	}
}

func TestDetailsUseFullWidthOnNarrowTerminal(t *testing.T) {
	m := renderModel()
	m.Width = 70
	doc := &document.Document{URL: "https://example.com/article", Title: "Left-only result"}
	m.Results = &indexer.Results{Documents: []*document.Document{doc}}
	m.SelectedIdx = 0
	m.State = model.StateDetails
	m.DetailsURL = doc.URL
	m.DetailsHintTitle = doc.Title
	m.DetailsPreview = &client.PreviewResponse{Title: "Narrow preview", Content: "Readable body"}
	prepareSearchRender(m)

	view := MainView(m)
	if strings.Contains(view, "Left-only result") || !strings.Contains(view, "Narrow preview") {
		t.Fatalf("narrow view did not replace results with preview:\n%s", view)
	}
	if DetailsSplit(m) {
		t.Fatal("narrow terminal unexpectedly uses split details")
	}
}

func TestResultDetailsRendersHTMLAsReadableText(t *testing.T) {
	m := renderModel()
	m.DetailsURL = "https://example.com/article"
	m.DetailsPreview = &client.PreviewResponse{
		Title:   "Article",
		Content: `<h1>Heading</h1><p>First <strong>paragraph</strong>.</p><script>bad()</script><span aria-hidden="true">hidden</span><ul><li>One</li></ul>`,
	}
	content := ResultDetailsContent(m)

	for _, want := range []string{"Heading", "First paragraph.", "One"} {
		if !strings.Contains(content, want) {
			t.Errorf("details content is missing %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, "bad()") {
		t.Fatalf("details content includes script text:\n%s", content)
	}
}

func TestResultDetailsWrapsDocumentTextToViewport(t *testing.T) {
	m := renderModel()
	m.Width = 27
	m.DetailsURL = "https://example.com/article-with-a-very-long-path"
	m.DetailsPreview = &client.PreviewResponse{
		Title:   "A long readable article title",
		Content: "Document text should wrap at natural word boundaries instead of being clipped by the details viewport. supercalifragilisticexpialidocious",
	}

	content := ResultDetailsContent(m)
	contentWidth := DetailsPaneWidth(m) - 2
	if len(strings.Split(content, "\n")) < 8 {
		t.Fatalf("details content was not wrapped:\n%s", content)
	}
	for i, line := range strings.Split(content, "\n") {
		if width := ansi.StringWidth(line); width > contentWidth {
			t.Fatalf("line %d width = %d, want <= %d: %q", i, width, contentWidth, line)
		}
	}
}

func TestRulesTabRendersVersioningAndAllowAndEmitsTargets(t *testing.T) {
	m := renderModel()
	m.RulesData.Versioning = []string{"example.com/article/*"}
	m.RulesData.Allow = []string{"allowed.example/*"}
	view := RulesTab(m)

	if !strings.Contains(view, "Versioning Patterns") || !strings.Contains(view, "example.com/article/*") {
		t.Fatalf("rules view is missing versioning section:\n%s", view)
	}
	if !strings.Contains(view, "Allow Patterns") || !strings.Contains(view, "allowed.example/*") {
		t.Fatalf("rules view is missing allow section:\n%s", view)
	}
	if len(m.WorkspaceTargets) != 7 {
		t.Fatalf("workspace target count = %d, want 7 (five forms and two items)", len(m.WorkspaceTargets))
	}
	found := false
	foundAllow := false
	for _, target := range m.WorkspaceTargets {
		if target.Kind == model.WorkspaceRulesItem && target.Section == model.RulesSectionVersioning {
			found = true
		}
		if target.Kind == model.WorkspaceRulesItem && target.Section == model.RulesSectionAllow {
			foundAllow = true
		}
	}
	if !found {
		t.Fatal("renderer did not emit a hit target for the versioning item")
	}
	if !foundAllow {
		t.Fatal("renderer did not emit a hit target for the allow item")
	}
}

func TestAddTabUsesComponentGeometry(t *testing.T) {
	m := renderModel()
	m.AddFocusIdx = 2
	_ = AddTab(m)

	if len(m.WorkspaceTargets) != 4 {
		t.Fatalf("workspace target count = %d, want 4", len(m.WorkspaceTargets))
	}
	if got := m.WorkspaceTargets[2]; got.Kind != model.WorkspaceAddField || got.Index != 2 || got.Height < model.AddTextHeight {
		t.Fatalf("text area target = %#v", got)
	}
	if m.WorkspaceSelectionY != m.WorkspaceTargets[2].Y {
		t.Fatalf("selection y = %d, want %d", m.WorkspaceSelectionY, m.WorkspaceTargets[2].Y)
	}
}

func TestOverlayCompositorPreservesCanvasSize(t *testing.T) {
	background := strings.TrimSuffix(strings.Repeat(strings.Repeat("b", 30)+"\n", 10), "\n")
	view := renderOverlay(background, "dialog", 30, 10, 0, 0)
	if got := lipgloss.Width(view); got != 30 {
		t.Fatalf("overlay width = %d, want 30", got)
	}
	if got := lipgloss.Height(view); got != 10 {
		t.Fatalf("overlay height = %d, want 10", got)
	}
	if !strings.Contains(view, "dialog") {
		t.Fatal("composited overlay is missing foreground content")
	}
}

func TestDimCanvasPreservesNestedStyles(t *testing.T) {
	background := lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render("red") + " plain"
	canvas := lipgloss.NewCanvas(lipgloss.Width(background), 1)
	canvas.Compose(lipgloss.NewCompositor(lipgloss.NewLayer(background)))
	dimCanvas(canvas)
	for x := range canvas.Width() {
		cell := canvas.CellAt(x, 0)
		if cell == nil || cell.Style.Attrs&uv.AttrFaint == 0 {
			t.Fatalf("background cell %d was not dimmed: %#v", x, cell)
		}
	}
}

func TestScrollbarUsesViewportScrollMetrics(t *testing.T) {
	m := renderModel()
	m.Viewport = viewport.New(viewport.WithWidth(10), viewport.WithHeight(3))
	m.Viewport.SetContent("one\ntwo\nthree\nfour\nfive\nsix")

	top := strings.Split(ansi.Strip(Scrollbar(m)), "\n")
	if len(top) != 3 || top[0] != "█" {
		t.Fatalf("top scrollbar = %#v", top)
	}
	m.Viewport.GotoBottom()
	bottom := strings.Split(ansi.Strip(Scrollbar(m)), "\n")
	if len(bottom) != 3 || bottom[2] != "█" {
		t.Fatalf("bottom scrollbar = %#v", bottom)
	}
}

func TestHeaderCompactsTabsAndOwnsTheirHitboxes(t *testing.T) {
	m := renderModel()
	m.Width = 24
	header := Header(m)

	if got := lipgloss.Width(header); got != m.Width-1 {
		t.Fatalf("header width = %d, want %d", got, m.Width-1)
	}
	if !strings.Contains(header, "[S]") {
		t.Fatalf("compact header is missing active search tab: %q", header)
	}
	if len(m.TabTargets) != len(model.Tabs) {
		t.Fatalf("tab target count = %d, want %d", len(m.TabTargets), len(model.Tabs))
	}
	for i := 1; i < len(m.TabTargets); i++ {
		if m.TabTargets[i].X0 <= m.TabTargets[i-1].X1 {
			t.Fatalf("tab targets overlap: %#v", m.TabTargets)
		}
	}
}

func TestThemePickerPresentsTerminalAppearance(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	m := renderModel()
	picker := ansi.Strip(ThemePicker(m))

	if !strings.Contains(picker, "Mode: [terminal]") {
		t.Fatalf("theme picker does not present terminal mode:\n%s", picker)
	}
}

func TestSettingsExposeTerminalAppearance(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	m := renderModel()
	settings := ansi.Strip(Settings(m))

	if !strings.Contains(settings, "Appearance  Terminal (pass-through)") {
		t.Fatalf("settings do not expose terminal appearance:\n%s", settings)
	}
}

func TestComfortableHeaderEstablishesBrandAndWorkspaceHierarchy(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	m := renderModel()
	header := ansi.Strip(Header(m))

	if !strings.HasPrefix(header, " hister  [Search]") {
		t.Fatalf("comfortable header lacks brand/workspace hierarchy: %q", header)
	}
	if len(m.TabTargets) == 0 || m.TabTargets[0].X0 <= model.TabBarLeftPad {
		t.Fatalf("branded header did not shift tab hit targets: %#v", m.TabTargets)
	}
}

func TestSearchEmptyStatesExplainTheNextStep(t *testing.T) {
	m := renderModel()
	m.WsReady = true
	prepareSearchRender(m)
	initial := ansi.Strip(m.Viewport.View())
	if !strings.Contains(initial, "Search your history") || !strings.Contains(initial, "Type above") {
		t.Fatalf("initial search state is not actionable:\n%s", initial)
	}

	m.TextInput.SetValue("wrod")
	m.Results = &indexer.Results{QuerySuggestion: "word"}
	RefreshViewport(m)
	empty := ansi.Strip(m.Viewport.View())
	for _, want := range []string{"No results for “wrod”", "Did you mean “word”?", "Press Enter to try it"} {
		if !strings.Contains(empty, want) {
			t.Errorf("no-result state is missing %q:\n%s", want, empty)
		}
	}
}

func TestOfflineSearchStateExplainsRecovery(t *testing.T) {
	m := renderModel()
	m.TextInput.SetValue("kept query")
	prepareSearchRender(m)
	content := ansi.Strip(m.Viewport.View())
	if !strings.Contains(content, "Search is offline") || !strings.Contains(content, "reconnecting automatically") {
		t.Fatalf("offline state is misleading:\n%s", content)
	}
}

func TestResultSelectionDistinguishesFocusFromRetention(t *testing.T) {
	m := renderModel()
	m.Results = &indexer.Results{Documents: []*document.Document{{
		URL: "https://example.com", Title: "Selected result",
	}}}
	m.SelectedIdx = 0
	m.State = model.StateInput
	prepareSearchRender(m)
	inactive := ansi.Strip(m.Viewport.View())
	if !strings.Contains(inactive, "│") || strings.Contains(inactive, "┃") {
		t.Fatalf("retained selection looks focused:\n%s", inactive)
	}

	m.State = model.StateResults
	RefreshViewport(m)
	active := ansi.Strip(m.Viewport.View())
	if !strings.Contains(active, "┃") {
		t.Fatalf("focused result lacks a strong focus marker:\n%s", active)
	}
}

func TestSelectedResultTitleStyleResumesAfterSearchHighlight(t *testing.T) {
	m := renderModel()
	m.State = model.StateResults
	highlight := lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	doc := &document.Document{
		URL:   "https://example.com",
		Title: "Before " + highlight.Render("match") + " after",
	}

	rendered := Document(m, doc, true, 60)
	if want := m.Styles.SelTitle.Render(" after"); !strings.Contains(rendered, want) {
		t.Fatalf("selected title style was not restored after highlight reset:\n%q\nwant segment %q", rendered, want)
	}
	if got := ansi.Strip(rendered); !strings.Contains(got, "Before match after") {
		t.Fatalf("styled title changed its text: %q", got)
	}
}

func TestHeaderStatusMatchesTheActiveWorkspace(t *testing.T) {
	m := renderModel()
	m.WsReady = true
	m.ActiveTab = model.TabHistory
	m.HistoryItems = []model.HistoryItem{{}, {}}
	header := ansi.Strip(Header(m))
	if !strings.Contains(header, "2 history items") || strings.Contains(header, "results") {
		t.Fatalf("history header reports the wrong workspace status: %q", header)
	}

	m.Notice = "Could not save rules"
	m.NoticeKind = model.NoticeError
	header = ansi.Strip(Header(m))
	if !strings.Contains(header, "! Could not save rules") {
		t.Fatalf("error notice relies on color alone: %q", header)
	}
}

func TestLongOverlaysFitShortTerminal(t *testing.T) {
	m := renderModel()
	m.Height = 14

	settings := renderOverlayBox(Settings(m), m.Styles.HelpBorder, m.Width-5)
	if got := lipgloss.Height(settings); got > m.Height {
		t.Fatalf("settings overlay height = %d, terminal height = %d", got, m.Height)
	}
	themes := renderOverlayBox(ThemePicker(m), m.Styles.ThemeBorder, m.Width-5)
	if got := lipgloss.Height(themes); got > m.Height {
		t.Fatalf("theme overlay height = %d, terminal height = %d", got, m.Height)
	}
}
