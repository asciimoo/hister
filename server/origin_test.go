// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/asciimoo/hister/server/testutil"
)

func TestBrowserExtensionOriginsBypassCSRF(t *testing.T) {
	// A POST to a rules endpoint requires either a valid CSRF token or an
	// origin that withCSRF explicitly trusts. Requests from the packaged
	// browser extensions should be trusted; a random cross-origin request
	// should not.
	_, handler := newTokenTestServer(t, false)

	cases := []struct {
		name       string
		origin     string
		wantAllowed bool
	}{
		{"firefox", "moz-extension://a1b2c3d4-e5f6-7890-1234-567890abcdef", true},
		{"chrome", "chrome-extension://cciilamhchpmbdnniabclekddabkifhb", true},
		{"safari", "safari-web-extension://12345678-90AB-CDEF-1234-567890ABCDEF", true},
		{"unknown", "https://random.example", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/rules",
				strings.NewReader("skip=&priority="),
				map[string]string{
					"Content-Type":   "application/x-www-form-urlencoded",
					"Origin":         tc.origin,
					"X-Access-Token": "secret",
				})
			gotForbidden := rec.Code == http.StatusForbidden
			if gotForbidden == tc.wantAllowed {
				t.Fatalf("POST /api/rules from %s: status = %d, want-allowed = %v",
					tc.origin, rec.Code, tc.wantAllowed)
			}
		})
	}
}
