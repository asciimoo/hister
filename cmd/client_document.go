// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"github.com/asciimoo/nogon/client"
	"github.com/asciimoo/nogon/server/document"
)

// clientDocument prepares locally extracted content for submission to the server.
func clientDocument(d *document.Document) *client.Document {
	if d == nil {
		return nil
	}
	return &client.Document{
		DocumentID:         d.DocumentID,
		URL:                d.URL,
		Domain:             d.Domain,
		HTML:               d.HTML,
		HTMLKey:            d.HTMLKey,
		Title:              d.Title,
		Text:               d.Text,
		Favicon:            d.Favicon,
		FaviconKey:         d.FaviconKey,
		Score:              d.Score,
		Added:              d.Added,
		Updated:            d.Updated,
		Type:               client.DocType(d.Type),
		Language:           d.Language,
		LanguageOverride:   d.LanguageOverride,
		UserID:             d.UserID,
		Label:              d.Label,
		AddCount:           d.AddCount,
		Metadata:           d.Metadata,
		SkipSensitiveCheck: d.SkipSensitiveCheck,
		Processed:          d.Processed,
	}
}
