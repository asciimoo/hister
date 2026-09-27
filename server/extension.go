package server

import (
	"net/http"
	"strings"
)

const chromeExtensionOrigin = "chrome-extension://cciilamhchpmbdnniabclekddabkifhb"

// Paths the official Chrome/Firefox extensions and the unofficial Safari
// port are allowed to call without a web-session CSRF token. Keep this in
// lockstep with the endpoints the extension actually uses.
var browserExtensionAPIPaths = map[string]struct{}{
	"/add":          {},
	"/api/add":      {},
	"/api/add_pdf":  {},
	"/api/config":   {},
	"/api/rules":    {},
	"/api/delete":   {},
	"/api/label":    {},
	"/api/versions": {},
	"/api/history":  {},
	"/api/profile":  {},
	"/api/document": {},
}

func trustedBrowserExtensionOrigin(origin string) bool {
	switch {
	case strings.HasPrefix(origin, "moz-extension://"):
		return true
	case strings.HasPrefix(origin, "safari-web-extension://"):
		return true
	case origin == chromeExtensionOrigin:
		return true
	default:
		return false
	}
}

func browserExtensionAPIPath(path string) bool {
	_, ok := browserExtensionAPIPaths[path]
	return ok
}

func browserExtensionRequest(origin, path string) bool {
	return trustedBrowserExtensionOrigin(origin) && browserExtensionAPIPath(path)
}

// extensionCORSAllowHeaders returns the response Access-Control-Allow-Headers
// value. Safari preflights every extension fetch and lists the actual request
// headers in Access-Control-Request-Headers; echoing that list keeps
// user-configured custom headers (reverse-proxy auth, etc.) working. When no
// preflight header is present, fall back to the headers the extension always
// sends.
func extensionCORSAllowHeaders(r *http.Request) string {
	if requested := r.Header.Get("Access-Control-Request-Headers"); requested != "" {
		return requested
	}
	return "Content-Type, X-Access-Token, X-Hister-Public, X-CSRF-Token, Authorization"
}

// withExtensionCORS answers CORS preflight and attaches CORS headers for
// requests that come from a trusted browser-extension origin on the extension
// API surface.
//
// Chrome and Firefox exempt extension background fetches from CORS, but
// Safari still preflights JSON / custom-header requests from the popup and
// options pages. Without this, those pages cannot talk to a stock Hister
// server (see asciimoo/hister#49).
func withExtensionCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if !browserExtensionRequest(origin, r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", origin)
		h.Set("Access-Control-Allow-Methods", "GET, HEAD, POST, OPTIONS")
		h.Set("Access-Control-Allow-Headers", extensionCORSAllowHeaders(r))
		h.Set("Access-Control-Allow-Credentials", "true")
		h.Set("Access-Control-Max-Age", "600")
		h.Add("Vary", "Origin")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
