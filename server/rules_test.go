// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/document"
	"github.com/asciimoo/hister/server/model"
	"github.com/asciimoo/hister/server/testutil"
)

func TestSaveRulesPreservesOmittedRuleGroups(t *testing.T) {
	cfg, handler := newTokenTestServer(t, false)
	cfg.Rules.Allow.ReStrs = []string{"old-allow"}
	cfg.Rules.Skip.ReStrs = []string{"old-skip"}
	cfg.Rules.Priority.ReStrs = []string{"old-priority"}
	cfg.Rules.Versioning.ReStrs = []string{"old-versioning"}
	if err := cfg.SaveRules(); err != nil {
		t.Fatal(err)
	}

	rec := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/rules",
		strings.NewReader(url.Values{"skip": {"new-skip"}}.Encode()),
		map[string]string{
			"Content-Type":   "application/x-www-form-urlencoded",
			"Origin":         "hister://",
			"X-Access-Token": "secret",
		})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/rules status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	assertRulePatterns(t, "skip", cfg.Rules.Skip.ReStrs, []string{"new-skip"})
	assertRulePatterns(t, "allow", cfg.Rules.Allow.ReStrs, []string{"old-allow"})
	assertRulePatterns(t, "priority", cfg.Rules.Priority.ReStrs, []string{"old-priority"})
	assertRulePatterns(t, "versioning", cfg.Rules.Versioning.ReStrs, []string{"old-versioning"})
}

func TestSaveRulesClearsExplicitEmptyRuleGroup(t *testing.T) {
	cfg, handler := newTokenTestServer(t, false)
	cfg.Rules.Skip.ReStrs = []string{"old-skip"}
	cfg.Rules.Priority.ReStrs = []string{"old-priority"}
	cfg.Rules.Versioning.ReStrs = []string{"old-versioning"}
	if err := cfg.SaveRules(); err != nil {
		t.Fatal(err)
	}

	rec := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/rules",
		strings.NewReader(url.Values{"versioning": {""}}.Encode()),
		map[string]string{
			"Content-Type":   "application/x-www-form-urlencoded",
			"Origin":         "hister://",
			"X-Access-Token": "secret",
		})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/rules status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	assertRulePatterns(t, "skip", cfg.Rules.Skip.ReStrs, []string{"old-skip"})
	assertRulePatterns(t, "priority", cfg.Rules.Priority.ReStrs, []string{"old-priority"})
	assertRulePatterns(t, "versioning", cfg.Rules.Versioning.ReStrs, []string{})
}

func TestSaveUserRulesPreservesOmittedRuleGroups(t *testing.T) {
	cfg := testutil.Config(t)
	cfg.App.UserHandling = true
	cfg.Server.Address = "127.0.0.1:4433"
	if err := cfg.UpdateBaseURL("http://127.0.0.1:4433"); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveRules(); err != nil {
		t.Fatal(err)
	}
	cfg.Server.Database = "file::memory:"
	testutil.InitModelWithConfig(t, cfg)
	sessionStore = newSessionStore([]byte(strings.Repeat("x", 32)), cfg.BaseURL(""), sessionMaxAge)
	user := testutil.CreateUser(t, "alice")
	rules := &config.Rules{
		Allow:      &config.Rule{ReStrs: []string{"old-allow"}},
		Skip:       &config.Rule{ReStrs: []string{"old-skip"}},
		Priority:   &config.Rule{ReStrs: []string{"old-priority"}},
		Versioning: &config.Rule{ReStrs: []string{"old-versioning"}},
		Aliases:    make(config.Aliases),
	}
	if err := rules.Compile(); err != nil {
		t.Fatal(err)
	}
	if err := model.SaveUserRules(user.ID, rules); err != nil {
		t.Fatal(err)
	}
	handler := registerEndpoints(cfg, newServerTestIndexer(t, cfg))

	rec := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/rules",
		strings.NewReader(url.Values{"skip": {"new-skip"}}.Encode()),
		map[string]string{
			"Content-Type":   "application/x-www-form-urlencoded",
			"Origin":         "hister://",
			"X-Access-Token": user.Token,
		})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/rules status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	saved, err := model.GetUserRules(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertRulePatterns(t, "skip", saved.Skip.ReStrs, []string{"new-skip"})
	assertRulePatterns(t, "allow", saved.Allow.ReStrs, []string{"old-allow"})
	assertRulePatterns(t, "priority", saved.Priority.ReStrs, []string{"old-priority"})
	assertRulePatterns(t, "versioning", saved.Versioning.ReStrs, []string{"old-versioning"})
}

