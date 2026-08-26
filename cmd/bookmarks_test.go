// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"testing"

	"github.com/asciimoo/hister/pkg/browser/bookmarks/chromium"
	"github.com/asciimoo/hister/pkg/browser/bookmarks/firefox"
	"github.com/asciimoo/hister/pkg/browser/bookmarks/ladybird"
)

func TestResolveBookmarkStoresNamedDB(t *testing.T) {
	got, err := resolveBookmarkStores("", "/tmp/profile/places.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Path != "/tmp/profile/places.sqlite" {
		t.Fatalf("firefox store = %#v", got)
	}
	if _, ok := got[0].Source.(firefox.Source); !ok {
		t.Fatalf("source = %T, want firefox.Source", got[0].Source)
	}

	got, err = resolveBookmarkStores("chrome", "/tmp/Default/Bookmarks")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got[0].Source.(chromium.Source); !ok {
		t.Fatalf("source = %T, want chromium.Source", got[0].Source)
	}

	got, err = resolveBookmarkStores("", "/tmp/Ladybird/Bookmarks.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got[0].Source.(ladybird.Source); !ok {
		t.Fatalf("source = %T, want ladybird.Source", got[0].Source)
	}
}

func TestResolveBookmarkStoresRejectsUnknown(t *testing.T) {
	if _, err := resolveBookmarkStores("safari", ""); err == nil {
		t.Fatal("expected safari to be rejected")
	}
	if _, err := resolveBookmarkStores("", "/tmp/History"); err == nil {
		t.Fatal("expected a history path to be rejected")
	}
}
