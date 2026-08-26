// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import "testing"

func TestHistoryTableFromPath(t *testing.T) {
	tests := map[string]string{
		"/tmp/places.sqlite":       "moz_places",
		"/tmp/Default/History":     "urls",
		"/tmp/Ladybird/History.db": "History",
	}
	for path, want := range tests {
		got, err := historyTableFromPath(path)
		if err != nil {
			t.Fatalf("historyTableFromPath(%q) err = %v", path, err)
		}
		if got != want {
			t.Fatalf("historyTableFromPath(%q) = %q, want %q", path, got, want)
		}
	}
	if _, err := historyTableFromPath("/tmp/Bookmarks"); err == nil {
		t.Fatal("expected Bookmarks path to be rejected as history")
	}
}

func TestResolveHistoryImportsNamedDB(t *testing.T) {
	got, err := resolveHistoryImports("", "/tmp/profile/places.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].table != "moz_places" || got[0].databaseFile != "/tmp/profile/places.sqlite" {
		t.Fatalf("resolveHistoryImports() = %#v", got)
	}

	got, err = resolveHistoryImports("chrome", "/tmp/Default/History")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].table != "urls" {
		t.Fatalf("chrome History table = %#v", got)
	}
}

func TestResolveHistoryImportsRejectsUnknownBrowser(t *testing.T) {
	if _, err := resolveHistoryImports("safari", ""); err == nil {
		t.Fatal("expected safari to be rejected")
	}
}
