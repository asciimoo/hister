package indexer

import (
	"testing"
	"time"

	"github.com/asciimoo/nogon/server/document"
	"github.com/asciimoo/nogon/server/testutil"
)

func TestGetLatestDocumentsFiltered(t *testing.T) {
	cfg := testutil.Config(t)
	idx := newTestIndexer(t, cfg)
	defer idx.Close()

	docs := []*document.Document{
		{
			URL:       "https://example.com/go",
			Title:     "Golang Test Guide",
			Text:      "Go documentation",
			Processed: true,
		},
		{
			URL:       "https://example.com/Rust/Testing",
			Title:     "Rust Guide",
			Text:      "Rust documentation",
			Processed: true,
		},
		{
			URL:       "https://example.com/other",
			Title:     "Other result",
			Text:      "Unrelated documentation",
			Processed: true,
		},
	}
	for _, doc := range docs {
		if err := idx.Add(doc); err != nil {
			t.Fatalf("Add failed: %v", err)
		}
	}

	tests := []struct {
		name    string
		filter  string
		wantURL string
	}{
		{"title phrase", "golang test", docs[0].URL},
		{"partial title", "olang", docs[0].URL},
		{"URL is case insensitive", "rust/testing", docs[1].URL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := idx.GetLatestDocumentsFiltered(100, "", 0, tt.filter)
			if result == nil {
				t.Fatal("filtered result is nil")
			}
			if len(result.Documents) != 1 {
				t.Fatalf("document count = %d, want 1", len(result.Documents))
			}
			if result.Documents[0].URL != tt.wantURL {
				t.Fatalf("URL = %q, want %q", result.Documents[0].URL, tt.wantURL)
			}
		})
	}
}

func TestHistoryTimelineAndDateFilter(t *testing.T) {
	cfg := testutil.Config(t)
	idx := newTestIndexer(t, cfg)
	defer idx.Close()

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	docs := []*document.Document{
		{
			URL:       "https://example.com/today",
			Title:     "Timeline today",
			Text:      "Timeline document",
			Added:     today.Add(time.Hour).Unix(),
			Updated:   today.Add(time.Hour).Unix(),
			Processed: true,
		},
		{
			URL:       "https://example.com/ten-days-ago",
			Title:     "Timeline ten days ago",
			Text:      "Timeline document",
			Added:     today.AddDate(0, 0, -10).Unix(),
			Updated:   today.AddDate(0, 0, -10).Unix(),
			Processed: true,
		},
		{
			URL:       "https://example.com/older",
			Title:     "Timeline older",
			Text:      "Timeline document",
			Added:     today.AddDate(0, 0, -100).Unix(),
			Updated:   today.AddDate(0, 0, -100).Unix(),
			Processed: true,
		},
	}
	for _, doc := range docs {
		if err := idx.Add(doc); err != nil {
			t.Fatalf("Add failed: %v", err)
		}
	}

	result, err := idx.GetHistoryTimeline(0, "Timeline", time.UTC)
	if err != nil {
		t.Fatalf("GetHistoryTimeline failed: %v", err)
	}
	if result.Days[0].Count != 1 {
		t.Fatalf("today count = %d, want 1", result.Days[0].Count)
	}
	if result.Older.Count != 2 {
		t.Fatalf("older count = %d, want 2", result.Older.Count)
	}
	var populatedMonthFrom, populatedMonthTo int64
	var populatedMonthCount int
	for _, bucket := range result.Months {
		if docs[1].Updated >= bucket.From && docs[1].Updated < bucket.To {
			populatedMonthFrom = bucket.From
			populatedMonthTo = bucket.To
			populatedMonthCount = bucket.Count
			break
		}
	}
	if populatedMonthCount == 0 {
		t.Fatal("no populated monthly bucket")
	}
	dailyResult, err := idx.GetHistoryTimelineDays(
		0,
		"Timeline",
		time.UTC,
		populatedMonthFrom,
		populatedMonthTo,
	)
	if err != nil {
		t.Fatalf("GetHistoryTimelineDays failed: %v", err)
	}
	if len(dailyResult.Days) == 0 || len(dailyResult.Days) > 31 {
		t.Fatalf("drilldown day count = %d, want between 1 and 31", len(dailyResult.Days))
	}
	drilldownCount := 0
	for _, bucket := range dailyResult.Days {
		drilldownCount += bucket.Count
	}
	if drilldownCount != populatedMonthCount {
		t.Fatalf("drilldown result count = %d, want %d", drilldownCount, populatedMonthCount)
	}

	filtered := idx.GetLatestDocumentsFilteredByDate(
		100,
		"",
		0,
		"Timeline",
		result.Days[0].From,
		result.Days[0].To,
	)
	if filtered == nil || len(filtered.Documents) != 1 {
		t.Fatalf("daily document count = %v, want 1", filtered)
	}
	if filtered.Documents[0].URL != docs[0].URL {
		t.Fatalf("daily document URL = %q, want %q", filtered.Documents[0].URL, docs[0].URL)
	}
}

func TestLatestDocumentsDatePaginationWithEqualTimestamps(t *testing.T) {
	cfg := testutil.Config(t)
	idx := newTestIndexer(t, cfg)
	defer idx.Close()

	timestamp := time.Now().UTC().Truncate(time.Second).Unix()
	for n := range 3 {
		doc := &document.Document{
			URL:       "https://example.com/page-" + string(rune('a'+n)),
			Title:     "Pagination result",
			Text:      "Pagination document",
			Added:     timestamp,
			Updated:   timestamp,
			Processed: true,
		}
		if err := idx.Add(doc); err != nil {
			t.Fatalf("Add failed: %v", err)
		}
	}

	first := idx.GetLatestDocumentsFilteredByDate(2, "", 0, "", timestamp, timestamp+1)
	if first == nil || len(first.Documents) != 2 || first.PageKey == "" {
		t.Fatalf("first page = %+v, want two documents and a cursor", first)
	}
	second := idx.GetLatestDocumentsFilteredByDate(2, first.PageKey, 0, "", timestamp, timestamp+1)
	if second == nil || len(second.Documents) != 1 {
		t.Fatalf("second page = %+v, want one document", second)
	}
	seen := map[string]bool{}
	for _, doc := range append(first.Documents, second.Documents...) {
		seen[doc.URL] = true
	}
	if len(seen) != 3 {
		t.Fatalf("unique paginated documents = %d, want 3", len(seen))
	}
}
