package indexer

import (
	"fmt"
	"html"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"

	"github.com/asciimoo/nogon/config"
	"github.com/asciimoo/nogon/server/document"
	"github.com/asciimoo/nogon/server/testutil"
	"github.com/asciimoo/nogon/server/vectorstore"

	"github.com/charmbracelet/x/ansi"
)

func snippetConfig(t *testing.T, length int) *config.Config {
	t.Helper()
	cfg := testutil.Config(t)
	cfg.Indexer.DetectLanguages = false
	if length > 0 {
		cfg.App.SnippetLength = length
	}
	return cfg
}

func plainSnippet(s, style string) string {
	if style == "HTML" {
		s = html.UnescapeString(strings.NewReplacer("<mark>", "", "</mark>", "").Replace(s))
	} else {
		s = ansi.Strip(s)
	}
	return strings.Trim(s, "…")
}

func TestSearchSnippetLength(t *testing.T) {
	for _, length := range []int{0, 80, 800} {
		t.Run(fmt.Sprint(length), func(t *testing.T) {
			idx := newTestIndexer(t, snippetConfig(t, length))
			defer idx.Close()
			body := strings.Repeat("előtte ", 150) + "needle <script>alert(1)</script> " + strings.Repeat("utána ", 150)
			if err := idx.Add(&document.Document{URL: "https://example.com/context", Title: "Article", Text: body, Processed: true}); err != nil {
				t.Fatal(err)
			}
			want := length
			if want == 0 {
				want = 300
			}
			for _, style := range []string{"HTML", "text", "tui"} {
				for _, query := range []string{"needle", "title:Article"} {
					if style != "HTML" && query == "title:Article" {
						continue // Terminal styles only return fragments for matching fields.
					}
					t.Run(style+"/"+query, func(t *testing.T) {
						res, err := idx.Search(&Query{Text: query, Highlight: style})
						if err != nil || len(res.Documents) != 1 {
							t.Fatalf("search = %v, %v", res, err)
						}
						snippet := res.Documents[0].Text
						plain := plainSnippet(snippet, style)
						if !utf8.ValidString(snippet) || utf8.RuneCountInString(plain) != want {
							t.Errorf("snippet length = %d, want %d; valid UTF-8 = %v", utf8.RuneCountInString(plain), want, utf8.ValidString(snippet))
						}
						if query == "needle" {
							if !strings.Contains(plain, "előtte") || !strings.Contains(plain, "utána") || !strings.Contains(plain, "needle") {
								t.Errorf("snippet lacks surrounding match context: %q", snippet)
							}
							if style == "HTML" && (!strings.Contains(snippet, "<mark>needle</mark>") || strings.Contains(snippet, "<script>")) {
								t.Errorf("HTML highlighting or escaping was lost: %q", snippet)
							}
						}
					})
				}
			}
			res, err := idx.Search(&Query{Text: "needle", Highlight: "HTML", IncludeText: true})
			if err != nil || len(res.Documents) != 1 || res.Documents[0].Text != body {
				t.Fatalf("IncludeText did not return the full original text: %v, %v", res, err)
			}
			const shortText = "tiny rövid"
			if err := idx.Add(&document.Document{URL: "https://example.com/short", Text: shortText, Processed: true}); err != nil {
				t.Fatal(err)
			}
			res, err = idx.Search(&Query{Text: "tiny", Highlight: "HTML"})
			if err != nil || len(res.Documents) != 1 || res.Documents[0].Text != "<mark>tiny</mark> rövid" {
				t.Fatalf("short snippet was truncated or padded: %v, %v", res, err)
			}
		})
	}
}

func TestSnippetLengthIsIndependentAndNeedsNoReindex(t *testing.T) {
	cfg := snippetConfig(t, 80)
	idx := newTestIndexer(t, cfg)
	body := "needle " + strings.Repeat("context ", 150)
	if err := idx.Add(&document.Document{URL: "https://example.com/context", Text: body, Processed: true}); err != nil {
		t.Fatal(err)
	}
	idx.Close()
	cfg.App.SnippetLength = 800
	idx = newTestIndexer(t, cfg)
	defer idx.Close()
	other := newTestIndexer(t, snippetConfig(t, 80))
	defer other.Close()
	if err := other.Add(&document.Document{URL: "https://example.com/context", Text: body, Processed: true}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		idx  *Indexer
		want int
	}{{idx, 800}, {other, 80}, {idx, 800}} {
		res, err := tc.idx.Search(&Query{Text: "needle", Highlight: "HTML"})
		if err != nil || len(res.Documents) != 1 {
			t.Fatalf("search = %v, %v", res, err)
		}
		if got := utf8.RuneCountInString(plainSnippet(res.Documents[0].Text, "HTML")); got != tc.want {
			t.Errorf("snippet length = %d, want %d", got, tc.want)
		}
	}
}

