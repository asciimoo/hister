// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"strings"
	"testing"
)

func TestExtractLinks(t *testing.T) {
	tests := []struct {
		name  string
		html  string
		want  []Link
	}{
		{
			name: "basic anchor",
			html: `<html><body><a href="https://example.com">link</a></body></html>`,
			want: []Link{{Href: "https://example.com", Rel: ""}},
		},
		{
			name: "rel nofollow",
			html: `<html><body><a href="/page" rel="nofollow">link</a></body></html>`,
			want: []Link{{Href: "/page", Rel: "nofollow"}},
		},
		{
			name: "multi-valued rel",
			html: `<html><body><a href="/page" rel="nofollow noopener">link</a></body></html>`,
			want: []Link{{Href: "/page", Rel: "nofollow noopener"}},
		},
		{
			name: "no href skipped",
			html: `<html><body><a name="anchor">anchor</a></body></html>`,
			want: nil,
		},
		{
			name: "multiple links",
			html: `<html><body><a href="/a">a</a><a href="/b" rel="nofollow">b</a></body></html>`,
			want: []Link{{Href: "/a", Rel: ""}, {Href: "/b", Rel: "nofollow"}},
		},
		{
			name: "malformed HTML still extracts links",
			html: `<a href="/broken">text<a href="/second">more`,
			want: []Link{{Href: "/broken", Rel: ""}, {Href: "/second", Rel: ""}},
		},
		{
			name: "empty",
			html: ``,
			want: nil,
		},
		{
			name: "link with rel but no nofollow",
			html: `<a href="/page" rel="noopener noreferrer">link</a>`,
			want: []Link{{Href: "/page", Rel: "noopener noreferrer"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractLinks(strings.NewReader(tt.html))
			if len(got) != len(tt.want) {
				t.Fatalf("extractLinks() len = %d, want %d\ngot: %v\nwant: %v", len(got), len(tt.want), got, tt.want)
			}
			for i, link := range got {
				if link.Href != tt.want[i].Href || link.Rel != tt.want[i].Rel {
					t.Errorf("extractLinks()[%d] = %+v, want %+v", i, link, tt.want[i])
				}
			}
		})
	}
}

func TestExtractLinksIsNofollow(t *testing.T) {
	tests := []struct {
		rel  string
		want bool
	}{
		{"nofollow", true},
		{"NOFOLLOW", true},
		{"nofollow noopener", true},
		{"noopener nofollow", true},
		{"noopener", false},
		{"", false},
		{"noreferrer", false},
	}

	for _, tt := range tests {
		t.Run(tt.rel, func(t *testing.T) {
			if got := isNofollow(tt.rel); got != tt.want {
				t.Errorf("isNofollow(%q) = %v, want %v", tt.rel, got, tt.want)
			}
		})
	}
}

func TestExtractLinksStreaming(t *testing.T) {
	// Build a large HTML document to verify streaming tokenizer works.
	var sb strings.Builder
	sb.WriteString("<html><body>")
	const n = 10000
	for i := 0; i < n; i++ {
		sb.WriteString(`<a href="/page">link</a>`)
	}
	sb.WriteString("</body></html>")

	links := extractLinks(strings.NewReader(sb.String()))
	if len(links) != n {
		t.Errorf("extractLinks() len = %d, want %d", len(links), n)
	}
}
