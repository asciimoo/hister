package indexer

import (
	"os"
	"sort"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
)

// DomainStat is the storage a single domain accounts for.
type DomainStat struct {
	Domain string `json:"domain"`
	Pages  uint64 `json:"pages"`
	// TextBytes is the length of the indexed text. It is an ESTIMATE of the domain's share of the
	// search index, not a measurement of it: Bleve does not report per-document storage, and the
	// index also holds term vectors and postings that no document owns individually. It is
	// nevertheless the number worth showing, because indexed text is the thing that grows.
	TextBytes uint64 `json:"text_bytes"`
	// HTMLBytes and FaviconBytes are exact, being the compressed sizes of files on disk.
	//
	// Only blobs referenced by this domain ALONE are counted. Stored data is content addressed, so
	// two pages with byte-identical HTML share one file, and charging both domains for it would
	// promise storage that pruning either one would not release. This is what you actually get
	// back. Measured on a real index the difference is about 1% for HTML and much larger for
	// favicons, where one icon commonly serves a whole site.
	HTMLBytes    uint64 `json:"html_bytes"`
	FaviconBytes uint64 `json:"favicon_bytes"`
	// SharedBytes is stored data this domain references but does not own alone, and so is not
	// counted in the figures above.
	//
	// Reported rather than silently omitted. Without it a domain whose every page is byte
	// identical to another domain's shows zero stored bytes, which reads as a fault: example.com
	// and example.org serve the same document, so one file serves both and deleting either
	// releases nothing. The number explains its own absence.
	SharedBytes uint64 `json:"shared_bytes"`
	// TotalBytes is the sum of the text, HTML and favicon bytes this domain owns outright. Shared
	// bytes are excluded, because the total answers "what would deleting this release".
	TotalBytes uint64 `json:"total_bytes"`
}

// DomainStats reports storage per domain, largest first.
//
// The whole index is walked, because the sizes are per document and there is nothing to aggregate
// against. That is one pass over the stored fields and one os.Stat per distinct blob; the blobs
// themselves are never read, which is what keeps a report about 89 MB of HTML from costing 89 MB
// of reads.
func (i *Indexer) DomainStats() ([]DomainStat, error) {
	return i.domainStats(query.NewMatchAllQuery())
}

// DomainStatsByUser reports storage per domain for one user's documents.
//
// It exists for the same reason TotalByUser does: with user handling enabled a report over every
// document would show one user the list of sites another has indexed, which a storage breakdown
// makes unusually legible.
func (i *Indexer) DomainStatsByUser(userID uint) ([]DomainStat, error) {
	uid := float64(userID)
	q := bleve.NewNumericRangeInclusiveQuery(&uid, &uid, new(true), new(true))
	q.SetField("user_id")
	return i.domainStats(q)
}

func (i *Indexer) domainStats(q query.Query) ([]DomainStat, error) {
	type acc struct {
		pages     uint64
		textBytes uint64
	}
	domains := make(map[string]*acc)
	// Which domains reference each blob. A set rather than a count, because the question is
	// whether exactly one domain owns it, not how many documents do.
	htmlOwners := make(map[string]map[string]struct{})
	faviconOwners := make(map[string]map[string]struct{})

	note := func(owners map[string]map[string]struct{}, key, domain string) {
		if key == "" {
			return
		}
		set, ok := owners[key]
		if !ok {
			set = make(map[string]struct{})
			owners[key] = set
		}
		set[domain] = struct{}{}
	}

	req := bleve.NewSearchRequest(q)
	// Only what the sums need. Notably not "html": it is blank in the index anyway, since
	// prepareForStorage moves it to the data store, and its size comes from the file.
	req.Fields = []string{"domain", "text", "html_key", "favicon_key"}
	req.Size = 200
	req.SortBy([]string{"_id"})

	var sortKey []string
	for {
		if len(sortKey) > 0 {
			req.SetSearchAfter(sortKey)
		}
		res, err := i.searchIndexes(req)
		if err != nil {
			return nil, err
		}
		n := len(res.Hits)
		if n < 1 {
			break
		}
		for _, h := range res.Hits {
			domain, _ := h.Fields["domain"].(string)
			if domain == "" {
				// A document with no domain is still storage, and hiding it would make the
				// columns fail to add up to what is on disk.
				domain = "(unknown)"
			}
			a, ok := domains[domain]
			if !ok {
				a = &acc{}
				domains[domain] = a
			}
			a.pages++
			if text, ok := h.Fields["text"].(string); ok {
				a.textBytes += uint64(len(text))
			}
			htmlKey, _ := h.Fields["html_key"].(string)
			faviconKey, _ := h.Fields["favicon_key"].(string)
			note(htmlOwners, htmlKey, domain)
			note(faviconOwners, faviconKey, domain)
		}
		sortKey = res.Hits[n-1].Sort
	}

	stats := make(map[string]*DomainStat, len(domains))
	for domain, a := range domains {
		stats[domain] = &DomainStat{
			Domain:    domain,
			Pages:     a.pages,
			TextBytes: a.textBytes,
		}
	}

	i.addBlobBytes(htmlOwners, htmlSubdir, stats, func(s *DomainStat, n uint64) {
		s.HTMLBytes += n
	})
	i.addBlobBytes(faviconOwners, faviconSubdir, stats, func(s *DomainStat, n uint64) {
		s.FaviconBytes += n
	})

	out := make([]DomainStat, 0, len(stats))
	for _, s := range stats {
		s.TotalBytes = s.TextBytes + s.HTMLBytes + s.FaviconBytes
		out = append(out, *s)
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].TotalBytes != out[b].TotalBytes {
			return out[a].TotalBytes > out[b].TotalBytes
		}
		// A stable order for equal sizes, so repeated calls agree and a test can rely on it.
		return out[a].Domain < out[b].Domain
	})
	return out, nil
}

// addBlobBytes charges each domain for the blobs only it references, and records the rest as
// shared so that nothing a domain references goes unreported.
func (i *Indexer) addBlobBytes(
	owners map[string]map[string]struct{},
	subdir string,
	stats map[string]*DomainStat,
	add func(*DomainStat, uint64),
) {
	for key, set := range owners {
		info, err := os.Stat(dataFilePath(i.data.dir, subdir, key))
		if err != nil {
			// A missing blob is not worth failing a report over: the index can outlive its data
			// file after an interrupted cleanup, and a size of zero is the honest answer.
			continue
		}
		size := uint64(info.Size())
		if len(set) == 1 {
			for domain := range set {
				if stat, ok := stats[domain]; ok {
					add(stat, size)
				}
			}
			continue
		}
		// Shared. Every referencing domain is told about it, and none is charged: deleting any one
		// of them leaves the file in place for the others.
		for domain := range set {
			if stat, ok := stats[domain]; ok {
				stat.SharedBytes += size
			}
		}
	}
}
