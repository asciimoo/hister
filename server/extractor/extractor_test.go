package extractor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/document"
	"github.com/asciimoo/hister/server/extractor/sdk"
)

type stubExtractor struct {
	name         string
	capabilities sdk.Capabilities
	cfg          *config.Extractor
	match        func(*document.Document) bool
	extract      func(*document.Document) sdk.ExtractResult
	preview      func(*document.Document) sdk.PreviewResult
}

type contextStubExtractor struct {
	*stubExtractor
	extractContext func(context.Context, *document.Document) sdk.ExtractResult
	previewContext func(context.Context, *document.Document) sdk.PreviewResult
}

func (e *stubExtractor) Name() string { return e.name }

func (e *stubExtractor) Description() string { return e.name }

func (e *stubExtractor) Capabilities() sdk.Capabilities { return e.capabilities }

func (e *stubExtractor) Match(d *document.Document) bool {
	return e.match == nil || e.match(d)
}

func (e *stubExtractor) Extract(d *document.Document) sdk.ExtractResult {
	if e.extract == nil {
		return sdk.ExtractFallback(nil)
	}
	return e.extract(d)
}

func (e *stubExtractor) Preview(d *document.Document) sdk.PreviewResult {
	if e.preview == nil {
		return sdk.PreviewFallback(nil)
	}
	return e.preview(d)
}

func (e *contextStubExtractor) ExtractContext(ctx context.Context, d *document.Document) sdk.ExtractResult {
	if e.extractContext == nil {
		return e.Extract(d)
	}
	return e.extractContext(ctx, d)
}

func (e *contextStubExtractor) PreviewContext(ctx context.Context, d *document.Document) sdk.PreviewResult {
	if e.previewContext == nil {
		return e.Preview(d)
	}
	return e.previewContext(ctx, d)
}

func (e *stubExtractor) GetConfig() *config.Extractor {
	if e.cfg == nil {
		return &config.Extractor{Enable: true, Options: map[string]any{}}
	}
	return e.cfg
}

func (e *stubExtractor) SetConfig(cfg *config.Extractor) error {
	e.cfg = cfg
	return nil
}

