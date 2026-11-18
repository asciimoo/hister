// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/asciimoo/nogon/config"
)

func TestParseProxyURL(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "empty"},
		{name: "http", raw: "http://proxy.example:8080", want: "http://proxy.example:8080"},
		{name: "socks5", raw: "socks5://proxy.example:1080", want: "socks5://proxy.example:1080"},
		{name: "case insensitive scheme", raw: "HTTP://proxy.example:8080", want: "http://proxy.example:8080"},
		{name: "unsupported scheme", raw: "https://proxy.example:8443", wantErr: true},
		{name: "missing host", raw: "http://", wantErr: true},
		{name: "path", raw: "http://proxy.example/path", wantErr: true},
		{name: "query", raw: "http://proxy.example?x=1", wantErr: true},
		{name: "credentials", raw: "http://user:secret@proxy.example:8080", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proxyURL, err := parseProxyURL(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatal("parseProxyURL() error = nil")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tt.want == "" {
				if proxyURL != nil {
					t.Fatalf("parseProxyURL() = %q, want nil", proxyURL)
				}
				return
			}
			if got := proxyURL.String(); got != tt.want {
				t.Fatalf("parseProxyURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHTTPFetcherUsesConfiguredProxy(t *testing.T) {
	fetcher, err := newHTTPFetcher(&config.CrawlerConfig{
		Proxy: "http://proxy.example:8080",
	})
	if err != nil {
		t.Fatal(err)
	}

	transport, ok := fetcher.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", fetcher.client.Transport)
	}
	req, err := http.NewRequest(http.MethodGet, "https://target.example", nil)
	if err != nil {
		t.Fatal(err)
	}
	proxyURL, err := transport.Proxy(req)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := proxyURL.String(), "http://proxy.example:8080"; got != want {
		t.Fatalf("proxy URL = %q, want %q", got, want)
	}
}

func TestEveryBackendRejectsInvalidProxy(t *testing.T) {
	tests := []struct {
		name string
		new  func(*config.CrawlerConfig) error
	}{
		{
			name: "http",
			new: func(cfg *config.CrawlerConfig) error {
				_, err := newHTTPFetcher(cfg)
				return err
			},
		},
		{
			name: "chromedp",
			new: func(cfg *config.CrawlerConfig) error {
				_, err := newChromedpFetcher(cfg)
				return err
			},
		},
		{
			name: "bidi",
			new: func(cfg *config.CrawlerConfig) error {
				_, err := newBidiFetcher(cfg)
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.new(&config.CrawlerConfig{Proxy: "ftp://proxy.example"}); err == nil {
				t.Fatal("backend accepted an invalid proxy")
			}
		})
	}
}

func TestBidiProxyCapability(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want map[string]any
	}{
		{
			name: "http",
			raw:  "http://proxy.example:8080",
			want: map[string]any{
				"proxyType": "manual",
				"httpProxy": "proxy.example:8080",
				"sslProxy":  "proxy.example:8080",
			},
		},
		{
			name: "socks5",
			raw:  "socks5://proxy.example:1080",
			want: map[string]any{
				"proxyType":    "manual",
				"socksProxy":   "proxy.example:1080",
				"socksVersion": 5,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proxyURL, err := parseProxyURL(tt.raw)
			if err != nil {
				t.Fatal(err)
			}
			if got := bidiProxyCapability(proxyURL); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("bidiProxyCapability() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestRobotsCacheUsesConfiguredProxy(t *testing.T) {
	cache, err := NewRobotsCacheWithProxy("Nogon", "socks5://proxy.example:1080")
	if err != nil {
		t.Fatal(err)
	}
	transport, ok := cache.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", cache.client.Transport)
	}
	req, err := http.NewRequest(http.MethodGet, "https://target.example/robots.txt", nil)
	if err != nil {
		t.Fatal(err)
	}
	proxyURL, err := transport.Proxy(req)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := proxyURL.String(), "socks5://proxy.example:1080"; got != want {
		t.Fatalf("proxy URL = %q, want %q", got, want)
	}
}
