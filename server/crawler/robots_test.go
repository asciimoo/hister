// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRobotsCacheTTLExpiry(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprintln(w, "User-agent: *\nAllow: /")
	}))
	defer srv.Close()

	cache, err := NewRobotsCacheWithProxyAndTTL("TestBot", "", 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	cache.client = srv.Client()

	ctx := context.Background()
	cache.Allowed(ctx, srv.URL+"/page")
	cache.Allowed(ctx, srv.URL+"/page") // should hit cache

	if calls != 1 {
		t.Errorf("expected 1 fetch before TTL, got %d", calls)
	}

	time.Sleep(100 * time.Millisecond)
	cache.Allowed(ctx, srv.URL+"/page") // TTL expired, should re-fetch

	if calls != 2 {
		t.Errorf("expected 2 fetches after TTL, got %d", calls)
	}
}

func TestRobotsCache5xxDenyAll(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	cache, err := NewRobotsCacheWithProxy("TestBot", "")
	if err != nil {
		t.Fatal(err)
	}
	cache.client = srv.Client()

	allowed := cache.Allowed(context.Background(), srv.URL+"/page")
	if allowed {
		t.Error("expected deny-all on 5xx robots.txt, got allowed=true")
	}
}

func TestRobotsCache404AllowAll(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	cache, err := NewRobotsCacheWithProxy("TestBot", "")
	if err != nil {
		t.Fatal(err)
	}
	cache.client = srv.Client()

	allowed := cache.Allowed(context.Background(), srv.URL+"/page")
	if !allowed {
		t.Error("expected allow-all on 404 robots.txt, got allowed=false")
	}
}

func TestRobotsCacheCrawlDelay(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "User-agent: *\nCrawl-delay: 5\nAllow: /")
	}))
	defer srv.Close()

	cache, err := NewRobotsCacheWithProxy("TestBot", "")
	if err != nil {
		t.Fatal(err)
	}
	cache.client = srv.Client()

	ctx := context.Background()
	cache.Allowed(ctx, srv.URL+"/page") // prime the cache

	d := cache.CrawlDelay(srv.URL + "/page")
	if d != 5*time.Second {
		t.Errorf("CrawlDelay = %v, want 5s", d)
	}
}

func TestRobotsCacheLRUEviction(t *testing.T) {
	call := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call++
		fmt.Fprintln(w, "User-agent: *\nAllow: /")
	}))
	defer srv.Close()

	cache, err := NewRobotsCacheWithProxy("TestBot", "")
	if err != nil {
		t.Fatal(err)
	}
	cache.client = srv.Client()

	// Fill the cache beyond capacity by faking many different hosts.
	// We can't easily create 1001 HTTP servers, so instead we directly
	// fill the internal LRU structures to verify eviction.
	ctx := context.Background()

	for i := 0; i < robotsCacheLRUCapacity+5; i++ {
		key := fmt.Sprintf("http://host%d.example.com", i)
		entry := &robotsEntry{fetchedAt: time.Now(), ttl: time.Hour, allowAll: true}
		cache.store(key, entry)
	}

	cache.mu.Lock()
	size := cache.lru.Len()
	cache.mu.Unlock()

	if size > robotsCacheLRUCapacity {
		t.Errorf("LRU size = %d, want <= %d", size, robotsCacheLRUCapacity)
	}

	_ = ctx
}
