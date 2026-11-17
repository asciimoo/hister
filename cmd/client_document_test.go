// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"encoding/json"
	"testing"

	"github.com/asciimoo/nogon/client"
	"github.com/asciimoo/nogon/server/document"
)

func TestClientDocumentPreservesExportedLanguageOverride(t *testing.T) {
	const exported = `{"url":"https://example.com/pinned","language":"en","language_override":"en"}`
	var decoded client.Document
	if err := json.Unmarshal([]byte(exported), &decoded); err != nil {
		t.Fatal(err)
	}
	exportJSON, err := json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	var imported document.Document
	if err := json.Unmarshal(exportJSON, &imported); err != nil {
		t.Fatal(err)
	}
	submission, err := json.Marshal(clientDocument(&imported))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(submission, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["language_override"] != "en" {
		t.Fatalf("export and import lost language override: %s", submission)
	}
}
