package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/asciimoo/nogon/server/document"
	"github.com/asciimoo/nogon/server/model"
	"github.com/asciimoo/nogon/server/testutil"
	"github.com/asciimoo/nogon/server/types"
)

func TestPurgeVersionsPreviewsAndPreservesCurrentDocuments(t *testing.T) {
	cfg := testutil.InitModel(t)
	cfg.App.AccessToken = "secret"
	if err := cfg.UpdateBaseURL("http://127.0.0.1:4433"); err != nil {
		t.Fatal(err)
	}
	sessionStore = newSessionStore([]byte(strings.Repeat("x", 32)), cfg.BaseURL(""), sessionMaxAge)
	idx := newServerTestIndexer(t, cfg)
	handler := registerEndpoints(cfg, idx)
	const url = "https://example.com/versioned"
	cutoff := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, created := range []time.Time{cutoff.Add(-time.Second), cutoff, cutoff.Add(time.Second)} {
		if err := model.DB.Create(&model.DocumentVersion{URL: url, CreatedAt: created, TextDiff: "saved text"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := idx.Add(&document.Document{URL: url, Title: "Current", Text: "current text", Processed: true}); err != nil {
		t.Fatal(err)
	}
	for _, attempt := range []struct {
		before string
		dryRun bool
	}{
		{"2026-01-01T00:00:00Z", true},
		{"2026-01-01T01:00:00+01:00", true},
		{"2026-01-01T01:00:00+01:00", false},
	} {
		dryRun := attempt.dryRun
		body := fmt.Sprintf(`{"versions":true,"before":%q,"dry_run":%t}`, attempt.before, dryRun)
		rec := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/purge", strings.NewReader(body), map[string]string{
			"Authorization": "Bearer secret", "Content-Type": "application/json", "Origin": "nogon://",
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("purge dry=%t status=%d: %s", dryRun, rec.Code, rec.Body)
		}
		var result struct {
			Versions int64 `json:"versions"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Versions != 1 {
			t.Fatalf("versions=%d, want 1 strictly before the cutoff", result.Versions)
		}
		want := int64(2)
		if dryRun {
			want = 3
		}
		if count, err := model.CountDocumentVersions(url, 0); err != nil || count != want {
			t.Fatalf("remaining versions=%d, error=%v, want %d", count, err, want)
		}
		if current := idx.GetByDocID(url); current == nil || current.Text != "current text" {
			t.Fatalf("purge changed the current document: %+v", current)
		}
	}
}

func TestPurgeHistoryPreservesPinnedRecentAndSharedRecords(t *testing.T) {
	_, handler := newTokenTestServer(t, false)
	cutoff := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	old := cutoff.Add(-time.Second)
	for _, entry := range []struct {
		user       uint
		query, url string
		updated    time.Time
		pinned     bool
	}{
		{1, "expired", "https://example.com/expired", old, false},
		{2, "shared", "https://example.com/shared", old, false},
		{2, "shared", "https://example.com/current", cutoff, false},
		{3, "pinned", "https://example.com/shared", old, true},
		{4, "other", "https://example.com/shared", cutoff.Add(time.Second), false},
	} {
		if err := model.UpdateHistory(entry.user, entry.query, entry.url, "Title"); err != nil {
			t.Fatal(err)
		}
		items, err := model.GetLatestHistoryItems(entry.user, 10, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range items {
			if item.URL == entry.url {
				if err := model.DB.Model(&model.HistoryLink{}).Where("id = ?", item.ID).
					UpdateColumns(map[string]any{"updated_at": entry.updated, "pinned": entry.pinned}).Error; err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	if model.GetOrCreateHistory(5, "orphan") == nil || model.GetOrCreateLink("https://example.com/orphan", "Orphan") == nil {
		t.Fatal("create orphaned history")
	}
	for _, value := range []any{&model.History{}, &model.Link{}} {
		if err := model.DB.Model(value).Where("id > 0").UpdateColumn("updated_at", old).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := model.DB.Create(&model.DocumentVersion{URL: "https://example.com/expired", CreatedAt: old}).Error; err != nil {
		t.Fatal(err)
	}
	for _, dryRun := range []bool{true, false} {
		body := fmt.Sprintf(`{"versions":true,"history":true,"before":"2026-01-01T00:00:00Z","dry_run":%t}`, dryRun)
		rec := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/purge", strings.NewReader(body), map[string]string{
			"Authorization": "Bearer secret", "Content-Type": "application/json", "Origin": "nogon://",
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("purge status=%d: %s", rec.Code, rec.Body)
		}
		var result types.PurgeResult
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.HistoryEntries != 2 || result.HistoryQueries != 2 || result.HistoryURLs != 2 || result.Versions != 1 {
			t.Fatalf("purge counts = %+v, want 2 entries, 2 queries, 2 URLs and 1 version", result)
		}
		want := int64(3)
		if dryRun {
			want = 5
		}
		for _, value := range []any{&model.HistoryLink{}, &model.History{}} {
			var count int64
			if err := model.DB.Unscoped().Model(value).Count(&count).Error; err != nil || count != want {
				t.Fatalf("remaining %T rows=%d, err=%v, want %d", value, count, err, want)
			}
		}
		var urls int64
		wantURLs := int64(2)
		if dryRun {
			wantURLs = 4
		}
		if err := model.DB.Unscoped().Model(&model.Link{}).Count(&urls).Error; err != nil || urls != wantURLs {
			t.Fatalf("remaining URL rows=%d, err=%v, want %d", urls, err, wantURLs)
		}
	}
	for _, user := range []uint{2, 3, 4} {
		items, err := model.GetLatestHistoryItems(user, 10, 0)
		if err != nil || len(items) != 1 {
			t.Fatalf("user %d retained history=%+v, err=%v", user, items, err)
		}
	}
}

func TestPurgeCrawlJobsPreservesResumableWork(t *testing.T) {
	_, handler := newTokenTestServer(t, false)
	cutoff := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, job := range []struct {
		id, status, urlStatus string
		updated               time.Time
	}{
		{"expired", model.CrawlJobCompleted, model.CrawlURLDone, cutoff.Add(-time.Second)},
		{"running", model.CrawlJobRunning, model.CrawlURLDone, cutoff.Add(-time.Second)},
		{"interrupted", model.CrawlJobInterrupted, model.CrawlURLFailed, cutoff.Add(-time.Second)},
		{"boundary", model.CrawlJobCompleted, model.CrawlURLDone, cutoff},
		{"pending", model.CrawlJobCompleted, model.CrawlURLPending, cutoff.Add(-time.Second)},
		{"active", model.CrawlJobCompleted, model.CrawlURLInProgress, cutoff.Add(-time.Second)},
	} {
		if err := model.DB.Create(&model.CrawlJob{ID: job.id, Status: job.status, UpdatedAt: job.updated}).Error; err != nil {
			t.Fatal(err)
		}
		if err := model.DB.Create(&model.CrawlURL{JobID: job.id, URL: "https://example.com/" + job.id, Status: job.urlStatus}).Error; err != nil {
			t.Fatal(err)
		}
	}
	// Cross the purge batch boundary while retaining every resumable job above.
	for n := range 200 {
		id := fmt.Sprintf("old-%03d", n)
		if err := model.DB.Create(&model.CrawlJob{ID: id, Status: model.CrawlJobCompleted, UpdatedAt: cutoff.Add(-time.Second)}).Error; err != nil {
			t.Fatal(err)
		}
		if err := model.DB.Create(&model.CrawlURL{JobID: id, URL: "https://example.com/" + id, Status: model.CrawlURLFailed}).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, dryRun := range []bool{true, false} {
		body := fmt.Sprintf(`{"crawl_jobs":true,"before":"2026-01-01T00:00:00Z","dry_run":%t}`, dryRun)
		rec := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/purge", strings.NewReader(body), map[string]string{
			"Authorization": "Bearer secret", "Content-Type": "application/json", "Origin": "nogon://",
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("purge status=%d: %s", rec.Code, rec.Body)
		}
		var result types.PurgeResult
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.CrawlJobs != 201 || result.CrawlURLs != 201 {
			t.Fatalf("purge counts=%+v, want 201 jobs and 201 URLs", result)
		}
		jobs, err := model.ListCrawlJobs()
		want := 5
		if dryRun {
			want = 206
		}
		if err != nil || len(jobs) != want {
			t.Fatalf("remaining jobs=%d, err=%v, want %d", len(jobs), err, want)
		}
		for _, job := range jobs {
			if !dryRun && job.ID == "expired" {
				t.Fatal("expired job remains")
			}
			if n, err := model.CountCrawlURLs(job.ID); err != nil || n != 1 {
				t.Fatalf("job %s URLs=%d, err=%v", job.ID, n, err)
			}
		}
	}
	if n, err := model.CountCrawlURLs("expired"); err != nil || n != 0 {
		t.Fatalf("purged job URLs=%d, err=%v", n, err)
	}
}

func TestPurgeRequiresAdminAndCSRF(t *testing.T) {
	cfg, handler, _, admin := newUserDeletionTestServer(t)
	cfg.App.Public = true
	alice := testutil.CreateUser(t, "purge-alice")
	const body = `{"versions":true,"before":"2026-01-01T00:00:00Z","dry_run":true}`
	for _, tc := range []struct {
		token, origin string
		want          int
	}{
		{"", "nogon://", http.StatusForbidden},
		{alice.Token, "nogon://", http.StatusForbidden},
		{admin.Token, "https://other.example", http.StatusForbidden},
		{admin.Token, "nogon://", http.StatusOK},
	} {
		rec := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/purge", strings.NewReader(body), map[string]string{"Authorization": "Bearer " + tc.token, "Origin": tc.origin})
		if rec.Code != tc.want {
			t.Fatalf("purge authorization status=%d, want %d: %s", rec.Code, tc.want, rec.Body)
		}
	}
	login := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/login", strings.NewReader(`{"username":"admin","password":"password123"}`), map[string]string{"Origin": "nogon://"})
	cookie := responseSessionCookie(t, login)
	for _, csrf := range []bool{false, true} {
		req := httptest.NewRequest(http.MethodPost, "/api/purge", strings.NewReader(body))
		req.Header.Set("Origin", "https://proxy.example")
		req.AddCookie(cookie)
		if csrf {
			configReq := httptest.NewRequest(http.MethodGet, "/api/config", nil)
			configReq.Header.Set("Origin", "https://proxy.example")
			configReq.AddCookie(cookie)
			configRec := httptest.NewRecorder()
			handler.ServeHTTP(configRec, configReq)
			req.Header.Set("X-CSRF-Token", configRec.Header().Get("X-CSRF-Token"))
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		want := http.StatusForbidden
		if csrf {
			want = http.StatusOK
		}
		if rec.Code != want {
			t.Fatalf("purge session csrf=%t status=%d: %s", csrf, rec.Code, rec.Body)
		}
	}
}

func TestPurgeRejectsInvalidRequests(t *testing.T) {
	_, handler := newTokenTestServer(t, false)
	for _, body := range []string{
		`{}`, `null`, `{"versions":true}`, `{"before":"2026-01-01T00:00:00Z"}`,
		`{"versions":true,"before":"bad"}`, `{"versions":true,"before":"0001-01-01T00:00:00Z"}`,
		`{"versions":true,"before":"9999-01-01T00:00:00Z"}`,
		`{"versions":true,"before":"2026-01-01T00:00:00Z","unknown":true}`,
		`{"versions":true,"before":"2026-01-01T00:00:00Z"} {}`,
	} {
		rec := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/purge", strings.NewReader(body), map[string]string{"Authorization": "Bearer secret", "Origin": "nogon://"})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("purge %s status=%d, want 400: %s", body, rec.Code, rec.Body)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/api/purge", strings.NewReader(strings.Repeat(" ", int(maxRequestBodyBytes)+1)+`{}`))
	req.ContentLength = -1
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Origin", "nogon://")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized streamed purge request status=%d, want 413", rec.Code)
	}
}

func TestPurgeRollsBackEveryCategoryOnDatabaseFailure(t *testing.T) {
	_, handler := newTokenTestServer(t, false)
	old := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := model.DB.Create(&model.DocumentVersion{URL: "https://example.com/a", CreatedAt: old}).Error; err != nil {
		t.Fatal(err)
	}
	if err := model.UpdateHistory(0, "query", "https://example.com/a", "A"); err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{&model.History{}, &model.HistoryLink{}, &model.Link{}} {
		if err := model.DB.Model(value).Where("id > 0").UpdateColumn("updated_at", old).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := model.DB.Exec("CREATE TRIGGER reject_purge BEFORE DELETE ON histories BEGIN SELECT RAISE(ABORT, 'purge test failure'); END").Error; err != nil {
		t.Fatal(err)
	}
	rec := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/purge", strings.NewReader(`{"versions":true,"history":true,"before":"2026-01-01T00:00:00Z"}`), map[string]string{"Authorization": "Bearer secret", "Origin": "nogon://"})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("purge failure status=%d: %s", rec.Code, rec.Body)
	}
	for _, value := range []any{&model.DocumentVersion{}, &model.History{}, &model.HistoryLink{}, &model.Link{}} {
		var count int64
		if err := model.DB.Unscoped().Model(value).Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("rollback %T count=%d, err=%v", value, count, err)
		}
	}
}