func useExtractors(t *testing.T, replacements ...Extractor) *Registry {
	t.Helper()
	registry, err := NewRegistry(replacements...)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func TestReadabilityPreviewSanitizesMetaRefresh(t *testing.T) {
	doc := &document.Document{
		URL: "file:///tmp/index.html",
		HTML: `<!doctype html>
<html>
  <head>
    <title>Local docs</title>
    <meta http-equiv="refresh" content="0; url=EEx.html">
  </head>
  <body>
    <article>
      <h1>Local docs</h1>
      <p>This document has enough readable content for the readability extractor to render a preview.</p>
      <p>The preview must not preserve navigation tags from the original head.</p>
    </article>
  </body>
</html>`,
	}

	result := (&readabilityExtractor{}).Preview(doc)
	if result.Err() != nil {
		t.Fatalf("Preview failed: %v", result.Err())
	}
	if result.Decision() != sdk.ExtractorSuccess {
		t.Fatalf("decision = %v, want %v", result.Decision(), sdk.ExtractorSuccess)
	}
	resp := result.Response()
	lower := strings.ToLower(resp.Content)
	for _, disallowed := range []string{"http-equiv", "refresh", "eex.html"} {
		if strings.Contains(lower, disallowed) {
			t.Fatalf("preview content contains %q:\n%s", disallowed, resp.Content)
		}
	}
	if !strings.Contains(resp.Content, "readable content") {
		t.Fatalf("preview content missing article text:\n%s", resp.Content)
	}
}

func TestBasicPreviewEscapesMarkup(t *testing.T) {
	doc := &document.Document{
		Text: `<p>safe text</p><meta http-equiv="refresh" content="0; url=EEx.html">`,
	}

	result := (&basicExtractor{}).Preview(doc)
	if result.Err() != nil {
		t.Fatalf("Preview failed: %v", result.Err())
	}
	if result.Decision() != sdk.ExtractorSuccess {
		t.Fatalf("decision = %v, want %v", result.Decision(), sdk.ExtractorSuccess)
	}
	resp := result.Response()
	for _, disallowed := range []string{"<p>", "<meta", `http-equiv="refresh"`} {
		if strings.Contains(resp.Content, disallowed) {
			t.Fatalf("preview content contains %q:\n%s", disallowed, resp.Content)
		}
	}
	for _, want := range []string{"&lt;p&gt;safe text&lt;/p&gt;", "&lt;meta"} {
		if !strings.Contains(resp.Content, want) {
			t.Fatalf("preview content missing escaped markup %q:\n%s", want, resp.Content)
		}
	}
	if !strings.Contains(resp.Content, "safe text") {
		t.Fatalf("preview content missing text:\n%s", resp.Content)
	}
}

func TestEnrichersRunBeforeContentExtractor(t *testing.T) {
	doc := &document.Document{
		URL: "https://forum.example.com/t/topic/42",
		HTML: `<html><head>
			<meta name="generator" content="Discourse 2026.8.0">
			<script type="application/ld+json">{
				"@context": "https://schema.org",
				"@type": "QAPage",
				"name": "Topic"
			}</script>
		</head><body>
			<div id="topic-title"><h1 data-topic-id="42"><a>Topic</a></h1></div>
			<div class="post-stream">
				<div class="topic-post" data-post-number="1">
					<article data-post-id="100">
						<div class="names"><span class="username"><a>author</a></span></div>
						<div class="cooked"><p>Topic body.</p></div>
					</article>
				</div>
			</div>
		</body></html>`,
	}

	if err := Extract(doc); err != nil {
		t.Fatalf("Extract failed: %v", err)
	}
	if got := doc.Metadata["type"]; got != "discourse" {
		t.Fatalf("metadata type = %#v, want discourse", got)
	}
	if _, exists := doc.Metadata["jsonld"]; !exists {
		t.Fatal("JSON LD enricher did not run before Discourse selected the content")
	}
}

func TestRegisteredExtractorCapabilities(t *testing.T) {
	for _, candidate := range DefaultRegistry().Extractors() {
		caps := candidate.Capabilities()
		if !caps.Enrich && !caps.Extract && !caps.Preview {
			t.Errorf("%s declares no capabilities", candidate.Name())
		}

		switch candidate.Name() {
		case "EmbeddedVideo", "JSONLD":
			if !caps.Enrich || caps.Extract || caps.Preview {
				t.Errorf("%s capabilities = %+v, want enrichment only", candidate.Name(), caps)
			}
		case "Markdown", "OrgMode", "Email", "GoDoc":
			if caps.Enrich || caps.Extract || !caps.Preview {
				t.Errorf("%s capabilities = %+v, want preview only", candidate.Name(), caps)
			}
		default:
			if caps.Enrich || !caps.Extract || !caps.Preview {
				t.Errorf("%s capabilities = %+v, want content and preview", candidate.Name(), caps)
			}
		}
	}
}

func TestDefaultRegistryOrder(t *testing.T) {
	want := []string{
		"Markdown",
		"OrgMode",
		"EmbeddedVideo",
		"Discourse",
		"JSONLD",
		"Reddit",
		"StackExchange",
		"GoDoc",
		"GitHub",
		"Lobsters",
		"HackerNews",
		"Wikipedia",
		"Mastodon",
		"Bluesky",
		"Twitter",
		"Notion",
		"Ytdlp",
		"ChatGPT",
		"Email",
		"Readability",
		"Basic",
	}
	registered := DefaultRegistry().Extractors()
	if len(registered) != len(want) {
		t.Fatalf("registered extractor count = %d, want %d", len(registered), len(want))
	}
	for i, candidate := range registered {
		if candidate.Name() != want[i] {
			t.Errorf("extractor %d = %q, want %q", i, candidate.Name(), want[i])
		}
	}
}

func TestListMatchingPreviewOmitsEnrichers(t *testing.T) {
	doc := &document.Document{
		URL: "https://example.com/article",
		HTML: `<html><head><script type="application/ld+json">{"@type":"Article"}</script></head>
			<body><iframe src="https://www.youtube.com/embed/example"></iframe></body></html>`,
	}

	all := make(map[string]bool)
	for _, info := range ListMatching(doc) {
		all[info.Name] = true
	}
	for _, name := range []string{"EmbeddedVideo", "JSONLD"} {
		if !all[name] {
			t.Fatalf("ListMatching omitted matching enricher %s", name)
		}
	}

	for _, info := range ListMatchingPreview(doc) {
		if !info.Capabilities.Preview {
			t.Errorf("ListMatchingPreview returned non-preview extractor %s", info.Name)
		}
		if info.Name == "EmbeddedVideo" || info.Name == "JSONLD" {
			t.Errorf("ListMatchingPreview returned enricher %s", info.Name)
		}
	}
}

func TestExplicitPreviewRejectsExtractorWithoutPreviewCapability(t *testing.T) {
	doc := &document.Document{
		URL:  "https://example.com/article",
		HTML: `<script type="application/ld+json">{"@type":"Article"}</script>`,
	}

	_, err := Preview(doc, "JSONLD")
	if !errors.Is(err, ErrNoExtractor) {
		t.Fatalf("Preview error = %v, want ErrNoExtractor", err)
	}
}

func TestInvalidExtractionResultStopsTheChain(t *testing.T) {
	invalid := &stubExtractor{
		name:         "Invalid",
		capabilities: sdk.Capabilities{Extract: true},
		extract: func(*document.Document) sdk.ExtractResult {
			return sdk.ExtractResult{}
		},
	}
	registry := useExtractors(t, invalid)

	err := registry.Extract(&document.Document{URL: "https://example.com"})
	if !errors.Is(err, ErrInvalidExtractorResult) {
		t.Fatalf("Extract error = %v, want ErrInvalidExtractorResult", err)
	}
}

func TestExplicitPreviewFallsBack(t *testing.T) {
	first := &stubExtractor{
		name:         "First",
		capabilities: sdk.Capabilities{Preview: true},
		preview: func(*document.Document) sdk.PreviewResult {
			return sdk.PreviewFallback(errors.New("preview declined"))
		},
	}
	second := &stubExtractor{
		name:         "Second",
		capabilities: sdk.Capabilities{Preview: true},
		preview: func(*document.Document) sdk.PreviewResult {
			return sdk.Previewed(sdk.PreviewResponse{Content: "fallback preview"})
		},
	}
	registry := useExtractors(t, first, second)
	doc := &document.Document{URL: "https://example.com"}

	response, err := registry.Preview(doc, "First")
	if err != nil {
		t.Fatalf("Preview failed: %v", err)
	}
	if response.Content != "fallback preview" {
		t.Fatalf("Preview content = %q, want fallback preview", response.Content)
	}
}

func TestExplicitPreviewRequiresEnabledMatchingExtractor(t *testing.T) {
	t.Run("disabled", func(t *testing.T) {
		candidate := &stubExtractor{
			name:         "Disabled",
			capabilities: sdk.Capabilities{Preview: true},
			cfg:          &config.Extractor{Enable: false, Options: map[string]any{}},
		}
		registry := useExtractors(t, candidate)
		_, err := registry.Preview(&document.Document{URL: "https://example.com"}, candidate.Name())
		if !errors.Is(err, ErrNoExtractor) {
			t.Fatalf("Preview error = %v, want ErrNoExtractor", err)
		}
	})

	t.Run("not matching", func(t *testing.T) {
		candidate := &stubExtractor{
			name:         "NotMatching",
			capabilities: sdk.Capabilities{Preview: true},
			match:        func(*document.Document) bool { return false },
		}
		registry := useExtractors(t, candidate)
		_, err := registry.Preview(&document.Document{URL: "https://example.com"}, candidate.Name())
		if !errors.Is(err, ErrNoExtractor) {
			t.Fatalf("Preview error = %v, want ErrNoExtractor", err)
		}
	})
}

func TestExtractorContextPropagation(t *testing.T) {
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "request value")
	content := &contextStubExtractor{
		stubExtractor: &stubExtractor{
			name:         "Content",
			capabilities: sdk.Capabilities{Extract: true},
		},
		extractContext: func(received context.Context, _ *document.Document) sdk.ExtractResult {
			if got := received.Value(contextKey{}); got != "request value" {
				t.Fatalf("extract context value = %v, want request value", got)
			}
			return sdk.Extracted()
		},
	}
	preview := &contextStubExtractor{
		stubExtractor: &stubExtractor{
			name:         "Preview",
			capabilities: sdk.Capabilities{Preview: true},
		},
		previewContext: func(received context.Context, _ *document.Document) sdk.PreviewResult {
			if got := received.Value(contextKey{}); got != "request value" {
				t.Fatalf("preview context value = %v, want request value", got)
			}
			return sdk.Previewed(sdk.PreviewResponse{Content: "preview"})
		},
	}
	registry := useExtractors(t, content, preview)

	if err := registry.ExtractContext(ctx, &document.Document{URL: "https://example.com"}); err != nil {
		t.Fatalf("Extract failed: %v", err)
	}
	if _, err := registry.PreviewContext(ctx, &document.Document{URL: "https://example.com"}, ""); err != nil {
		t.Fatalf("Preview failed: %v", err)
	}
}

