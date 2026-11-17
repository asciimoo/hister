// SPDX-License-Identifier: AGPL-3.0-or-later

package model_test

import (
	"testing"

	"github.com/asciimoo/nogon/server/model"
	"github.com/asciimoo/nogon/server/testutil"
)

func TestMoveDocumentVersions(t *testing.T) {
	testutil.InitModel(t)
	const url = "https://example.com/article"
	if err := model.SaveDocumentVersion(url, 0, "html", "text"); err != nil {
		t.Fatal(err)
	}
	if err := model.MoveDocumentVersions(url, 0, 9); err != nil {
		t.Fatal(err)
	}
	if count, err := model.CountDocumentVersions(url, 0); err != nil || count != 0 {
		t.Fatalf("source version count = %d, error = %v", count, err)
	}
	if count, err := model.CountDocumentVersions(url, 9); err != nil || count != 1 {
		t.Fatalf("destination version count = %d, error = %v", count, err)
	}
}
