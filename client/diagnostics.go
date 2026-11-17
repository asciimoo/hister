// SPDX-License-Identifier: AGPL-3.0-or-later

package client

import (
	"context"
	"net/http"

	"github.com/asciimoo/nogon/server/types"
)

func (c *Client) FetchDiagnostics(ctx context.Context) ([]types.DiagnosticCheck, error) {
	var checks []types.DiagnosticCheck
	if err := c.requestJSON(ctx, http.MethodGet, "/api/diagnostics", nil, &checks); err != nil {
		return nil, err
	}
	return checks, nil
}
