// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"net/url"
	"testing"
)

func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "lowercase host",
			input: "http://Example.COM/path",
			want:  "http://example.com/path",
		},
		{
			name:  "strip default http port",
			input: "http://example.com:80/path",
			want:  "http://example.com/path",
		},
		{
			name:  "strip default https port",
			input: "https://example.com:443/path",
			want:  "https://example.com/path",
		},
		{
			name:  "keep non-default port",
			input: "http://example.com:8080/path",
			want:  "http://example.com:8080/path",
		},
		{
			name:  "strip fragment",
			input: "https://example.com/path#section",
			want:  "https://example.com/path",
		},
		{
			name:  "sort query params",
			input: "https://example.com/?b=2&a=1",
			want:  "https://example.com/?a=1&b=2",
		},
		{
			name:  "strip utm_source",
			input: "https://example.com/?utm_source=twitter&page=1",
			want:  "https://example.com/?page=1",
		},
		{
			name:  "strip utm_medium",
			input: "https://example.com/?utm_medium=email&q=test",
			want:  "https://example.com/?q=test",
		},
		{
			name:  "strip utm_campaign",
			input: "https://example.com/?utm_campaign=summer&id=5",
			want:  "https://example.com/?id=5",
		},
		{
			name:  "strip utm_term",
			input: "https://example.com/?utm_term=shoes&cat=2",
			want:  "https://example.com/?cat=2",
		},
		{
			name:  "strip utm_content",
			input: "https://example.com/?utm_content=banner",
			want:  "https://example.com/",
		},
		{
			name:  "strip fbclid",
			input: "https://example.com/page?fbclid=abc123",
			want:  "https://example.com/page",
		},
		{
			name:  "strip gclid",
			input: "https://example.com/page?gclid=xyz",
			want:  "https://example.com/page",
		},
		{
			name:  "strip mc_cid and mc_eid",
			input: "https://example.com/?mc_cid=abc&mc_eid=def",
			want:  "https://example.com/",
		},
		{
			name:  "strip ref",
			input: "https://example.com/?ref=homepage",
			want:  "https://example.com/",
		},
		{
			name:  "strip ref_src",
			input: "https://example.com/?ref_src=twsrc",
			want:  "https://example.com/",
		},
		{
			name:  "collapse duplicate slashes in path",
			input: "https://example.com/a//b///c",
			want:  "https://example.com/a/b/c",
		},
		{
			name:  "preserve leading // in path",
			input: "https://example.com//server/file",
			want:  "https://example.com//server/file",
		},
		{
			name:  "strip all tracking params, sort rest",
			input: "https://example.com/?z=last&utm_source=foo&a=first&fbclid=bar",
			want:  "https://example.com/?a=first&z=last",
		},
		{
			name:  "no params unchanged",
			input: "https://example.com/page",
			want:  "https://example.com/page",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := url.Parse(tt.input)
			if err != nil {
				t.Fatalf("url.Parse(%q) error: %v", tt.input, err)
			}
			got := NormalizeURL(u)
			if got != tt.want {
				t.Errorf("NormalizeURL(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
