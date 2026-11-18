package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"testing"

	"github.com/asciimoo/nogon/client"
	"github.com/asciimoo/nogon/server/document"
	"github.com/asciimoo/nogon/server/indexer"
	"github.com/asciimoo/nogon/server/model"
)

func contractDocument() *document.Document {
	return &document.Document{
		DocumentID: "7:https://example.com", URL: "https://example.com", Domain: "example.com",
		HTML: "<p>text</p>", HTMLKey: "html", Title: "title", Text: "text", Favicon: "icon", FaviconKey: "favicon",
		Score: 1.5, Added: 123, Updated: 456, Type: document.RemoteFile, Language: "en", UserID: 7,
		Label: "label", AddCount: 3, Metadata: map[string]any{"author": "Author", "nested": map[string]any{"number": 2}},
		SkipSensitiveCheck: true, Processed: true,
		ExtraDocuments: []*document.Document{{URL: "https://extra.example.com"}}, SkipIndexing: true,
	}
}

func contractJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func assertContractJSON(t *testing.T, got, want []byte) {
	t.Helper()
	var gotValue, wantValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(want, &wantValue); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("JSON = %s, want %s", got, want)
	}
}

// These tests live with the CLI so client tests do not import server machinery.
func TestClientSearchJSONContract(t *testing.T) {
	doc := contractDocument()
	query := &indexer.Query{
		Text: "test", Highlight: "html", Limit: 10, Sort: "date", DateFrom: 1, DateTo: 2, UserID: 7,
		SemanticEnabled: true, SemanticThreshold: 0.5, SemanticWeight: 0.4, PageKey: "next",
		IncludeHTML: true, IncludeText: true, Facets: true, FacetSizes: map[string]int{"domains": 5},
		FacetsOnly: true, MatchAll: true, PriorityPatterns: []string{"example"},
	}
	for _, result := range []*indexer.Results{
		nil,
		{},
		{Query: &indexer.Query{}},
		{Documents: []*document.Document{}, History: []*model.URLCount{}},
		{
			Total: 2, Query: query, Documents: []*document.Document{doc, nil},
			History: []*model.URLCount{{
				URL: doc.URL, Title: "Visited", Text: "Excerpt", Count: 3, Pinned: true,
				DocID: doc.ID(), Domain: "example.com", Added: 10, Updated: 20, AddCount: 4,
			}, nil},
			SearchDuration: "1ms", QuerySuggestion: "testing", PageKey: "next", SemanticEnabled: true,
			SemanticHits: []indexer.SemanticHit{{DocID: doc.ID(), Similarity: 0.9, MatchedChunk: "excerpt", Document: doc}, {DocID: "missing"}},
			Facets: &indexer.FacetsResult{
				Terms:         map[string]indexer.TermFacet{"domains": {Terms: []indexer.TermCount{{Term: "example.com", Count: 1, Label: "Example"}}, Other: 2}},
				DateHistogram: []indexer.RangeCount{{Name: "today", Count: 1}},
			},
		},
	} {
		response := contractJSON(t, result)
		queryJSON := contractJSON(t, query)
		c := client.New("http://nogon.test", client.WithHTTPClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			assertContractJSON(t, []byte(r.URL.Query().Get("query")), queryJSON)
			return jsonHTTPResponse(r, http.StatusOK, string(response)), nil
		})}))
		var request client.SearchQuery
		if err := json.Unmarshal(queryJSON, &request); err != nil {
			t.Fatal(err)
		}
		got, err := c.Search(&request)
		if err != nil {
			t.Fatal(err)
		}
		assertContractJSON(t, contractJSON(t, got), response)
		if got != nil && len(got.Documents) > 0 {
			if got.Documents[0].ID() != doc.ID() || got.Documents[0].Type.String() != doc.Type.String() {
				t.Fatal("client document identity or display type differs from the server")
			}
		}
	}
}

func TestClientDocumentSubmissionJSONContract(t *testing.T) {
	for _, doc := range []*document.Document{nil, {}, contractDocument()} {
		payload := contractJSON(t, doc)
		c := client.New("http://nogon.test", client.WithHTTPClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			assertContractJSON(t, body, payload)
			return jsonHTTPResponse(r, http.StatusCreated, ""), nil
		})}))
		if err := c.AddDocumentJSON(clientDocument(doc)); err != nil {
			t.Fatal(err)
		}
	}
}
