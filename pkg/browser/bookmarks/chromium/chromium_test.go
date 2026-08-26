// SPDX-License-Identifier: AGPL-3.0-or-later

package chromium

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/asciimoo/hister/pkg/browser/bookmarks"
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

func TestChromiumDetectMultipleProfiles(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	dir3 := t.TempDir()
	hist1 := filepath.Join(dir1, "History")
	hist2 := filepath.Join(dir2, "History")
	hist3 := filepath.Join(dir3, "History")
	bm1 := filepath.Join(dir1, "Bookmarks")
	bm2 := filepath.Join(dir2, "Bookmarks")

	writeFile(t, hist1, "")
	writeFile(t, hist2, "")
	writeFile(t, hist3, "")
	writeFile(t, bm1, `{"roots":{"bookmark_bar":{"type":"url","name":"one","url":"https://example.com/one"}}}`)
	writeFile(t, bm2, `{"roots":{"bookmark_bar":{"type":"url","name":"two","url":"https://example.com/two"}}}`)

	find := func(table, prefix string) []bookmarks.Profile {
		return []bookmarks.Profile{
			{Name: "chrome", Paths: []string{hist1}},
			{Name: "chromium", Paths: []string{hist2}},
			{Name: "brave", Paths: []string{hist3}},
		}
	}
	got := Source{}.Detect("", find)
	if len(got) != 2 {
		t.Fatalf("Detect returned %d stores, want 2: %#v", len(got), got)
	}
	want := map[string]string{
		"chrome":   bm1,
		"chromium": bm2,
	}
	for _, store := range got {
		path, ok := want[store.Browser]
		if !ok {
			t.Fatalf("unexpected browser %q", store.Browser)
		}
		if store.Path != path {
			t.Fatalf("%s path = %q, want %q", store.Browser, store.Path, path)
		}
		delete(want, store.Browser)
	}
	if len(want) != 0 {
		t.Fatalf("missing stores: %#v", want)
	}
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
