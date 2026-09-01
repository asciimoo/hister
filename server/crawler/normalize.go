// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"net/url"
	"sort"
	"strings"
)

var trackingParams = map[string]struct{}{
	"utm_source":   {},
	"utm_medium":   {},
	"utm_campaign": {},
	"utm_term":     {},
	"utm_content":  {},
	"fbclid":       {},
	"gclid":        {},
	"mc_cid":       {},
	"mc_eid":       {},
	"ref":          {},
	"ref_src":      {},
}

// NormalizeURL returns a canonical string form of u suitable for deduplication.
// It lowercases the host, strips default ports, sorts query params, strips
// known tracking params, removes the fragment, and collapses duplicate slashes
// in the path (preserving a leading //).
func NormalizeURL(u *url.URL) string {
	out := *u

	// Lowercase host and strip default port.
	host := strings.ToLower(out.Hostname())
	port := out.Port()
	switch {
	case out.Scheme == "http" && port == "80":
		out.Host = host
	case out.Scheme == "https" && port == "443":
		out.Host = host
	case port != "":
		out.Host = host + ":" + port
	default:
		out.Host = host
	}

	// Strip fragment.
	out.Fragment = ""

	// Strip tracking params and sort the remainder.
	q := out.Query()
	changed := false
	for k := range q {
		if _, ok := trackingParams[k]; ok {
			delete(q, k)
			changed = true
		}
	}
	if changed || out.RawQuery != "" {
		keys := make([]string, 0, len(q))
		for k := range q {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		sorted := url.Values{}
		for _, k := range keys {
			sorted[k] = q[k]
		}
		out.RawQuery = sorted.Encode()
	}

	// Collapse duplicate slashes in path, but preserve leading //.
	path := out.Path
	if strings.HasPrefix(path, "//") {
		path = "//" + collapseDuplicateSlashes(path[2:])
	} else {
		path = collapseDuplicateSlashes(path)
	}
	out.Path = path

	return out.String()
}

func collapseDuplicateSlashes(s string) string {
	if !strings.Contains(s, "//") {
		return s
	}
	var b strings.Builder
	prev := rune(0)
	for _, c := range s {
		if c == '/' && prev == '/' {
			continue
		}
		b.WriteRune(c)
		prev = c
	}
	return b.String()
}
