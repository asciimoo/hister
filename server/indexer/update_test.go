// SPDX-License-Identifier: AGPL-3.0-or-later

package indexer

import (
	"errors"
	"testing"

	"github.com/asciimoo/nogon/server/document"
	"github.com/asciimoo/nogon/server/testutil"
	servertypes "github.com/asciimoo/nogon/server/types"
)

func addUpdateTestDocument(t *testing.T, idx *Indexer, d *document.Document) {
	t.Helper()
	d.Processed = true
	if err := idx.Add(d); err != nil {
		t.Fatalf("add document %q: %v", d.URL, err)
	}
}

func TestUpdateByQueryChangesAttributesAfterDryRun(t *testing.T) {
	idx := newTestIndexer(t, testutil.Config(t))
	defer idx.Close()
	url := "https://example.com/article"
	addUpdateTestDocument(t, idx, &document.Document{
		URL:      url,
		Domain:   "example.com",
		Title:    "Original title",
		Text:     "Original body",
		Language: document.UnknownLanguage,
		Label:    "old",
	})
	addUpdateTestDocument(t, idx, &document.Document{
		URL:      "https://other.example/article",
		Domain:   "other.example",
		Title:    "Other title",
		Text:     "Other body",
		Language: document.UnknownLanguage,
	})

	changes := servertypes.DocumentChanges{
		Label:    new("research"),
		Title:    new("Corrected title"),
		Language: new("EN"),
	}
	dryResult, err := idx.UpdateByQuery("domain:example.com", nil, changes, true)
	if err != nil {
		t.Fatalf("dry update: %v", err)
	}
	if dryResult.Matched != 1 || dryResult.Updated != 1 || dryResult.Unchanged != 0 || dryResult.Conflicts != 0 {
		t.Fatalf("dry result = %+v", dryResult)
	}
	if stored := idx.GetByDocID(url); stored.Label != "old" || stored.Title != "Original title" {
		t.Fatalf("dry update changed document: %+v", stored)
	}

	result, err := idx.UpdateByQuery("domain:example.com", nil, changes, false)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if result.Matched != 1 || result.Updated != 1 || result.Unchanged != 0 || result.Conflicts != 0 {
		t.Fatalf("result = %+v", result)
	}
	stored := idx.GetByDocID(url)
	if stored == nil {
		t.Fatal("updated document not found")
	}
	if stored.Label != "research" || stored.Title != "Corrected title" || stored.Language != "en" {
		t.Fatalf("updated document = %+v", stored)
	}
	if stored.Text != "Original body" {
		t.Fatalf("updated text = %q, want original body", stored.Text)
	}

	unchanged, err := idx.UpdateByQuery("domain:example.com", nil, changes, false)
	if err != nil {
		t.Fatalf("repeat update: %v", err)
	}
	if unchanged.Matched != 1 || unchanged.Updated != 0 || unchanged.Unchanged != 1 {
		t.Fatalf("repeat result = %+v", unchanged)
	}
}

