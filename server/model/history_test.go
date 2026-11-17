package model_test

import (
	"testing"
	"time"

	"github.com/asciimoo/nogon/server/model"
	"github.com/asciimoo/nogon/server/testutil"
)

func TestGetLatestHistoryItemsFiltered(t *testing.T) {
	testutil.InitModel(t)

	entries := []struct {
		query string
		url   string
		title string
	}{
		{"go", "https://example.com/go", "Golang Test Guide"},
		{"docs", "https://docs.example.com/rust", "Rust Guide"},
		{"coverage", "https://example.com/coverage", "100% coverage"},
		{"other", "https://example.com/other", "Other result"},
	}
	for _, entry := range entries {
		if err := model.UpdateHistory(0, entry.query, entry.url, entry.title); err != nil {
			t.Fatalf("UpdateHistory failed: %v", err)
		}
	}

	tests := []struct {
		name    string
		filter  string
		wantURL string
	}{
		{"title is case insensitive", "GOLANG TEST", "https://example.com/go"},
		{"URL is case insensitive", "DOCS.EXAMPLE.COM", "https://docs.example.com/rust"},
		{"SQL wildcards are literal", "%", "https://example.com/coverage"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items, err := model.GetLatestHistoryItemsFiltered(0, 100, 0, tt.filter)
			if err != nil {
				t.Fatalf("GetLatestHistoryItemsFiltered failed: %v", err)
			}
			if len(items) != 1 {
				t.Fatalf("item count = %d, want 1", len(items))
			}
			if items[0].URL != tt.wantURL {
				t.Fatalf("URL = %q, want %q", items[0].URL, tt.wantURL)
			}
		})
	}
}

func TestGetLatestHistoryItemsFilteredByDateUsesStableCursor(t *testing.T) {
	testutil.InitModel(t)

	entries := []struct {
		query string
		url   string
		title string
	}{
		{"today newer", "https://example.com/today-newer", "Today newer"},
		{"today older", "https://example.com/today-older", "Today older"},
		{"yesterday", "https://example.com/yesterday", "Yesterday"},
	}
	for _, entry := range entries {
		if err := model.UpdateHistory(0, entry.query, entry.url, entry.title); err != nil {
			t.Fatalf("UpdateHistory failed: %v", err)
		}
	}

	items, err := model.GetLatestHistoryItems(0, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	today := time.Date(2026, time.July, 28, 0, 0, 0, 0, time.UTC)
	timestamps := map[string]time.Time{
		entries[0].url: today.Add(2 * time.Hour),
		entries[1].url: today.Add(time.Hour),
		entries[2].url: today.Add(-time.Hour),
	}
	for _, item := range items {
		if err := model.DB.Model(&model.HistoryLink{}).
			Where("id = ?", item.ID).
			UpdateColumn("updated_at", timestamps[item.URL]).Error; err != nil {
			t.Fatal(err)
		}
	}

	firstPage, err := model.GetLatestHistoryItemsFilteredByDate(
		0,
		1,
		0,
		time.Time{},
		"",
		today.Unix(),
		today.AddDate(0, 0, 1).Unix(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstPage) != 1 || firstPage[0].URL != entries[0].url {
		t.Fatalf("first page = %+v, want newer item", firstPage)
	}

	secondPage, err := model.GetLatestHistoryItemsFilteredByDate(
		0,
		1,
		firstPage[0].ID,
		firstPage[0].UpdatedAt,
		"",
		today.Unix(),
		today.AddDate(0, 0, 1).Unix(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(secondPage) != 1 || secondPage[0].URL != entries[1].url {
		t.Fatalf("second page = %+v, want older item", secondPage)
	}
}
