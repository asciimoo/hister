// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"reflect"
	"testing"

	"github.com/asciimoo/nogon/config"
)

func TestIndexFlagCompatibility(t *testing.T) {
	// Keep the interface from before sitemap import, including the legacy alias.
	for _, want := range []struct {
		name, kind, value, shorthand string
	}{
		{"format", "string", "text", "f"},
		{"failed-urls", "string", "", ""},
		{"label", "string", "", ""},
		{"force", "bool", "false", ""},
		{"ignore-rules", "bool", "false", ""},
		{"recursive", "bool", "false", "r"},
		{"max-depth", "int", "0", ""},
		{"max-links", "int", "0", ""},
		{"allowed-domain", "stringArray", "[]", ""},
		{"exclude-domain", "stringArray", "[]", ""},
		{"allowed-pattern", "stringArray", "[]", ""},
		{"exclude-pattern", "stringArray", "[]", ""},
		{"global", "bool", "false", ""},
		{"user-id", "uint", "0", ""},
		{"input", "string", "", ""},
		{"url-list", "string", "", ""},
		{"job-id", "string", "", ""},
		{"backend", "string", "", ""},
		{"backend-option", "stringToString", "[]", ""},
		{"proxy", "string", "", ""},
		{"header", "stringToString", "[]", ""},
		{"cookie", "stringArray", "[]", ""},
		{"no-robots", "bool", "false", ""},
		{"delay", "int", "0", ""},
		{"timeout", "int", "0", ""},
		{"user-agent", "string", "", ""},
		{"allow-sensitive", "bool", "false", ""},
	} {
		t.Run(want.name, func(t *testing.T) {
			flag := indexCmd.Flags().Lookup(want.name)
			if flag == nil {
				t.Fatal("previously supported flag is missing")
			}
			if flag.Value.Type() != want.kind || flag.DefValue != want.value || flag.Shorthand != want.shorthand {
				t.Errorf("type/default/shorthand = %s/%q/%q, want %s/%q/%q", flag.Value.Type(), flag.DefValue, flag.Shorthand, want.kind, want.value, want.shorthand)
			}
			if flag.Hidden != (want.name == "url-list") {
				t.Errorf("unexpected visibility: hidden=%v", flag.Hidden)
			}
			if want.kind == "bool" && flag.NoOptDefVal != "true" {
				t.Error("boolean flag should work without an explicit value")
			}
		})
	}
}

func TestURLInputCrawlerFlagOverrides(t *testing.T) {
	for _, newCommand := range []struct {
		name    string
		sitemap bool
	}{{name: "index"}, {name: "sitemap", sitemap: true}} {
		for _, mode := range []string{"config", "override", "clear"} {
			t.Run(newCommand.name+"/"+mode, func(t *testing.T) {
				oldCfg, oldUserAgent := cfg, UserAgent
				t.Cleanup(func() { cfg, UserAgent = oldCfg, oldUserAgent })
				cfg = config.CreateDefaultConfig()
				cfg.Crawler = config.CrawlerConfig{
					Backend: "chromedp", BackendOptions: map[string]any{"exec_path": "/configured/browser"},
					Delay: 3, Timeout: 9, Proxy: "http://configured.example:8080", UserAgent: "configured agent",
					Headers: map[string]string{"X-Keep": "configured", "X-Replace": "old"},
					Cookies: []config.CrawlerCookie{{Name: "keep", Value: "configured", Domain: "example.com", Path: "/"}},
				}
				UserAgent = cfg.Crawler.UserAgent
				want := config.CrawlerConfig{
					Backend: "chromedp", BackendOptions: map[string]any{"exec_path": "/configured/browser"},
					Delay: 3, Timeout: 9, Proxy: "http://configured.example:8080", UserAgent: "configured agent",
					Headers: map[string]string{"X-Keep": "configured", "X-Replace": "old"},
					Cookies: []config.CrawlerCookie{{Name: "keep", Value: "configured", Domain: "example.com", Path: "/"}},
				}
				var args []string
				switch mode {
				case "override":
					args = []string{
						"--backend=bidi", "--backend-option=host=localhost", "--backend-option=port=9222",
						"--delay=1", "--timeout=2", "--proxy=socks5://localhost:1080", "--user-agent=flag agent",
						"--header=X-Replace=new", "--header=X-Added=added",
						"--cookie=session=abc; Domain=example.com; Path=/private", "--cookie=other=def; Domain=example.org",
					}
					want.Backend, want.BackendOptions = "bidi", map[string]any{"host": "localhost", "port": "9222"}
					want.Delay, want.Timeout, want.Proxy, want.UserAgent = 1, 2, "socks5://localhost:1080", "flag agent"
					want.Headers["X-Replace"], want.Headers["X-Added"] = "new", "added"
					want.Cookies = append(want.Cookies,
						config.CrawlerCookie{Name: "session", Value: "abc", Domain: "example.com", Path: "/private"},
						config.CrawlerCookie{Name: "other", Value: "def", Domain: "example.org", Path: "/"},
					)
				case "clear":
					args = []string{"--delay=0", "--timeout=0", "--proxy="}
					want.Delay, want.Timeout, want.Proxy = 0, 0, ""
				}
				cmd := newIndexTestCommand()
				if newCommand.sitemap {
					cmd = sitemapTestCommand()
				}
				if err := cmd.ParseFlags(args); err != nil {
					t.Fatal(err)
				}
				applyIndexCrawlerFlags(cmd)
				if !reflect.DeepEqual(cfg.Crawler, want) {
					t.Errorf("crawler settings = %#v, want %#v", cfg.Crawler, want)
				}
				if UserAgent != want.UserAgent {
					t.Errorf("client user agent = %q, want %q", UserAgent, want.UserAgent)
				}
			})
		}
	}
}

func TestURLInputRobotsFlags(t *testing.T) {
	for _, tc := range []struct {
		name         string
		configured   bool
		args         []string
		wantDisabled bool
	}{
		{name: "default"},
		{name: "flag", args: []string{"--no-robots"}, wantDisabled: true},
		{name: "config", configured: true, wantDisabled: true},
		{name: "false flag preserves config", configured: true, args: []string{"--no-robots=false"}, wantDisabled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			oldCfg := cfg
			t.Cleanup(func() { cfg = oldCfg })
			cfg = config.CreateDefaultConfig()
			cfg.Crawler.NoRobots = tc.configured
			cmd := newIndexTestCommand()
			if err := cmd.ParseFlags(tc.args); err != nil {
				t.Fatal(err)
			}
			cache, err := indexRobotsCache(cmd)
			if err != nil {
				t.Fatal(err)
			}
			if (cache == nil) != tc.wantDisabled {
				t.Errorf("robots disabled = %v, want %v", cache == nil, tc.wantDisabled)
			}
		})
	}
}
