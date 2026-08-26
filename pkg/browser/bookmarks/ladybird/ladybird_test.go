// SPDX-License-Identifier: AGPL-3.0-or-later

package ladybird

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/asciimoo/hister/pkg/browser/bookmarks"
)

func TestLadybirdBookmarkSourceListURLs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Bookmarks.json")
	raw := `{
	  "version": 2,
	  "items": [
	    {"type": "bookmark", "url": "https://ladybird.org/", "title": "Ladybird"},
	    {"type": "folder", "title": "dev", "children": [
	      {"type": "bookmark", "url": "https://github.com/LadybirdBrowser/ladybird", "title": "GitHub"}
	    ]},
	    {"type": "bookmark", "url": "about:blank", "title": "blank"}
	  ]
	}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Source{}.ListURLs(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"https://github.com/LadybirdBrowser/ladybird", "https://ladybird.org/"}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("ladybird ListURLs = %#v, want %#v", got, want)
	}
}

func TestLadybirdDetectBothRootsAndProfiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths := []string{
		filepath.Join(home, ".config", "Ladybird", "Bookmarks.json"),
		filepath.Join(home, ".config", "Ladybird", "profile-a", "Bookmarks.json"),
		filepath.Join(home, ".local", "share", "Ladybird", "Bookmarks.json"),
		filepath.Join(home, ".local", "share", "Ladybird", "profile-b", "Bookmarks.json"),
	}
	for _, path := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(`{}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	find := func(table, prefix string) []bookmarks.Profile {
		return nil
	}
	got := Source{}.Detect("", find)
	gotPaths := make([]string, 0, len(got))
	for _, store := range got {
		gotPaths = append(gotPaths, store.Path)
	}
	slices.Sort(gotPaths)
	want := append([]string(nil), paths...)
	slices.Sort(want)
	if !slices.Equal(gotPaths, want) {
		t.Fatalf("Detect paths = %#v, want %#v", gotPaths, want)
	}
}
