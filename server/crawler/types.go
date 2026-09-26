// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"fmt"
	"time"
)

// Link is a hyperlink extracted from a page.
type Link struct {
	Href string
	Rel  string
}

// FetchMeta carries HTTP response metadata from a fetch.
type FetchMeta struct {
	StatusCode int
	RetryAfter time.Duration
}

// HTTPStatusError is returned by fetchPage when the server responds with a
// non-2xx status code.
type HTTPStatusError struct {
	Status     int
	RetryAfter time.Duration
}

func (e *HTTPStatusError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("HTTP %d (retry after %s)", e.Status, e.RetryAfter)
	}
	return fmt.Sprintf("HTTP %d", e.Status)
}
