// SPDX-License-Identifier: AGPL-3.0-or-later

package ladybird

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