func TestCanceledContextStopsExtractorFallback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	first := &stubExtractor{
		name:         "First",
		capabilities: sdk.Capabilities{Extract: true},
		extract: func(*document.Document) sdk.ExtractResult {
			cancel()
			return sdk.ExtractFallback(context.Canceled)
		},
	}
	secondCalled := false
	second := &stubExtractor{
		name:         "Second",
		capabilities: sdk.Capabilities{Extract: true},
		extract: func(*document.Document) sdk.ExtractResult {
			secondCalled = true
			return sdk.Extracted()
		},
	}
	registry := useExtractors(t, first, second)

	err := registry.ExtractContext(ctx, &document.Document{URL: "https://example.com"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Extract error = %v, want context.Canceled", err)
	}
	if secondCalled {
		t.Fatal("fallback extractor ran after cancellation")
	}
}

func TestRegisterBeforeControlsChainOrder(t *testing.T) {
	fallback := &stubExtractor{
		name:         "Fallback",
		capabilities: sdk.Capabilities{Extract: true},
		extract: func(d *document.Document) sdk.ExtractResult {
			d.Text = "fallback"
			return sdk.Extracted()
		},
	}
	preferred := &stubExtractor{
		name:         "Preferred",
		capabilities: sdk.Capabilities{Extract: true},
		extract: func(d *document.Document) sdk.ExtractResult {
			d.Text = "preferred"
			return sdk.Extracted()
		},
	}
	registry := useExtractors(t, fallback)
	if err := registry.RegisterBefore("fallback", preferred); err != nil {
		t.Fatal(err)
	}

	doc := &document.Document{URL: "https://example.com"}
	if err := registry.Extract(doc); err != nil {
		t.Fatal(err)
	}
	if doc.Text != "preferred" {
		t.Fatalf("extracted text = %q, want preferred", doc.Text)
	}
}