func TestSaveAllowRules(t *testing.T) {
	cfg, handler := newTokenTestServer(t, false)
	headers := map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Origin": "hister://", "X-Access-Token": "secret"}
	for _, tc := range []struct {
		patterns string
		status   int
		want     []string
	}{
		{"example.com docs.example.org example.com", http.StatusOK, []string{"example.com", "docs.example.org"}},
		{"[", http.StatusBadRequest, []string{"example.com", "docs.example.org"}},
		{"", http.StatusOK, []string{}},
	} {
		rec := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/rules", strings.NewReader(url.Values{"allow": {tc.patterns}}.Encode()), headers)
		if rec.Code != tc.status {
			t.Fatalf("save %q: status = %d, body = %s", tc.patterns, rec.Code, rec.Body.String())
		}
		assertRulePatterns(t, "allow", cfg.Rules.Allow.ReStrs, tc.want)
		rec = testutil.ServeHTTP(t, handler, http.MethodGet, "/api/rules", nil, headers)
		var response struct {
			Allow []string `json:"allow"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if response.Allow == nil {
			t.Fatal("allow response should be an array")
		}
		assertRulePatterns(t, "allow response", response.Allow, tc.want)
	}
}

func TestAllowRulesApplyToSubmissionsAndReindex(t *testing.T) {
	cfg, handler := newTokenTestServer(t, false)
	headers := map[string]string{"Content-Type": "application/json", "Origin": "hister://", "X-Access-Token": "secret"}
	docs := []struct {
		url    string
		manual bool
		keep   bool
	}{
		{"https://allowed.example/page", false, true},
		{"https://other.example/page", false, false},
		{"https://allowed.example/private/page", false, false},
		{"https://other.example/manual", true, true},
		{"https://allowed.example/private/manual", true, true},
	}
	for _, d := range docs {
		body := fmt.Sprintf(`{"url":%q,"text":"Saved content","processed":true,"metadata":{"ignore_skip_rules":%t}}`, d.url, d.manual)
		rec := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/add", strings.NewReader(body), headers)
		if rec.Code != http.StatusCreated {
			t.Fatalf("seed %s: status %d, %s", d.url, rec.Code, rec.Body.String())
		}
	}
	cfg.Rules.Allow.ReStrs = []string{`^https://allowed\.example/`}
	cfg.Rules.Skip.ReStrs = []string{`/private/`}
	if err := cfg.SaveRules(); err != nil {
		t.Fatal(err)
	}
	for _, d := range docs {
		body := fmt.Sprintf(`{"url":%q,"text":"Updated content","processed":true,"metadata":{"ignore_skip_rules":%t}}`, d.url, d.manual)
		for _, endpoint := range []string{"/api/add", "/api/batch"} {
			payload := body
			if endpoint == "/api/batch" {
				payload = `{"ops":[{"op":"add",` + body[1:] + `]}`
			}
			rec := testutil.ServeHTTP(t, handler, http.MethodPost, endpoint, strings.NewReader(payload), headers)
			status := rec.Code
			if endpoint == "/api/batch" {
				var response batchResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				if len(response.Results) != 1 {
					t.Fatalf("batch response = %s", rec.Body.String())
				}
				status = response.Results[0].Status
			}
			want := http.StatusNotAcceptable
			if d.keep {
				want = http.StatusCreated
			}
			if status != want {
				t.Errorf("%s %s: status = %d, want %d", endpoint, d.url, status, want)
			}
		}
	}
	// A stored manual override must not authorize a later automatic submission.
	rec := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/add", strings.NewReader(`{"url":"https://other.example/manual","text":"automatic"}`), headers)
	if rec.Code != http.StatusNotAcceptable {
		t.Fatalf("automatic update status = %d", rec.Code)
	}
	// PDF rules must be checked before parsing or storing the file.
	rec = testutil.ServeHTTP(t, handler, http.MethodPost, "/api/add_pdf", strings.NewReader(`{"document":{"url":"https://other.example/file.pdf"},"pdf":"bm90IGEgcGRm"}`), headers)
	if rec.Code != http.StatusNotAcceptable {
		t.Fatalf("excluded PDF status = %d", rec.Code)
	}
	rec = testutil.ServeHTTP(t, handler, http.MethodPost, "/api/reindex", strings.NewReader(`{"detectLanguages":false}`), headers)
	if rec.Code != http.StatusOK {
		t.Fatalf("reindex: %d %s", rec.Code, rec.Body.String())
	}
	for _, d := range docs {
		rec := testutil.ServeHTTP(t, handler, http.MethodHead, "/api/document?url="+url.QueryEscape(d.url), nil, headers)
		want := http.StatusNotFound
		if d.keep {
			want = http.StatusOK
		}
		if rec.Code != want {
			t.Errorf("after reindex %s: status = %d, want %d", d.url, rec.Code, want)
		}
	}
}

func TestIndexingRulesFilterExtractedDocuments(t *testing.T) {
	for _, multiUser := range []bool{false, true} {
		for _, endpoint := range []string{"/api/add", "/api/batch"} {
			t.Run(fmt.Sprintf("multiUser=%t%s", multiUser, endpoint), func(t *testing.T) {
				cfg := testutil.Config(t)
				cfg.App.UserHandling = multiUser
				cfg.App.AccessToken = "secret"
				cfg.Server.Database = "file::memory:"
				if err := cfg.SaveRules(); err != nil {
					t.Fatal(err)
				}
				testutil.InitModelWithConfig(t, cfg)
				sessionStore = newSessionStore([]byte(strings.Repeat("x", 32)), cfg.BaseURL(""), sessionMaxAge)
				rules := &config.Rules{
					Allow: &config.Rule{ReStrs: []string{`^https://bsky\.app/profile/alice\.test/`}},
					Skip:  &config.Rule{ReStrs: []string{`/post/private$`}},
				}
				uid := uint(0)
				token := "secret"
				if multiUser {
					user := testutil.CreateUser(t, "alice")
					uid, token = user.ID, user.Token
					if err := model.SaveUserRules(uid, rules); err != nil {
						t.Fatal(err)
					}
					// Instance rules must not replace the submitting user's rules.
					cfg.Rules.Allow.ReStrs = []string{`^https://instance\.example/`}
				} else {
					cfg.Rules.Allow = rules.Allow
					cfg.Rules.Skip = rules.Skip
				}
				if err := cfg.SaveRules(); err != nil {
					t.Fatal(err)
				}
				idx := newServerTestIndexer(t, cfg)
				handler := registerEndpoints(cfg, idx)
				headers := map[string]string{"Content-Type": "application/json", "Origin": "hister://", "X-Access-Token": token}
				const rootURL = "https://bsky.app/profile/alice.test/post/root"
				urls := []string{
					rootURL,
					"https://bsky.app/profile/bob.test/post/reply",
					"https://bsky.app/profile/alice.test/post/private",
				}
				for _, manual := range []bool{false, true} {
					d := document.Document{
						URL: rootURL,
						HTML: `<script type="application/ld+json">{
							"@type":"DiscussionForumPosting",
							"url":"https://bsky.app/profile/alice.test/post/root",
							"text":"Root post",
							"comment":[
								{"@type":"Comment","url":"https://bsky.app/profile/bob.test/post/reply","text":"Reply outside the allow rules"},
								{"@type":"Comment","url":"https://bsky.app/profile/alice.test/post/private","text":"Reply matching a skip rule"}
							]
						}</script>`,
					}
					d.SetIgnoreSkipRules(manual)
					var payload any = d
					if endpoint == "/api/batch" {
						payload = batchRequest{Ops: []batchOp{{Op: batchOpAdd, Document: d}}}
					}
					body, err := json.Marshal(payload)
					if err != nil {
						t.Fatal(err)
					}
					rec := testutil.ServeHTTP(t, handler, http.MethodPost, endpoint, strings.NewReader(string(body)), headers)
					status := rec.Code
					if endpoint == "/api/batch" {
						var response batchResponse
						if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
							t.Fatal(err)
						}
						if len(response.Results) != 1 {
							t.Fatalf("batch response = %s", rec.Body.String())
						}
						status = response.Results[0].Status
					}
					if status != http.StatusCreated {
						t.Fatalf("manual=%t: status = %d, body = %s", manual, status, rec.Body.String())
					}
					for _, reindex := range []bool{false, true} {
						if reindex {
							// Seed a saved thread from before the extractor was enabled.
							// Reindexing must filter the newly extracted replies too.
							d.Processed = true
							d.UserID = uid
							d.Text = "Saved thread"
							if err := idx.Add(&d); err != nil {
								t.Fatal(err)
							}
							if err := idx.Reindex(cfg.Rules, false, false, false, nil); err != nil {
								t.Fatal(err)
							}
						}
						for _, rawURL := range urls {
							doc := idx.GetByURLAndUser(rawURL, uid)
							want := manual || rawURL == rootURL
							if (doc != nil) != want {
								t.Fatalf("manual=%t reindex=%t: document %s stored=%t, want %t", manual, reindex, rawURL, doc != nil, want)
							}
							if manual && !doc.IgnoreSkipRules() {
								t.Errorf("manual override missing from %s", rawURL)
							}
						}
					}
				}
			})
		}
	}
}

func TestReindexAppliesDocumentOwnerRules(t *testing.T) {
	cfg := testutil.Config(t)
	cfg.App.UserHandling = true
	cfg.Server.Database = "file::memory:"
	if err := cfg.SaveRules(); err != nil {
		t.Fatal(err)
	}
	cfg.Rules.Allow.ReStrs = []string{`^https://public\.example/`}
	if err := cfg.Rules.Compile(); err != nil {
		t.Fatal(err)
	}
	testutil.InitModelWithConfig(t, cfg)
	alice := testutil.CreateUser(t, "alice")
	bob := testutil.CreateUser(t, "bob")
	for _, u := range []*model.User{alice, bob} {
		rules := &config.Rules{Allow: &config.Rule{ReStrs: []string{`^https://` + u.Username + `\.example/`}}}
		if err := model.SaveUserRules(u.ID, rules); err != nil {
			t.Fatal(err)
		}
	}
	idx := newServerTestIndexer(t, cfg)
	docs := []struct {
		url    string
		owner  uint
		manual bool
		keep   bool
	}{
		{"https://alice.example/page", alice.ID, false, true},
		{"https://alice.example/page", bob.ID, false, false},
		{"https://outside.example/manual", bob.ID, true, true},
		{"https://public.example/page", 0, false, true},
		{"https://outside.example/page", 0, false, false},
	}
	for _, d := range docs {
		doc := &document.Document{URL: d.url, UserID: d.owner, Text: "Saved content", Processed: true}
		doc.SetIgnoreSkipRules(d.manual)
		if err := idx.Add(doc); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := idx.Reindex(cfg.Rules, false, false, false, nil); err != nil {
			t.Fatal(err)
		}
		for _, d := range docs {
			got := idx.GetByURLAndUser(d.url, d.owner)
			if (got != nil) != d.keep {
				t.Errorf("user %d URL %s retained=%v, want %v", d.owner, d.url, got != nil, d.keep)
			}
		}
	}
}

func TestUserRulesInitializeAllow(t *testing.T) {
	for _, raw := range []string{"", `{}`, `{"allow":null}`, `{"skip":["private"]}`} {
		rules, err := (&model.User{RulesJSON: raw}).ParseRules()
		if err != nil {
			t.Fatal(err)
		}
		if rules.Allow == nil || len(rules.Allow.ReStrs) != 0 {
			t.Fatalf("unexpected allow rules for %s", raw)
		}
		if rules.IsSkip("https://example.com/public") {
			t.Fatal("legacy rules unexpectedly restrict public URLs")
		}
	}
}

func assertRulePatterns(t *testing.T, label string, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("%s rules = %#v, want %#v", label, got, want)
	}
}
