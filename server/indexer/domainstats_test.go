package indexer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/asciimoo/hister/server/document"
	"github.com/asciimoo/hister/server/testutil"
)

// addDoc indexes a document with Domain set explicitly. Domain is normally derived while a
// document is processed, and these tests mark documents Processed to keep the extractor and the
// network out of it — so the field has to be supplied, or every document lands in one bucket and
// the grouping under test is never exercised.
func addDoc(t *testing.T, i *Indexer, domain, url, text, html string) {
	t.Helper()
	d := &document.Document{
		URL: url, Domain: domain, Title: "t", Text: text, HTML: html, Processed: true,
	}
	if err := i.Add(d); err != nil {
		t.Fatalf("Add(%s): %v", url, err)
	}
}

func statsByDomain(stats []DomainStat) map[string]DomainStat {
	out := make(map[string]DomainStat, len(stats))
	for _, s := range stats {
		out[s.Domain] = s
	}
	return out
}

func TestDomainStatsGroupsAndCounts(t *testing.T) {
	i := newTestIndexer(t, testutil.Config(t))
	defer i.Close()
	addDoc(t, i, "a.example", "https://a.example/1", "aaaa", "<html>one</html>")
	addDoc(t, i, "a.example", "https://a.example/2", "bb", "<html>two</html>")
	addDoc(t, i, "b.example", "https://b.example/1", "cccccc", "<html>three</html>")

	stats, err := i.DomainStats()
	if err != nil {
		t.Fatalf("DomainStats(): %v", err)
	}
	by := statsByDomain(stats)

	if got := by["a.example"].Pages; got != 2 {
		t.Fatalf("a.example pages = %d, want 2", got)
	}
	if got := by["b.example"].Pages; got != 1 {
		t.Fatalf("b.example pages = %d, want 1", got)
	}
	if got := by["a.example"].TextBytes; got != 6 {
		t.Fatalf("a.example text bytes = %d, want 6 (4+2)", got)
	}
	// Largest first.
	if stats[0].TotalBytes < stats[len(stats)-1].TotalBytes {
		t.Fatalf("DomainStats() is not sorted by size descending: %+v", stats)
	}
}

func TestDomainStatsChargesBlobsOnlyToTheirSoleOwner(t *testing.T) {
	// Stored data is content addressed, so identical HTML is one file. Charging both domains for
	// it would promise storage that pruning either would not release.
	i := newTestIndexer(t, testutil.Config(t))
	defer i.Close()
	const shared = "<html>identical</html>"
	addDoc(t, i, "a.example", "https://a.example/1", "a", shared)
	addDoc(t, i, "b.example", "https://b.example/1", "b", shared)
	addDoc(t, i, "c.example", "https://c.example/1", "c", "<html>unique to c</html>")

	stats, err := i.DomainStats()
	if err != nil {
		t.Fatalf("DomainStats(): %v", err)
	}
	by := statsByDomain(stats)

	if by["a.example"].HTMLBytes != 0 {
		t.Fatalf("a.example was charged %d bytes for HTML it shares", by["a.example"].HTMLBytes)
	}
	if by["b.example"].HTMLBytes != 0 {
		t.Fatalf("b.example was charged %d bytes for HTML it shares", by["b.example"].HTMLBytes)
	}
	if by["c.example"].HTMLBytes == 0 {
		t.Fatal("c.example was not charged for HTML only it references")
	}
}

func TestDomainStatsHTMLBytesMatchTheFileOnDisk(t *testing.T) {
	// The figure must be the real compressed size, since that is what the disk holds.
	i := newTestIndexer(t, testutil.Config(t))
	defer i.Close()
	addDoc(t, i, "only.example", "https://only.example/1", "text", "<html>"+string(make([]byte, 4096))+"</html>")

	stats, err := i.DomainStats()
	if err != nil {
		t.Fatalf("DomainStats(): %v", err)
	}
	by := statsByDomain(stats)

	var onDisk uint64
	root := filepath.Join(i.data.dir, htmlSubdir)
	if err := filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() {
			onDisk += uint64(info.Size())
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if by["only.example"].HTMLBytes != onDisk {
		t.Fatalf("HTMLBytes = %d, want %d (the bytes actually on disk)",
			by["only.example"].HTMLBytes, onDisk)
	}
}

func TestDomainStatsTotalIsTheSumOfItsParts(t *testing.T) {
	i := newTestIndexer(t, testutil.Config(t))
	defer i.Close()
	addDoc(t, i, "sum.example", "https://sum.example/1", "some text", "<html>body</html>")

	stats, err := i.DomainStats()
	if err != nil {
		t.Fatalf("DomainStats(): %v", err)
	}
	for _, s := range stats {
		if s.TotalBytes != s.TextBytes+s.HTMLBytes+s.FaviconBytes {
			t.Fatalf("%s: total %d != %d + %d + %d",
				s.Domain, s.TotalBytes, s.TextBytes, s.HTMLBytes, s.FaviconBytes)
		}
	}
}
