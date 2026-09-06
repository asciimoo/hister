// SPDX-License-Identifier: AGPL-3.0-or-later

package client

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/asciimoo/hister/server/types"
)

func (c *Client) FetchDiagnostics(ctx context.Context) (_ []types.DiagnosticCheck, err error) {
	req, err := c.newRequest(http.MethodGet, "/api/diagnostics", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	defer closeBody(resp, &err)
	if err = checkStatus(resp); err != nil {
		return nil, err
	}
	var checks []types.DiagnosticCheck
	if err = json.NewDecoder(resp.Body).Decode(&checks); err != nil {
		return nil, err
	}
	return checks, nil
}
