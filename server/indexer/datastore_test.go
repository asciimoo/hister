// SPDX-License-Identifier: AGPL-3.0-or-later

package indexer

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/asciimoo/nogon/server/document"
	"github.com/asciimoo/nogon/server/testutil"

	"github.com/blevesearch/bleve/v2"
)

const (
	pendingHTML    = "<p>Content waiting for an index commit</p>"
	pendingFavicon = "data:image/png;base64,cGVuZGluZw=="
)

func pendingDocument(url string) *document.Document {
	return &document.Document{
		URL:       url,
		Text:      "Content waiting for an index commit",
		HTML:      pendingHTML,
		Favicon:   pendingFavicon,
		Processed: true,
	}
}

func assertStoredBlobs(t *testing.T, idx *Indexer, id string) {
	t.Helper()
	doc := idx.GetByDocID(id)
	if doc == nil {
		t.Fatal("committed document is missing")
	}
	if doc.HTML != pendingHTML {
		t.Errorf("stored HTML = %q, want %q", doc.HTML, pendingHTML)
	}
	icon, err := idx.ReadFavicon(doc.FaviconKey)
	if err != nil || string(icon) != pendingFavicon {
		t.Errorf("stored favicon = %q, %v, want %q", icon, err, pendingFavicon)
	}
}

func TestCleanupPreservesStagedDocumentBlobs(t *testing.T) {
	idx := newTestIndexer(t, testutil.Config(t))
	defer idx.Close()
	batch := idx.NewMultiBatch()
	doc := pendingDocument("https://example.com/staged")
	if err := batch.Add(doc); err != nil {
		t.Fatal(err)
	}
	if _, _, err := idx.CleanupDataFiles(); err != nil {
		t.Fatal(err)
	}
	if err := batch.Save(); err != nil {
		t.Fatal(err)
	}
	assertStoredBlobs(t, idx, doc.ID())
}

func TestDeletePreservesSharedStagedBlobs(t *testing.T) {
	for _, referencesOnly := range []bool{false, true} {
		name := "inline content"
		if referencesOnly {
			name = "existing storage references"
		}
		t.Run(name, func(t *testing.T) {
			idx := newTestIndexer(t, testutil.Config(t))
			defer idx.Close()
			original := pendingDocument("https://example.com/original")
			if err := idx.Add(original); err != nil {
				t.Fatal(err)
			}
			batch := idx.NewMultiBatch()
			doc := pendingDocument("https://example.com/staged")
			if referencesOnly {
				doc.HTML, doc.Favicon = "", ""
				doc.HTMLKey, doc.FaviconKey = original.HTMLKey, original.FaviconKey
			}
			if err := batch.Add(doc); err != nil {
				t.Fatal(err)
			}
			if err := idx.Delete(original.ID()); err != nil {
				t.Fatal(err)
			}
			if err := batch.Save(); err != nil {
				t.Fatal(err)
			}
			assertStoredBlobs(t, idx, doc.ID())
		})
	}
}

// Run cleanup after blob preparation, before the document becomes searchable.
type cleanupBeforeIndex struct {
	bleveIndex
	cleanup func()
}

func (i cleanupBeforeIndex) Index(id string, data any) error {
	i.cleanup()
	return i.bleveIndex.Index(id, data)
}

func TestCleanupPreservesBlobsDuringDocumentCommit(t *testing.T) {
	for _, operation := range []string{"add", "save"} {
		t.Run(operation, func(t *testing.T) {
			idx := newTestIndexer(t, testutil.Config(t))
			defer idx.Close()
			if err := idx.waitForMaintenance(); err != nil {
				t.Fatal(err)
			}
			idx.indexesMu.Lock()
			for name, index := range idx.indexers {
				idx.indexers[name] = cleanupBeforeIndex{bleveIndex: index, cleanup: func() {
					if _, _, err := idx.CleanupDataFiles(); err != nil {
						t.Fatal(err)
					}
				}}
			}
			idx.indexesMu.Unlock()
			doc := pendingDocument("https://example.com/commit")
			var err error
			if operation == "add" {
				err = idx.Add(doc)
			} else {
				err = idx.Save(doc)
			}
			if err != nil {
				t.Fatal(err)
			}
			assertStoredBlobs(t, idx, doc.ID())
		})
	}
}

func TestDiscardReleasesOnlyItsPendingBlobs(t *testing.T) {
	idx := newTestIndexer(t, testutil.Config(t))
	defer idx.Close()
	first, second := idx.NewMultiBatch(), idx.NewMultiBatch()
	defer first.Discard()
	defer second.Discard()
	if err := first.Add(pendingDocument("https://example.com/first")); err != nil {
		t.Fatal(err)
	}
	doc := pendingDocument("https://example.com/second")
	if err := second.Add(doc); err != nil {
		t.Fatal(err)
	}
	first.Discard()
	first.Discard()
	if _, _, err := idx.CleanupDataFiles(); err != nil {
		t.Fatal(err)
	}
	if err := second.Save(); err != nil {
		t.Fatal(err)
	}
	assertStoredBlobs(t, idx, doc.ID())
	if err := idx.Delete(doc.ID()); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []dataReference{{htmlSubdir, doc.HTMLKey}, {faviconSubdir, doc.FaviconKey}} {
		if _, err := idx.data.read(ref.subdir, ref.key); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("deleted document's %s blob remains: %v", ref.subdir, err)
		}
	}
}

