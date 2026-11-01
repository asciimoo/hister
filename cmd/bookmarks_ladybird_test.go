// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
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
	got, err := ladybirdBookmarkSource{}.listURLs(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"https://github.com/LadybirdBrowser/ladybird", "https://ladybird.org/"}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("ladybird listURLs = %#v, want %#v", got, want)
	}
}

func TestLadybirdDetectBothRootsAndProfiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	want := []string{
		filepath.Join(home, ".config", "Ladybird", "Bookmarks.json"),
		filepath.Join(home, ".config", "Ladybird", "Profiles", "default", "Bookmarks.json"),
		filepath.Join(home, ".local", "share", "Ladybird", "Bookmarks.json"),
		filepath.Join(home, "Library", "Application Support", "Ladybird", "Profiles", "work", "Bookmarks.json"),
		filepath.Join(home, ".var", "app", "org.ladybird.Ladybird", "config", "Ladybird", "Profiles", "flat", "Bookmarks.json"),
		filepath.Join(home, "custom", "Bookmarks.json"),
	}
	decoy := filepath.Join(home, ".config", "Ladybird", "not-a-profile", "Bookmarks.json")
	for _, path := range append(append([]string{}, want...), decoy) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(`{}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	profiles := []browserDB{
		{name: "Ladybird", table_name: "History", paths: []string{
			filepath.Join(home, "custom", "History.db"),
			filepath.Join(home, ".config", "Ladybird", "History.db"),
		}},
		{name: "Chrome", table_name: "urls", paths: []string{
			filepath.Join(filepath.Dir(decoy), "History"),
		}},
	}
	got := ladybirdBookmarkSource{}.detect(profiles)
	gotPaths := make([]string, 0, len(got))
	for _, store := range got {
		gotPaths = append(gotPaths, store.path)
	}
	slices.Sort(gotPaths)
	slices.Sort(want)
	if !slices.Equal(gotPaths, want) {
		t.Fatalf("detect paths = %#v, want %#v", gotPaths, want)
	}
	if slices.Contains(gotPaths, decoy) {
		t.Fatalf("detect included decoy path %q", decoy)
	}
}