func TestRegistryRejectsInvalidRegistration(t *testing.T) {
	registered := &stubExtractor{
		name:         "Registered",
		capabilities: sdk.Capabilities{Extract: true},
	}
	registry := useExtractors(t, registered)

	duplicate := &stubExtractor{
		name:         "registered",
		capabilities: sdk.Capabilities{Preview: true},
	}
	if err := registry.Register(duplicate); !errors.Is(err, ErrDuplicateExtractor) {
		t.Fatalf("duplicate registration error = %v, want ErrDuplicateExtractor", err)
	}

	invalid := &stubExtractor{name: "Invalid"}
	if err := registry.Register(invalid); !errors.Is(err, ErrInvalidExtractor) {
		t.Fatalf("invalid registration error = %v, want ErrInvalidExtractor", err)
	}

	var nilExtractor *stubExtractor
	if err := registry.Register(nilExtractor); !errors.Is(err, ErrInvalidExtractor) {
		t.Fatalf("nil registration error = %v, want ErrInvalidExtractor", err)
	}
}

func TestRegistryExtractorSnapshotIsIndependent(t *testing.T) {
	registered := &stubExtractor{
		name:         "Registered",
		capabilities: sdk.Capabilities{Extract: true},
	}
	registry := useExtractors(t, registered)

	snapshot := registry.Extractors()
	snapshot[0] = nil
	if got := registry.Extractors()[0]; got != registered {
		t.Fatalf("registered extractor changed through snapshot: %v", got)
	}
}

func TestRegistryInstancesHaveIndependentConfiguration(t *testing.T) {
	first := &stubExtractor{
		name:         "Custom",
		capabilities: sdk.Capabilities{Extract: true},
	}
	second := &stubExtractor{
		name:         "Custom",
		capabilities: sdk.Capabilities{Extract: true},
	}
	firstRegistry := useExtractors(t, first)
	_ = useExtractors(t, second)
	if err := firstRegistry.Init(map[string]*config.Extractor{
		"custom": {Enable: false, Options: map[string]any{}},
	}); err != nil {
		t.Fatal(err)
	}
	if first.GetConfig().Enable {
		t.Fatal("configured extractor remained enabled")
	}
	if !second.GetConfig().Enable {
		t.Fatal("second registry inherited configuration")
	}
}
