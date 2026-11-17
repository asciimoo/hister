// SPDX-License-Identifier: AGPL-3.0-or-later

package client

import (
	"context"
	"net/http"

	servertypes "github.com/asciimoo/nogon/server/types"
)

// UpdateDocuments changes attributes on documents selected by a search query.
func (c *Client) UpdateDocuments(request servertypes.UpdateDocumentsRequest) (*servertypes.UpdateDocumentsResult, error) {
	var result servertypes.UpdateDocumentsResult
	if err := c.requestJSON(context.Background(), http.MethodPost, "/api/update", request, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
