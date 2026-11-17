// SPDX-License-Identifier: AGPL-3.0-or-later

package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	servertypes "github.com/asciimoo/nogon/server/types"
)

func TestUpdateDocumentsPayloadAndResponse(t *testing.T) {
	label := "research"
	userID := uint(4)
	var received servertypes.UpdateDocumentsRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/update" || r.Method != http.MethodPost {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if contentType := r.Header.Get("Content-Type"); contentType != "application/json" {
			t.Errorf("Content-Type = %q", contentType)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Error(err)
		}
		_ = json.NewEncoder(w).Encode(servertypes.UpdateDocumentsResult{Matched: 3, Updated: 2, Conflicts: 1})
	}))
	defer server.Close()

	request := servertypes.UpdateDocumentsRequest{
		Query: "domain:example.com",
		Changes: servertypes.DocumentChanges{
			UserID: &userID,
			Label:  &label,
		},
		DryRun: true,
	}
	result, err := New(server.URL).UpdateDocuments(request)
	if err != nil {
		t.Fatal(err)
	}
	if received.Query != request.Query || received.Changes.UserID == nil || *received.Changes.UserID != userID || received.Changes.Label == nil || *received.Changes.Label != label || !received.DryRun {
		t.Fatalf("request = %+v", received)
	}
	if result.Matched != 3 || result.Updated != 2 || result.Conflicts != 1 {
		t.Fatalf("result = %+v", result)
	}
}
