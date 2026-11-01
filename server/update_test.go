// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/asciimoo/hister/server/document"
	"github.com/asciimoo/hister/server/model"
	"github.com/asciimoo/hister/server/testutil"
	servertypes "github.com/asciimoo/hister/server/types"
)

func TestUpdateDocumentsAuthorizationScopeAndVersionMove(t *testing.T) {
	cfg := testutil.Config(t)
	cfg.App.UserHandling = true
	cfg.Server.Database = "test.sqlite3"
	cfg.Server.Address = "127.0.0.1:4433"
	if err := cfg.UpdateBaseURL("http://127.0.0.1:4433"); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveRules(); err != nil {
		t.Fatal(err)
	}
	testutil.InitModelWithConfig(t, cfg)
	sessionStore = newSessionStore([]byte(strings.Repeat("x", 32)), cfg.BaseURL(""), sessionMaxAge)
	admin, err := model.CreateUser("admin", "password123", true)
	if err != nil {
		t.Fatal(err)
	}
	alice := testutil.CreateUser(t, "alice")
	target := testutil.CreateUser(t, "target")
	idx := newServerTestIndexer(t, cfg)
	handler := registerEndpoints(cfg, idx)

	ownedURL := "https://example.com/private"
	globalURL := "https://example.com/global"
	for _, d := range []*document.Document{
		{URL: ownedURL, Title: "Private", Text: "private body", UserID: alice.ID, Processed: true},
		{URL: globalURL, Title: "Global", Text: "global body", Processed: true},
	} {
		if err := idx.Add(d); err != nil {
			t.Fatal(err)
		}
	}

	label := "mine"
	labelRequest := servertypes.UpdateDocumentsRequest{
		Query:   "*",
		Changes: servertypes.DocumentChanges{Label: &label, Language: new("de")},
	}
	labelBody, _ := json.Marshal(labelRequest)
	labelRec := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/update", strings.NewReader(string(labelBody)), map[string]string{
		"Content-Type":   "application/json",
		"Origin":         "hister://",
		"X-Access-Token": alice.Token,
	})
	if labelRec.Code != http.StatusOK {
		t.Fatalf("regular update status = %d, body = %s", labelRec.Code, labelRec.Body.String())
	}
	var labelResult servertypes.UpdateDocumentsResult
	if err := json.Unmarshal(labelRec.Body.Bytes(), &labelResult); err != nil {
		t.Fatal(err)
	}
	if labelResult.Matched != 1 || labelResult.Updated != 1 {
		t.Fatalf("regular update result = %+v", labelResult)
	}
	if got := idx.GetByDocID(document.GetDocID(alice.ID, ownedURL)).Label; got != label {
		t.Fatalf("owned label = %q, want %q", got, label)
	}
	if got := idx.GetByDocID(globalURL).Label; got != "" {
		t.Fatalf("global label = %q, want empty", got)
	}
	if got := idx.GetByDocID(globalURL).LanguageOverride; got != "" {
		t.Fatalf("regular user pinned a global document to %q", got)
	}
	resubmit := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/add",
		strings.NewReader(fmt.Sprintf(`{"url":%q,"text":"Updated content"}`, ownedURL)), map[string]string{
			"Content-Type": "application/json", "Origin": "hister://", "X-Access-Token": alice.Token,
		})
	if resubmit.Code != http.StatusCreated {
		t.Fatalf("resubmission status = %d: %s", resubmit.Code, resubmit.Body)
	}
	if got := idx.GetByDocID(document.GetDocID(alice.ID, ownedURL)); got.Language != "de" || got.LanguageOverride != "de" {
		t.Fatalf("resubmission lost the owner's language pin: %+v", got)
	}

	unauthorizedRequest := fmt.Sprintf(`{"query":"*","changes":{"user_id":%d}}`, target.ID)
	unauthorizedRec := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/update", strings.NewReader(unauthorizedRequest), map[string]string{
		"Content-Type":   "application/json",
		"Origin":         "hister://",
		"X-Access-Token": alice.Token,
	})
	if unauthorizedRec.Code != http.StatusForbidden {
		t.Fatalf("regular ownership update status = %d, want %d", unauthorizedRec.Code, http.StatusForbidden)
	}

	if err := model.SaveDocumentVersion(ownedURL, alice.ID, "html diff", "text diff"); err != nil {
		t.Fatal(err)
	}
	moveRequest := fmt.Sprintf(`{"query":"user_id:%d","changes":{"user_id":%d}}`, alice.ID, target.ID)
	moveRec := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/update", strings.NewReader(moveRequest), map[string]string{
		"Content-Type":   "application/json",
		"Origin":         "hister://",
		"X-Access-Token": admin.Token,
	})
	if moveRec.Code != http.StatusOK {
		t.Fatalf("admin ownership update status = %d, body = %s", moveRec.Code, moveRec.Body.String())
	}
	if idx.GetByDocID(document.GetDocID(alice.ID, ownedURL)) != nil {
		t.Fatal("source document remains after ownership update")
	}
	if moved := idx.GetByDocID(document.GetDocID(target.ID, ownedURL)); moved == nil {
		t.Fatal("destination document missing after ownership update")
	} else if moved.LanguageOverride != "de" {
		t.Fatalf("ownership move lost language pin: %+v", moved)
	}
	if count, err := model.CountDocumentVersions(ownedURL, alice.ID); err != nil || count != 0 {
		t.Fatalf("source version count = %d, error = %v", count, err)
	}
	if count, err := model.CountDocumentVersions(ownedURL, target.ID); err != nil || count != 1 {
		t.Fatalf("destination version count = %d, error = %v", count, err)
	}
}
