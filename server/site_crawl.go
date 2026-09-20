package server

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/crawler"
	"github.com/asciimoo/hister/server/indexer"
)

// Keep just one crawl per server: no new database schema or worker pool.
// Closing a popup does not cancel it; stopping the server does.
type siteCrawls struct {
	mu     sync.Mutex
	wg     sync.WaitGroup
	closed bool
	job    *siteCrawl
}

type siteCrawl struct {
	ID       string `json:"id"`
	URL      string `json:"url"`
	State    string `json:"state"`
	MaxPages int    `json:"max_pages"`
	Indexed  int    `json:"indexed"`
	Skipped  int    `json:"skipped"`
	Error    string `json:"error,omitempty"`
	userID   uint
	cancel   context.CancelFunc
}

type crawlHandler struct {
	http.Handler
	crawls *siteCrawls
}

func (s *siteCrawls) close() {
	s.mu.Lock()
	s.closed = true
	if s.job != nil {
		s.job.cancel()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

func serveSiteCrawl(c *webContext) {
	s := c.crawls
	s.mu.Lock()
	var status *siteCrawl
	if s.job != nil && s.job.userID == c.UserID {
		snapshot := *s.job
		status = &snapshot
	}
	s.mu.Unlock()
	c.Response.Header().Set("Content-Type", "application/json")
	c.Response.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(c.Response).Encode(status)
}

func startSiteCrawl(c *webContext) {
	var input struct {
		URL      string `json:"url"`
		MaxPages int    `json:"max_pages"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(c.Response, c.Request.Body, 4096)).Decode(&input); err != nil {
		http.Error(c.Response, "invalid crawl request", http.StatusBadRequest)
		return
	}
	u, err := url.Parse(input.URL)
	if err != nil {
		http.Error(c.Response, "invalid site URL", http.StatusBadRequest)
		return
	}
	// Start at the home page, regardless of which tab launched the crawl.
	u.Path, u.RawPath, u.RawQuery, u.Fragment = "/", "", "", ""
	if input.MaxPages == 0 {
		input.MaxPages = 100
	}
	// Copy skip patterns, so editing rules cannot mutate an active crawl.
	rules := &config.Rule{}
	if r := c.effectiveRules(); r != nil && r.Skip != nil {
		rules.ReStrs = append([]string(nil), r.Skip.ReStrs...)
	}
	if err := rules.Compile(); err != nil {
		http.Error(c.Response, "invalid skip rules", http.StatusInternalServerError)
		return
	}
	cr, validator, err := crawler.NewSite(u.String(), input.MaxPages, func(rawURL string) (bool, error) { return rules.Match(rawURL), nil })
	if err != nil {
		http.Error(c.Response, err.Error(), http.StatusBadRequest)
		return
	}
	s := c.crawls
	s.mu.Lock()
	if s.closed || (s.job != nil && s.job.State == "running") {
		s.mu.Unlock()
		_ = cr.Close()
		http.Error(c.Response, "a site crawl is already running or the server is stopping", http.StatusConflict)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	job := &siteCrawl{ID: rand.Text(), URL: u.String(), State: "running", MaxPages: input.MaxPages, userID: c.UserID, cancel: cancel}
	s.job = job
	s.wg.Add(1)
	s.mu.Unlock()
	go s.run(ctx, job, cr, validator, c.Indexer, rules)
	c.Response.WriteHeader(http.StatusAccepted)
}

func stopSiteCrawl(c *webContext) {
	s := c.crawls
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.job == nil || s.job.userID != c.UserID || s.job.ID != c.Request.URL.Query().Get("id") {
		http.Error(c.Response, "crawl not found", http.StatusNotFound)
		return
	}
	s.job.cancel()
	c.Response.WriteHeader(http.StatusNoContent)
}

func (s *siteCrawls) run(ctx context.Context, job *siteCrawl, cr crawler.Crawler, validator *crawler.Validator, idx *indexer.Indexer, skip *config.Rule) {
	defer s.wg.Done()
	defer job.cancel()
	defer func() { _ = cr.Close() }()
	docs, err := cr.Crawl(ctx, job.URL, validator)
	if err == nil {
		for doc := range docs {
			// Redirect targets must obey skip rules too. Existing pages still supply
			// links, but are not reindexed or transferred between users.
			skipped := skip.Match(doc.URL) || idx.GetByURLAndUser(doc.URL, job.userID) != nil
			var addErr error
			if !skipped {
				doc.UserID = job.userID
				addErr = idx.AddHTMLContext(ctx, doc)
			}
			s.mu.Lock()
			switch {
			case skipped:
				job.Skipped++
			case addErr != nil:
				job.Error = addErr.Error()
			default:
				job.Indexed++
			}
			s.mu.Unlock()
		}
		if reporter, ok := cr.(crawler.ErrorReporter); ok {
			err = reporter.Err()
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	job.State = "finished"
	if err != nil {
		job.Error = err.Error()
	}
	if ctx.Err() != nil {
		job.State = "stopped"
		if ctx.Err() == context.DeadlineExceeded {
			job.Error = "crawl reached the 30-minute time limit"
		}
	}
}
