// SPDX-License-Identifier: AGPL-3.0-or-later

package chromium

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestChromiumBookmarkSourceListURLs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Bookmarks")
	raw := `{
	  "roots": {
	    "bookmark_bar": {
	      "type": "folder",
	      "children": [
	        {"type": "url", "name": "Go Blog", "url": "https://go.dev/blog/"},
	        {"type": "url", "name": "bookmarklet", "url": "javascript:void(0)"},
	        {"type": "folder", "name": "more", "children": [
	          {"type": "url", "name": "hister", "url": "https://github.com/asciimoo/hister"}
	        ]}
	      ]
	    },
	    "other": {
	      "type": "folder",
	      "children": [
	        {"type": "url", "name": "dup", "url": "https://go.dev/blog/"}
	      ]
	    }
	  }
	}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Source{}.ListURLs(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"https://github.com/asciimoo/hister", "https://go.dev/blog/"}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("chromium ListURLs = %#v, want %#v", got, want)
	}
}
