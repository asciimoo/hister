//go:build live

package extractor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/crawler"
	"github.com/asciimoo/hister/server/document"
	"github.com/asciimoo/hister/server/extractor/sdk"
)

const liveUserAgent = "HisterLiveExtractorTest/1.0 (+https://github.com/asciimoo/nogon)"

func TestLiveExtractors(t *testing.T) {
	manifest, err := loadLiveManifest()
	if err != nil {
		t.Fatal(err)
	}
	if err := validateLiveManifest(manifest); err != nil {
		t.Fatal(err)
	}

	filter := strings.ToLower(strings.TrimSpace(os.Getenv("HISTER_LIVE_CASE")))
	robots := crawler.NewRobotsCache(liveUserAgent)
	pageCache := make(map[string]*document.Document)
	runCount := 0
	for _, testCase := range manifest.Cases {
		if filter != "" && !strings.Contains(strings.ToLower(testCase.Name), filter) {
			continue
		}
		runCount++
		t.Run(testCase.Name, func(t *testing.T) {
			runLiveExtractorCase(t, testCase, robots, pageCache)
		})
	}
	if runCount == 0 {
		t.Fatalf("no live extractor case matched HISTER_LIVE_CASE=%q", filter)
	}
}

func runLiveExtractorCase(
	t *testing.T,
	testCase liveCase,
	robots *crawler.RobotsCache,
	pageCache map[string]*document.Document,
) {
	t.Helper()
	if testCase.RequiresBinary != "" {
		if _, err := exec.LookPath(testCase.RequiresBinary); err != nil {
			t.Skipf("required binary %q is not available", testCase.RequiresBinary)
		}
	}
	var fetched, direct, chain *document.Document
	var preview string
	var matching []string
	defer func() {
		if t.Failed() {
			writeLiveArtifacts(t, testCase, fetched, direct, chain, preview, matching)
		}
	}()

	if liveBool(testCase.Fetch, true) {
		fetched = fetchLiveDocument(t, testCase, robots, pageCache)
	} else {
		fetched = &document.Document{URL: testCase.URL}
		t.Log("crawler fetch disabled; extractor performs the live request")
	}
	if testCase.Expect.FinalURLContains != "" &&
		!strings.Contains(fetched.URL, testCase.Expect.FinalURLContains) {
		t.Errorf("fetch phase: final URL %q does not contain %q", fetched.URL, testCase.Expect.FinalURLContains)
	}
	source := fetched
	if testCase.DocumentURL != "" {
		source = cloneLiveDocument(fetched)
		source.URL = testCase.DocumentURL
	}

	for _, info := range ListMatching(source) {
		matching = append(matching, info.Name)
	}
	t.Logf("matching extractors: %s", strings.Join(matching, ", "))

	candidate := liveExtractorByName(testCase.Extractor)
	matched := candidate.Match(source)
	wantMatch := liveBool(testCase.Match, true)
	if matched != wantMatch {
		t.Fatalf("match phase: %s.Match() = %v, want %v", candidate.Name(), matched, wantMatch)
	}
	if !wantMatch {
		return
	}

	direct = cloneLiveDocument(source)
	extractResult := candidate.Extract(direct)
	decision, extractErr := extractResult.Decision(), extractResult.Err()
	wantDecision, _ := parseLiveDecision(testCase.ExtractDecision, sdk.ExtractorSuccess)
	if extractErr != nil {
		t.Errorf("direct extraction phase: %s returned %v", candidate.Name(), extractErr)
	}
	if decision != wantDecision {
		t.Errorf("direct extraction phase: decision = %v, want %v", decision, wantDecision)
	}
	assertLiveDocument(t, "direct extraction", direct, testCase.Expect)

	if liveBool(testCase.RunChain, true) {
		chain = cloneLiveDocument(source)
		if err := Extract(chain); err != nil {
			t.Errorf("chain extraction phase: %v", err)
		} else {
			assertLiveDocument(t, "chain extraction", chain, testCase.Expect)
		}
	}

	if livePreviewExpected(testCase.Expect) {
		previewResult := candidate.Preview(cloneLiveDocument(source))
		response, decision, previewErr := previewResult.Response(), previewResult.Decision(), previewResult.Err()
		preview = response.Content
		wantPreviewDecision, _ := parseLiveDecision(testCase.Expect.PreviewDecision, sdk.ExtractorSuccess)
		if previewErr != nil {
			t.Errorf("preview phase: %s returned %v", candidate.Name(), previewErr)
		}
		if decision != wantPreviewDecision {
			t.Errorf("preview phase: decision = %v, want %v", decision, wantPreviewDecision)
		}
		assertLivePreview(t, preview, testCase.Expect)
	}
}

