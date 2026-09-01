// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/model"
)

func initTestDB(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	cfg := config.CreateDefaultConfig()
	// Absolute path bypasses FullPath prefix logic.
	cfg.Server.Database = dbPath

	if err := model.Init(cfg); err != nil {
		t.Fatalf("model.Init: %v", err)
	}
}

// TestSQLiteQueueNoConcurrentDoubleFetch verifies that multiple workers sharing
// the same sqlite queue never claim the same URL row twice.
func TestSQLiteQueueNoConcurrentDoubleFetch(t *testing.T) {
	initTestDB(t)

	jobID := "test-concurrent-job"
	if err := model.CreateCrawlJob(jobID, "http://example.com/", "", "test"); err != nil {
		t.Fatalf("CreateCrawlJob: %v", err)
	}

	// Insert 20 URLs.
	const urlCount = 20
	urls := make([]string, urlCount)
	for i := 0; i < urlCount; i++ {
		urls[i] = "http://example.com/" + string(rune('a'+i))
	}
	if err := model.BulkInsertCrawlURLs(jobID, urls, 0); err != nil {
		t.Fatalf("BulkInsertCrawlURLs: %v", err)
	}

	q := newSQLiteQueue(jobID)

	// 5 workers compete to claim all 20 URLs.
	const workers = 5
	var claimed []string
	var claimedMu sync.Mutex
	var wg sync.WaitGroup
	var totalClaimed atomic.Int64

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				item, ok, err := q.Pop(ctx)
				if err != nil || !ok {
					return
				}
				claimedMu.Lock()
				claimed = append(claimed, item.rawURL)
				claimedMu.Unlock()
				totalClaimed.Add(1)
				if err := q.Complete(ctx, item, completion{finalURL: item.rawURL}); err != nil {
					t.Errorf("Complete failed: %v", err)
				}
			}
		}()
	}

	wg.Wait()

	if got := totalClaimed.Load(); got != urlCount {
		t.Errorf("claimed %d URLs, want %d", got, urlCount)
	}

	// Check for duplicates.
	seen := make(map[string]int)
	for _, u := range claimed {
		seen[u]++
	}
	for u, count := range seen {
		if count > 1 {
			t.Errorf("URL %q claimed %d times (double-fetch)", u, count)
		}
	}
}
