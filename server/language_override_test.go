// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/asciimoo/nogon/server/testutil"
)

func TestLanguageOverrideSubmission(t *testing.T) {
	for _, endpoint := range []string{"/api/add", "/api/batch"} {
		for _, tc := range []struct {
			name, url, language, text string
			docType                   int
			status                    int
		}{
			{"web", "https://example.com/import", "de", "Imported text", 0, http.StatusCreated},
			{"regional", "https://example.com/regional", "en-US", "Imported text", 0, http.StatusCreated},
			{"no analyzer", "https://example.com/ukrainian", "uk", "Imported text", 0, http.StatusCreated},
			{"remote", "remote-file://client/tmp/note.txt", "de", "Imported text", 2, http.StatusCreated},
			{"invalid", "https://example.com/invalid", "../en", "text", 0, http.StatusBadRequest},
			{"sensitive", "remote-file://client/tmp/secret.txt", "de", "-----BEGIN OPENSSH PRIVATE KEY-----", 2, http.StatusUnprocessableEntity},
		} {
			t.Run(endpoint+"/"+tc.name, func(t *testing.T) {
				cfg := testutil.Config(t)
				idx := newServerTestIndexer(t, cfg)
				data, err := json.Marshal(map[string]any{
					"op": "add", "url": tc.url, "type": tc.docType, "text": tc.text,
					"language_override": tc.language, "processed": true,
				})
				if err != nil {
					t.Fatal(err)
				}
				body := string(data)
				if endpoint == "/api/batch" {
					body = `{"ops":[` + body + `]}`
				}
				req := httptest.NewRequest(http.MethodPost, endpoint, strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				ctx := &webContext{Request: req, Response: rec, Config: cfg, Indexer: idx}
				status := 0
				if endpoint == "/api/add" {
					serveAdd(ctx)
					status = rec.Code
				} else {
					serveBatch(ctx)
					var result batchResponse
					if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil || len(result.Results) != 1 {
						t.Fatalf("batch response = %s, error = %v", rec.Body, err)
					}
					status = result.Results[0].Status
				}
				if status != tc.status {
					t.Fatalf("status = %d, want %d: %s", status, tc.status, rec.Body)
				}
				stored := idx.GetByURLAndUser(tc.url, 0)
				if tc.status == http.StatusCreated {
					if stored == nil || stored.Language != tc.language {
						t.Fatalf("stored document = %+v, want language %q", stored, tc.language)
					}
				} else if stored != nil {
					t.Fatal("rejected document was stored")
				}
			})
		}
	}
}