type snippetVectorStore struct {
	metadataVectorStore
	results []vectorstore.Result
}

func (s *snippetVectorStore) Search([]float32, int, float64, uint) ([]vectorstore.Result, error) {
	return s.results, nil
}

func TestSemanticSnippetLength(t *testing.T) {
	var requests atomic.Int64
	server := embeddingTestServer(t, &requests)
	defer server.Close()
	for _, length := range []int{80, 800} {
		t.Run(fmt.Sprint(length), func(t *testing.T) {
			idx := newTestIndexer(t, snippetConfig(t, length))
			defer idx.Close()
			body := strings.Repeat("árvíztűrő ", 150)
			doc := &document.Document{URL: "https://example.com/semantic", Text: body, Processed: true}
			if err := idx.Add(doc); err != nil {
				t.Fatal(err)
			}
			idx.embedder = vectorstore.NewEmbedder(&config.SemanticSearch{
				EmbeddingEndpoint: server.URL, EmbeddingModel: "test", Dimensions: 2,
			})
			idx.vectorStore = &snippetVectorStore{results: []vectorstore.Result{{DocID: document.GetDocID(0, doc.URL), ChunkText: body, Similarity: 1}}}
			res, err := idx.Search(&Query{Text: "unmatched", SemanticEnabled: true})
			if err != nil || len(res.SemanticHits) != 1 || res.SemanticHits[0].Document == nil {
				t.Fatalf("semantic search = %+v, %v", res, err)
			}
			want := string([]rune(body)[:length]) + "…"
			if hit := res.SemanticHits[0]; hit.MatchedChunk != want || hit.Document.Text != want {
				t.Errorf("semantic previews do not use the configured %d character limit", length)
			}
		})
	}
}

func TestSemanticContentFlags(t *testing.T) {
	var requests atomic.Int64
	server := embeddingTestServer(t, &requests)
	defer server.Close()
	idx := newTestIndexer(t, snippetConfig(t, 80))
	defer idx.Close()
	body := strings.Repeat("Full document text. ", 100)
	fullHTML := "<html><body>" + body + "</body></html>"
	doc := &document.Document{
		URL: "https://example.com/semantic", Title: "Semantic result",
		Text: body, HTML: fullHTML, Processed: true,
	}
	if err := idx.Add(doc); err != nil {
		t.Fatal(err)
	}
	const chunk = "The relevant passage."
	idx.embedder = vectorstore.NewEmbedder(&config.SemanticSearch{
		EmbeddingEndpoint: server.URL, EmbeddingModel: "test", Dimensions: 2,
	})
	idx.vectorStore = &snippetVectorStore{results: []vectorstore.Result{{DocID: doc.ID(), ChunkText: chunk, Similarity: 0.9}}}
	for _, includeHTML := range []bool{false, true} {
		for _, includeText := range []bool{false, true} {
			t.Run(fmt.Sprintf("html=%t/text=%t", includeHTML, includeText), func(t *testing.T) {
				res, err := idx.Search(&Query{
					Text: "unmatched", SemanticEnabled: true, IncludeHTML: includeHTML, IncludeText: includeText,
				})
				if err != nil || len(res.SemanticHits) != 1 || res.SemanticHits[0].Document == nil {
					t.Fatalf("semantic search = %+v, %v", res, err)
				}
				hit := res.SemanticHits[0]
				wantHTML := ""
				if includeHTML {
					wantHTML = fullHTML
				}
				if hit.Document.HTML != wantHTML {
					t.Errorf("HTML length = %d, want %d", len(hit.Document.HTML), len(wantHTML))
				}
				wantText := string([]rune(body)[:80]) + "…"
				if includeText {
					wantText = body
				}
				if hit.Document.Text != wantText {
					t.Errorf("text length = %d, want %d", len(hit.Document.Text), len(wantText))
				}
				if hit.MatchedChunk != chunk || hit.Similarity != 0.9 || hit.Document.URL != doc.URL || hit.Document.Title != doc.Title {
					t.Errorf("semantic result metadata changed: %+v", hit)
				}
			})
		}
	}
}