func TestUpdateByQueryMovesOwnershipAndSkipsConflicts(t *testing.T) {
	idx := newTestIndexer(t, testutil.Config(t))
	defer idx.Close()
	const targetUserID = uint(7)
	movableURL := "https://example.com/movable"
	conflictURL := "https://example.com/conflict"
	addUpdateTestDocument(t, idx, &document.Document{
		URL:     movableURL,
		Title:   "Movable",
		Text:    "body",
		HTML:    "<p>stored preview</p>",
		Favicon: "stored favicon",
	})
	original := idx.GetByDocID(movableURL)
	if original == nil || original.HTMLKey == "" || original.FaviconKey == "" {
		t.Fatalf("original stored assets = %+v", original)
	}
	addUpdateTestDocument(t, idx, &document.Document{URL: conflictURL, Title: "Global", Text: "global body"})
	addUpdateTestDocument(t, idx, &document.Document{URL: conflictURL, Title: "Owned", Text: "owned body", UserID: targetUserID})

	result, err := idx.UpdateByQuery(
		"user_id:0",
		nil,
		servertypes.DocumentChanges{UserID: new(targetUserID)},
		false,
	)
	if err != nil {
		t.Fatalf("move ownership: %v", err)
	}
	if result.Matched != 2 || result.Updated != 1 || result.Conflicts != 1 {
		t.Fatalf("result = %+v", result)
	}
	if len(result.OwnershipChanges) != 1 || result.OwnershipChanges[0].URL != movableURL {
		t.Fatalf("ownership changes = %+v", result.OwnershipChanges)
	}
	if idx.GetByDocID(movableURL) != nil {
		t.Fatal("global source remains after ownership move")
	}
	moved := idx.GetByDocID(document.GetDocID(targetUserID, movableURL))
	if moved == nil || moved.Title != "Movable" || moved.Text != "body" {
		t.Fatalf("moved document = %+v", moved)
	}
	if moved.HTMLKey != original.HTMLKey || moved.FaviconKey != original.FaviconKey {
		t.Fatalf("moved asset keys = %q and %q, want %q and %q", moved.HTMLKey, moved.FaviconKey, original.HTMLKey, original.FaviconKey)
	}
	if html, err := idx.data.read(htmlSubdir, moved.HTMLKey); err != nil || string(html) != "<p>stored preview</p>" {
		t.Fatalf("moved HTML = %q, error = %v", html, err)
	}
	if favicon, err := idx.ReadFavicon(moved.FaviconKey); err != nil || string(favicon) != "stored favicon" {
		t.Fatalf("moved favicon = %q, error = %v", favicon, err)
	}
	if idx.GetByDocID(conflictURL) == nil || idx.GetByDocID(document.GetDocID(targetUserID, conflictURL)) == nil {
		t.Fatal("ownership conflict removed a source or destination document")
	}
}

func TestUpdateByQueryRestrictsSourceOwner(t *testing.T) {
	idx := newTestIndexer(t, testutil.Config(t))
	defer idx.Close()
	const sourceUserID = uint(3)
	globalURL := "https://example.com/global"
	ownedURL := "https://example.com/owned"
	addUpdateTestDocument(t, idx, &document.Document{URL: globalURL, Title: "Global", Text: "body"})
	addUpdateTestDocument(t, idx, &document.Document{URL: ownedURL, Title: "Owned", Text: "body", UserID: sourceUserID})

	result, err := idx.UpdateByQuery("*", new(sourceUserID), servertypes.DocumentChanges{Label: new("private")}, false)
	if err != nil {
		t.Fatalf("scoped update: %v", err)
	}
	if result.Matched != 1 || result.Updated != 1 {
		t.Fatalf("result = %+v", result)
	}
	if label := idx.GetByDocID(globalURL).Label; label != "" {
		t.Fatalf("global label = %q, want empty", label)
	}
	if label := idx.GetByDocID(document.GetDocID(sourceUserID, ownedURL)).Label; label != "private" {
		t.Fatalf("owned label = %q, want private", label)
	}
}

func TestUpdateByQueryValidatesChanges(t *testing.T) {
	idx := newTestIndexer(t, testutil.Config(t))
	defer idx.Close()
	if _, err := idx.UpdateByQuery("*", nil, servertypes.DocumentChanges{}, false); !errors.Is(err, ErrEmptyUpdate) {
		t.Fatalf("empty update error = %v, want %v", err, ErrEmptyUpdate)
	}
	if _, err := idx.UpdateByQuery("*", nil, servertypes.DocumentChanges{Language: new("../en")}, false); !errors.Is(err, ErrInvalidLanguage) {
		t.Fatalf("invalid language error = %v, want %v", err, ErrInvalidLanguage)
	}
	if _, err := idx.UpdateByQuery("*", nil, servertypes.DocumentChanges{Language: new("zz")}, false); !errors.Is(err, ErrInvalidLanguage) {
		t.Fatalf("unregistered language error = %v, want %v", err, ErrInvalidLanguage)
	}
	if analyzerForLanguage("en") != "en" || analyzerForLanguage("zh") != "cjk" {
		t.Fatal("registered language analyzer was rejected")
	}
}