func fetchLiveDocument(
	t *testing.T,
	testCase liveCase,
	robots *crawler.RobotsCache,
	pageCache map[string]*document.Document,
) *document.Document {
	t.Helper()
	timeout := testCase.Timeout
	if timeout <= 0 {
		timeout = 20
	}
	backend := testCase.Backend
	if backend == "" {
		backend = "http"
	}
	optionsJSON, _ := json.Marshal(testCase.BackendOptions)
	cacheKey := backend + "\n" + string(optionsJSON) + "\n" + testCase.URL
	if fetched := pageCache[cacheKey]; fetched != nil {
		t.Logf("reused %s fetched with %s backend, %d HTML bytes", fetched.URL, backend, len(fetched.HTML))
		return fetched
	}
	cfg := &config.CrawlerConfig{
		Backend:        backend,
		BackendOptions: testCase.BackendOptions,
		Timeout:        timeout,
		UserAgent:      liveUserAgent,
	}
	liveCrawler, err := crawler.New(cfg, robots)
	if err != nil {
		t.Fatalf("fetch phase: create %s crawler: %v", backend, err)
	}
	t.Cleanup(func() {
		if err := liveCrawler.Close(); err != nil {
			t.Logf("close %s crawler: %v", backend, err)
		}
	})
	validator, err := crawler.NewValidator(&crawler.ValidatorRules{MaxLinks: 1})
	if err != nil {
		t.Fatalf("fetch phase: create validator: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout+15)*time.Second)
	defer cancel()
	documents, err := liveCrawler.Crawl(ctx, testCase.URL, validator)
	if err != nil {
		t.Fatalf("fetch phase: crawl %s: %v", testCase.URL, err)
	}
	var fetched *document.Document
	for candidate := range documents {
		if fetched == nil {
			fetched = candidate
		}
	}
	if fetched == nil {
		if ctx.Err() != nil {
			t.Fatalf("fetch phase: %s: %v", testCase.URL, ctx.Err())
		}
		t.Fatalf("fetch phase: crawler returned no document for %s", testCase.URL)
	}
	t.Logf("fetched %s with %s backend, %d HTML bytes", fetched.URL, backend, len(fetched.HTML))
	pageCache[cacheKey] = fetched
	return fetched
}

func cloneLiveDocument(source *document.Document) *document.Document {
	clone := *source
	clone.Title = ""
	clone.Text = ""
	clone.Metadata = nil
	clone.ExtraDocuments = nil
	clone.Processed = false
	return &clone
}

func assertLiveDocument(t *testing.T, phase string, doc *document.Document, expected liveExpect) {
	t.Helper()
	assertContainsAll(t, phase+" title", doc.Title, expected.TitleContains)
	assertContainsAll(t, phase+" text", doc.Text, expected.TextContains)
	assertContainsNone(t, phase+" text", doc.Text, expected.TextNotContains)
	if len(doc.Text) < expected.MinTextLength {
		t.Errorf("%s: text length = %d, want at least %d", phase, len(doc.Text), expected.MinTextLength)
	}
	for key, want := range expected.Metadata {
		got, exists := doc.Metadata[key]
		if !exists {
			t.Errorf("%s: metadata %q is absent, want %#v", phase, key, want)
			continue
		}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("%s: metadata %q = %#v, want %#v", phase, key, got, want)
		}
	}
	for key, wanted := range expected.MetadataContains {
		got, exists := doc.Metadata[key]
		if !exists {
			t.Errorf("%s: metadata %q is absent", phase, key)
			continue
		}
		assertContainsAll(t, phase+" metadata "+key, fmt.Sprint(got), wanted)
	}
	for key, minimum := range expected.MetadataMinimums {
		got, exists := liveNumber(doc.Metadata[key])
		if !exists {
			t.Errorf("%s: metadata %q is not numeric", phase, key)
			continue
		}
		if got < minimum {
			t.Errorf("%s: metadata %q = %v, want at least %v", phase, key, got, minimum)
		}
	}
	for _, key := range expected.AbsentMetadata {
		if _, exists := doc.Metadata[key]; exists {
			t.Errorf("%s: metadata %q should be absent", phase, key)
		}
	}
	if expected.SkipIndexing != nil && doc.SkipIndexing != *expected.SkipIndexing {
		t.Errorf("%s: SkipIndexing = %v, want %v", phase, doc.SkipIndexing, *expected.SkipIndexing)
	}
	if len(doc.ExtraDocuments) < expected.MinExtraDocuments {
		t.Errorf(
			"%s: extra document count = %d, want at least %d",
			phase,
			len(doc.ExtraDocuments),
			expected.MinExtraDocuments,
		)
	}
	var extraTitles, extraText strings.Builder
	for _, extra := range doc.ExtraDocuments {
		extraTitles.WriteString(extra.Title)
		extraTitles.WriteByte('\n')
		extraText.WriteString(extra.Text)
		extraText.WriteByte('\n')
	}
	assertContainsAll(t, phase+" extra document titles", extraTitles.String(), expected.ExtraTitleContains)
	assertContainsAll(t, phase+" extra document text", extraText.String(), expected.ExtraTextContains)
}

func livePreviewExpected(expected liveExpect) bool {
	return expected.MinPreviewLength > 0 || len(expected.PreviewContains) > 0 ||
		len(expected.PreviewNotContains) > 0 || expected.PreviewDecision != ""
}

func assertLivePreview(t *testing.T, preview string, expected liveExpect) {
	t.Helper()
	assertContainsAll(t, "preview", preview, expected.PreviewContains)
	assertContainsNone(t, "preview", preview, expected.PreviewNotContains)
	if len(preview) < expected.MinPreviewLength {
		t.Errorf("preview length = %d, want at least %d", len(preview), expected.MinPreviewLength)
	}
}

func assertContainsAll(t *testing.T, field, value string, wanted []string) {
	t.Helper()
	for _, substring := range wanted {
		if !strings.Contains(value, substring) {
			t.Errorf("%s does not contain %q", field, substring)
		}
	}
}

func assertContainsNone(t *testing.T, field, value string, unwanted []string) {
	t.Helper()
	for _, substring := range unwanted {
		if strings.Contains(value, substring) {
			t.Errorf("%s contains %q", field, substring)
		}
	}
}

func liveNumber(value any) (float64, bool) {
	if value == nil {
		return 0, false
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(reflected.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(reflected.Uint()), true
	case reflect.Float32, reflect.Float64:
		return reflected.Float(), true
	default:
		return 0, false
	}
}

func writeLiveArtifacts(
	t *testing.T,
	testCase liveCase,
	fetched, direct, chain *document.Document,
	preview string,
	matching []string,
) {
	t.Helper()
	root := strings.TrimSpace(os.Getenv("HISTER_LIVE_ARTIFACT_DIR"))
	if root == "" {
		root = filepath.Join(os.TempDir(), "hister-live-extractors")
	}
	stamp := time.Now().UTC().Format("20060102T150405.000000000Z")
	directory := filepath.Join(root, safeLiveName(testCase.Name)+"_"+stamp)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Logf("create live artifact directory: %v", err)
		return
	}
	summary := map[string]any{
		"case":                testCase,
		"matching_extractors": matching,
	}
	if fetched != nil {
		summary["fetched_url"] = fetched.URL
		writeLiveArtifact(t, filepath.Join(directory, "page.html"), []byte(fetched.HTML))
	}
	writeLiveJSONArtifact(t, filepath.Join(directory, "summary.json"), summary)
	writeLiveDocumentArtifact(t, filepath.Join(directory, "direct.json"), direct)
	writeLiveDocumentArtifact(t, filepath.Join(directory, "chain.json"), chain)
	if preview != "" {
		writeLiveArtifact(t, filepath.Join(directory, "preview.html"), []byte(preview))
	}
	t.Logf("live failure artifacts: %s", directory)
}

func safeLiveName(value string) string {
	return strings.Map(func(character rune) rune {
		if unicode.IsLetter(character) || unicode.IsDigit(character) || character == '_' {
			return character
		}
		return '_'
	}, value)
}

func writeLiveDocumentArtifact(t *testing.T, path string, doc *document.Document) {
	t.Helper()
	if doc == nil {
		return
	}
	clone := *doc
	clone.HTML = ""
	writeLiveJSONArtifact(t, path, &clone)
}

func writeLiveJSONArtifact(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Logf("marshal live artifact %s: %v", path, err)
		return
	}
	writeLiveArtifact(t, path, append(data, '\n'))
}

func writeLiveArtifact(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Logf("write live artifact %s: %v", path, err)
	}
}
