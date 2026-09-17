// SPDX-License-Identifier: AGPL-3.0-or-later

package mastodon

import (
	"fmt"
	"testing"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/document"
	"github.com/asciimoo/hister/server/extractor/sdk"
)

func TestSetConfigRejectsUnknownOptions(t *testing.T) {
	e := &MastodonExtractor{}
	err := e.SetConfig(&config.Extractor{
		Enable:  true,
		Options: map[string]any{"unknown": true},
	})
	if err == nil {
		t.Fatal("SetConfig accepted an unknown option")
	}
}

func TestExtractPreservesStatusPermalink(t *testing.T) {
	tests := []struct {
		name string
		href string
		want string
	}{
		{
			name: "relative remote status",
			href: "/@peteorrall@bsd.cafe/117269611598821921",
			want: "https://hachyderm.io/@peteorrall@bsd.cafe/117269611598821921",
		},
		{
			name: "absolute remote status",
			href: "https://hachyderm.io/@peteorrall@bsd.cafe/117269611598821921",
			want: "https://hachyderm.io/@peteorrall@bsd.cafe/117269611598821921",
		},
		{
			name: "original status URL supplied by page",
			href: "https://mastodon.bsd.cafe/@peteorrall/117269611556537474",
			want: "https://mastodon.bsd.cafe/@peteorrall/117269611556537474",
		},
		{
			name: "local status",
			href: "/@alice/123",
			want: "https://hachyderm.io/@alice/123",
		},
	}

	for _, view := range []struct {
		name      string
		class     string
		linkClass string
	}{
		{"timeline", "status", "status__relative-time"},
		{"detail", "detailed-status", "detailed-status__datetime"},
	} {
		t.Run(view.name, func(t *testing.T) {
			for _, test := range tests {
				t.Run(test.name, func(t *testing.T) {
					d := &document.Document{
						URL: "https://hachyderm.io/public/local",
						HTML: fmt.Sprintf(`<div class="%s">
							<a class="%s" href="%s"></a>
							<span class="display-name">Example author</span>
							<div class="status__content"><p>Example toot</p></div>
						</div>`, view.class, view.linkClass, test.href),
					}

					state, err := (&MastodonExtractor{}).Extract(d).Unpack()
					if err != nil {
						t.Fatalf("Extract returned an error: %v", err)
					}
					if state != sdk.ExtractorSuccess {
						t.Fatalf("Extract state = %v, want %v", state, sdk.ExtractorSuccess)
					}
					if len(d.ExtraDocuments) != 1 {
						t.Fatalf("ExtraDocuments length = %d, want 1", len(d.ExtraDocuments))
					}
					if got := d.ExtraDocuments[0].URL; got != test.want {
						t.Fatalf("toot URL = %q, want %q", got, test.want)
					}
				})
			}
		})
	}
}

func TestExtractRenderedDetailedStatus(t *testing.T) {
	d := &document.Document{
		URL: "https://mastodon.social/@Mastodon/114818205314251502",
		HTML: `<meta content='{"repository":"mastodon/mastodon"}'>
		<div class="detailed-status">
			<span class="display-name">Mastodon @Mastodon@mastodon.social</span>
			<div class="status__content"><p>Mastodon 4.4 is now available.</p></div>
			<a class="detailed-status__datetime" href="/@Mastodon/114818205314251502">
				<time datetime="2025-07-08T14:59:35.443Z">Jul 8, 2025</time>
			</a>
		</div>`,
	}

	state, err := (&MastodonExtractor{}).Extract(d).Unpack()
	if err != nil {
		t.Fatalf("Extract returned an error: %v", err)
	}
	if state != sdk.ExtractorSuccess {
		t.Fatalf("Extract state = %v, want %v", state, sdk.ExtractorSuccess)
	}
	if len(d.ExtraDocuments) != 1 {
		t.Fatalf("ExtraDocuments length = %d, want 1", len(d.ExtraDocuments))
	}
	toot := d.ExtraDocuments[0]
	if got, want := toot.URL, d.URL; got != want {
		t.Fatalf("toot URL = %q, want %q", got, want)
	}
	if got, want := toot.Text, "Mastodon 4.4 is now available."; got != want {
		t.Fatalf("toot text = %q, want %q", got, want)
	}
}

func TestExtractSkipsDocumentWhenNoTootsFound(t *testing.T) {
	d := &document.Document{
		URL:  "https://chaos.social/public/local",
		HTML: `<meta content='{"repository":"mastodon/mastodon"}'>`,
	}

	state, err := (&MastodonExtractor{}).Extract(d).Unpack()
	if err != nil {
		t.Fatalf("Extract returned an error: %v", err)
	}
	if state != sdk.ExtractorSuccess {
		t.Fatalf("Extract state = %v, want %v", state, sdk.ExtractorSuccess)
	}
	if !d.SkipIndexing {
		t.Fatal("source document was not marked to skip indexing")
	}
	if len(d.ExtraDocuments) != 0 {
		t.Fatalf("ExtraDocuments length = %d, want 0", len(d.ExtraDocuments))
	}
}