type failingBlobCommitIndex struct {
	bleveIndex
	fail func() error
}

func (i failingBlobCommitIndex) Index(id string, data any) error {
	if err := i.fail(); err != nil {
		return err
	}
	return i.bleveIndex.Index(id, data)
}

func (i failingBlobCommitIndex) Batch(batch *bleve.Batch) error {
	if err := i.fail(); err != nil {
		return err
	}
	return i.bleveIndex.Batch(batch)
}

func TestFailedDocumentCommitReleasesPendingBlobs(t *testing.T) {
	for _, operation := range []string{"add", "save", "batch"} {
		t.Run(operation, func(t *testing.T) {
			idx := newTestIndexer(t, testutil.Config(t))
			defer idx.Close()
			if err := idx.waitForMaintenance(); err != nil {
				t.Fatal(err)
			}
			failure := errors.New("index commit failed")
			idx.indexesMu.Lock()
			for name, index := range idx.indexers {
				idx.indexers[name] = failingBlobCommitIndex{bleveIndex: index, fail: func() error { return failure }}
			}
			idx.indexesMu.Unlock()
			doc := pendingDocument("https://example.com/failed")
			var err error
			switch operation {
			case "add":
				err = idx.Add(doc)
			case "save":
				err = idx.Save(doc)
			case "batch":
				batch := idx.NewMultiBatch()
				if err := batch.Add(doc); err != nil {
					t.Fatal(err)
				}
				err = batch.Save()
			}
			if !errors.Is(err, failure) {
				t.Fatalf("commit error = %v, want %v", err, failure)
			}
			htmlRemoved, faviconRemoved, err := idx.CleanupDataFiles()
			if err != nil || htmlRemoved != 1 || faviconRemoved != 1 {
				t.Fatalf("cleanup removed HTML=%d favicon=%d, error=%v; want one of each", htmlRemoved, faviconRemoved, err)
			}
		})
	}
}

func TestFailedBlobPreparationReleasesPendingBlobs(t *testing.T) {
	idx := newTestIndexer(t, testutil.Config(t))
	defer idx.Close()
	// Let HTML storage succeed, then fail favicon directory creation.
	if err := os.MkdirAll(idx.data.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(idx.data.dir, faviconSubdir), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := idx.Add(pendingDocument("https://example.com/failed")); err == nil {
		t.Fatal("document preparation unexpectedly succeeded")
	}
	htmlRemoved, _, err := idx.CleanupDataFiles()
	if err != nil || htmlRemoved != 1 {
		t.Fatalf("cleanup removed HTML=%d, error=%v; want one uncommitted blob", htmlRemoved, err)
	}
}

func TestPartialBatchCommitPreservesCommittedBlobs(t *testing.T) {
	idx := newTestIndexer(t, testutil.Config(t))
	defer idx.Close()
	if err := idx.waitForMaintenance(); err != nil {
		t.Fatal(err)
	}
	idx.getOrCreate("en")
	failure := errors.New("second index commit failed")
	commits := 0
	idx.indexesMu.Lock()
	for name, index := range idx.indexers {
		idx.indexers[name] = failingBlobCommitIndex{bleveIndex: index, fail: func() error {
			commits++
			if commits == 2 {
				return failure
			}
			return nil
		}}
	}
	idx.indexesMu.Unlock()
	batch := idx.NewMultiBatch()
	first := pendingDocument("https://example.com/first")
	second := pendingDocument("https://example.com/second")
	second.Language = "en"
	second.HTML = "<p>A different document</p>"
	second.Favicon = "data:image/png;base64,c2Vjb25k"
	for _, doc := range []*document.Document{first, second} {
		if err := batch.Add(doc); err != nil {
			t.Fatal(err)
		}
	}
	if err := batch.Save(); !errors.Is(err, failure) {
		t.Fatalf("batch error = %v, want %v", err, failure)
	}
	htmlRemoved, faviconRemoved, err := idx.CleanupDataFiles()
	if err != nil || htmlRemoved != 1 || faviconRemoved != 1 {
		t.Fatalf("cleanup removed HTML=%d favicon=%d, error=%v; want only uncommitted blobs", htmlRemoved, faviconRemoved, err)
	}
	committed := 0
	for _, doc := range []*document.Document{first, second} {
		if stored := idx.GetByDocID(doc.ID()); stored != nil {
			committed++
			if stored.HTML == "" || stored.Favicon == "" {
				t.Errorf("committed document lost its blobs: %+v", stored)
			}
		}
	}
	if committed != 1 {
		t.Fatalf("committed documents = %d, want 1", committed)
	}
}
