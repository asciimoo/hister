// SPDX-License-Identifier: AGPL-3.0-or-later

package chatgpt

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/asciimoo/hister/server/document"
	"github.com/asciimoo/hister/server/extractor/sdk"
)

func TestExtractsTurnMarkers(t *testing.T) {
	for _, tag := range []string{"article", "section", "div"} {
		for _, legacy := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/legacy=%v", tag, legacy), func(t *testing.T) {
				question := `<p>Explain <strong>gravity</strong>.</p>`
				followup := `<section data-testid="conversation-turn-3" data-turn="user"><p>Thanks.</p></section>`
				if legacy {
					question = `<div data-message-author-role="user">` + question + `</div>`
					followup = `<div data-message-author-role="user"><p>Thanks.</p></div>`
				}
				doc := &document.Document{
					URL: "https://chatgpt.com/c/turn-markers",
					HTML: fmt.Sprintf(`<html><body><nav>Sidebar content</nav><main>
						<%[1]s data-testid="conversation-turn-1" data-turn="user">%[2]s</%[1]s>
						<%[1]s data-testid="conversation-turn-2" data-turn="assistant">
							<h4 class="sr-only">ChatGPT said:</h4>
							<div class="markdown"><p>Gravity attracts masses.</p><pre><code>F = G * m1 * m2 / r^2</code></pre></div>
							<button>Copy answer</button><script>ignored()</script>
						</%[1]s>
						%[3]s
					</main></body></html>`, tag, question, followup),
				}
				extractor := &ChatGPTExtractor{}
				decision, err := extractor.Extract(doc).Unpack()
				if err != nil || decision != sdk.ExtractorSuccess {
					t.Fatalf("Extract returned decision %v and error %v", decision, err)
				}
				want := "User:\nExplain gravity.\n\nAssistant:\nGravity attracts masses.\nF = G * m1 * m2 / r^2\n\nUser:\nThanks."
				if doc.Text != want {
					t.Fatalf("indexed text = %q, want %q", doc.Text, want)
				}
				preview, decision, err := extractor.Preview(doc).Unpack()
				if err != nil || decision != sdk.ExtractorSuccess {
					t.Fatalf("Preview returned decision %v and error %v", decision, err)
				}
				for _, want := range []string{"<strong>gravity</strong>", "<pre><code>F = G * m1 * m2 / r^2</code></pre>"} {
					if !strings.Contains(preview.Content, want) {
						t.Errorf("preview is missing %q: %s", want, preview.Content)
					}
				}
				if strings.Count(preview.Content, "<h2>User</h2>") != 2 || strings.Count(preview.Content, "<h2>Assistant</h2>") != 1 {
					t.Errorf("preview has missing or duplicate turns: %s", preview.Content)
				}
				for _, unwanted := range []string{"Sidebar content", "ChatGPT said:", "Copy answer", "ignored()"} {
					if strings.Contains(preview.Content, unwanted) {
						t.Errorf("preview contains %q: %s", unwanted, preview.Content)
					}
				}
			})
		}
	}
}

func TestTurnMarkersRespectExplicitRolesAndVisibility(t *testing.T) {
	doc := &document.Document{
		URL: "https://chatgpt.com/c/turn-visibility",
		HTML: `<html><body>
			<section data-testid="conversation-turn-1" data-turn="user" hidden><p>Hidden user</p></section>
			<div aria-hidden="true"><section data-testid="conversation-turn-2" data-turn="assistant"><p>Hidden ancestor</p></section></div>
			<section data-testid="conversation-turn-3" data-turn="assistant"><div data-message-author-role="assistant" hidden>Hidden message</div>Turn controls</section>
			<section data-testid="conversation-turn-4" data-turn="system"><div data-message-author-role="user">Internal user</div></section>
			<section data-testid="conversation-turn-5" data-turn="assistant"><div data-message-author-role="tool">Tool output</div>Turn controls</section>
			<section data-testid="conversation-turn-6" data-turn="assistant"><div data-message-author-role="assistant"><p>First answer.</p></div><div data-message-author-role="assistant"><p>Second answer.</p></div></section>
			<section data-testid="conversation-turn-7" data-turn="user"><section data-testid="conversation-turn-nested" data-turn="user"><p>Follow up.</p></section></section>
			<section data-testid="conversation-turn-8" data-turn="assistant"><div hidden>Hidden content</div><p>Final answer.</p></section>
			<section data-testid="conversation-turn-9" data-turn="assistant" data-message-author-role="tool">Explicit tool</section>
			<section data-testid="conversation-turn-10" data-turn="assistant"><h4 class="sr-only">ChatGPT said:</h4><button>Copy</button></section>
			<div data-turn="user">Unrelated element</div>
		</body></html>`,
	}
	extractor := &ChatGPTExtractor{}
	decision, err := extractor.Extract(doc).Unpack()
	if err != nil || decision != sdk.ExtractorSuccess {
		t.Fatalf("Extract returned decision %v and error %v", decision, err)
	}
	want := "Assistant:\nFirst answer.\n\nAssistant:\nSecond answer.\n\nUser:\nFollow up.\n\nAssistant:\nFinal answer."
	if doc.Text != want {
		t.Fatalf("indexed text = %q, want %q", doc.Text, want)
	}
	preview, decision, err := extractor.Preview(doc).Unpack()
	if err != nil || decision != sdk.ExtractorSuccess {
		t.Fatalf("Preview returned decision %v and error %v", decision, err)
	}
	for _, unwanted := range []string{"Hidden", "Internal", "Tool output", "Turn controls", "Unrelated", "Explicit tool", "ChatGPT said:"} {
		if strings.Contains(preview.Content, unwanted) {
			t.Errorf("preview contains %q: %s", unwanted, preview.Content)
		}
	}
}

func TestMatchConversationURLForms(t *testing.T) {
	extractor := &ChatGPTExtractor{}
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{name: "authenticated conversation", url: "https://chatgpt.com/c/conv-123", want: true},
		{name: "public shared conversation", url: "https://chatgpt.com/share/conv-123", want: true},
		{name: "custom GPT conversation", url: "https://chatgpt.com/g/gpt-123/c/conv-123", want: true},
		{name: "custom GPT on www host", url: "https://www.chatgpt.com/g/gpt-123/c/conv-123", want: true},
		{name: "custom GPT trailing slash", url: "https://chatgpt.com/g/gpt-123/c/conv-123/", want: true},
		{name: "trailing slash", url: "https://chatgpt.com/c/conv-123/", want: true},
		{name: "chatgpt home", url: "https://chatgpt.com/", want: false},
		{name: "empty conversation path", url: "https://chatgpt.com/c/", want: false},
		{name: "extra path segment", url: "https://chatgpt.com/c/conv-123/extra", want: false},
		{name: "unrelated chatgpt page", url: "https://chatgpt.com/g/gpt-123", want: false},
		{name: "custom GPT empty GPT ID", url: "https://chatgpt.com/g//c/conv-123", want: false},
		{name: "custom GPT empty conversation ID", url: "https://chatgpt.com/g/gpt-123/c/", want: false},
		{name: "custom GPT extra path segment", url: "https://chatgpt.com/g/gpt-123/c/conv-123/extra", want: false},
		{name: "custom GPT encoded slash in GPT ID", url: "https://chatgpt.com/g/gpt%2F123/c/conv-123", want: false},
		{name: "custom GPT encoded slash in conversation ID", url: "https://chatgpt.com/g/gpt-123/c/conv%2F123", want: false},
		{name: "custom GPT dot GPT ID", url: "https://chatgpt.com/g/./c/conv-123", want: false},
		{name: "custom GPT dot conversation ID", url: "https://chatgpt.com/g/gpt-123/c/..", want: false},
		{name: "custom GPT encoded dot GPT ID", url: "https://chatgpt.com/g/%2E/c/conv-123", want: false},
		{name: "custom GPT encoded dot conversation ID", url: "https://chatgpt.com/g/gpt-123/c/%2E%2E", want: false},
		{name: "custom GPT whitespace GPT ID", url: "https://chatgpt.com/g/gpt%20123/c/conv-123", want: false},
		{name: "custom GPT control conversation ID", url: "https://chatgpt.com/g/gpt-123/c/conv%00-123", want: false},
		{name: "lookalike host", url: "https://notchatgpt.com/c/conv-123", want: false},
		{name: "wrong scheme", url: "http://chatgpt.com/c/conv-123", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := extractor.Match(&document.Document{URL: test.url})
			if got != test.want {
				t.Fatalf("Match(%q) = %v, want %v", test.url, got, test.want)
			}
		})
	}
}

func TestExtractsConversationAsOneRichDocument(t *testing.T) {
	html, err := os.ReadFile("testdata/conversation.html")
	if err != nil {
		t.Fatal(err)
	}

	for _, url := range []string{
		"https://chatgpt.com/c/conv-123",
		"https://chatgpt.com/share/conv-123",
	} {
		t.Run(url, func(t *testing.T) {
			doc := &document.Document{URL: url, HTML: string(html), UserID: 7}
			extractor := &ChatGPTExtractor{}

			decision, err := extractor.Extract(doc).Unpack()
			if err != nil {
				t.Fatalf("Extract returned an error: %v", err)
			}
			if decision != sdk.ExtractorSuccess {
				t.Fatalf("Extract decision = %v, want %v", decision, sdk.ExtractorSuccess)
			}
			if doc.SkipIndexing {
				t.Fatal("conversation was marked to skip indexing")
			}
			if len(doc.ExtraDocuments) != 0 {
				t.Fatalf("ExtraDocuments length = %d, want 0", len(doc.ExtraDocuments))
			}
			if got, want := doc.Title, "Project planning | ChatGPT"; got != want {
				t.Fatalf("title = %q, want %q", got, want)
			}
			if got, want := doc.Metadata["type"], "chatgpt"; got != want {
				t.Fatalf("metadata type = %#v, want %#v", got, want)
			}

			for _, want := range []string{
				"User:",
				"Please draft a project plan.",
				"Assistant:",
				"Project plan",
				"- Design",
				"- Build",
				"Phase | Owner",
				"Draft | Ada",
				"go test ./server/...",
				"guide",
				"Nested answer.",
			} {
				if !strings.Contains(doc.Text, want) {
					t.Errorf("indexed text is missing %q: %q", want, doc.Text)
				}
			}
			for _, unwanted := range []string{"System-only content", "Tool-only content"} {
				if strings.Contains(doc.Text, unwanted) {
					t.Errorf("indexed text contains ignored turn %q: %q", unwanted, doc.Text)
				}
			}
			if got := strings.Count(doc.Text, "Nested answer."); got != 1 {
				t.Fatalf("nested role content appears %d times in indexed text, want 1: %q", got, doc.Text)
			}
			if user, assistant := strings.Index(doc.Text, "User:"), strings.Index(doc.Text, "Assistant:"); user < 0 || assistant < 0 || user >= assistant {
				t.Fatalf("role labels are missing or out of order: %q", doc.Text)
			}

			preview, decision, err := extractor.Preview(doc).Unpack()
			if err != nil {
				t.Fatalf("Preview returned an error: %v", err)
			}
			if decision != sdk.ExtractorSuccess {
				t.Fatalf("Preview decision = %v, want %v", decision, sdk.ExtractorSuccess)
			}
			for _, want := range []string{
				"Project planning | ChatGPT",
				"<h2>User</h2>",
				"<h2>Assistant</h2>",
				"<h2>Project plan</h2>",
				`href="https://chatgpt.com/docs/start"`,
				"Nested answer.",
			} {
				if !strings.Contains(preview.Content, want) {
					t.Errorf("preview is missing %q: %s", want, preview.Content)
				}
			}
			for _, unwanted := range []string{"<script", "onclick", "javascript:", "<img", "<iframe", "generated.png"} {
				if strings.Contains(strings.ToLower(preview.Content), strings.ToLower(unwanted)) {
					t.Errorf("preview contains unsafe or unsupported content %q: %s", unwanted, preview.Content)
				}
			}
			if user, assistant := strings.Index(preview.Content, "<h2>User</h2>"), strings.Index(preview.Content, "<h2>Assistant</h2>"); user < 0 || assistant < 0 || user >= assistant {
				t.Fatalf("preview role labels are missing or out of order: %s", preview.Content)
			}
		})
	}
}

func TestExtractAbortsWithoutVisibleUserOrAssistantTurns(t *testing.T) {
	doc := &document.Document{
		URL: "https://chatgpt.com/c/empty-123",
		HTML: `<html><head><title>Empty ChatGPT shell</title></head><body>
			<div id="__next"></div>
			<article data-testid="conversation-turn-0"><div data-message-author-role="system">System content</div></article>
			<article data-testid="conversation-turn-1"><div data-message-author-role="tool">Tool content</div></article>
		</body></html>`,
	}

	decision, err := (&ChatGPTExtractor{}).Extract(doc).Unpack()
	if decision != sdk.ExtractorAbort {
		t.Fatalf("Extract decision = %v, want %v", decision, sdk.ExtractorAbort)
	}
	if err == nil {
		t.Fatal("Extract returned no abort diagnostic")
	}
	for _, want := range []string{"no visible user or assistant turns found", "browser extension", "chromedp or bidi", "signed in browser"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Extract diagnostic %q is missing %q", err, want)
		}
	}
	if doc.Text != "" {
		t.Fatalf("abort populated text: %q", doc.Text)
	}

	preview, previewDecision, previewErr := (&ChatGPTExtractor{}).Preview(doc).Unpack()
	if previewDecision != sdk.ExtractorFallback {
		t.Fatalf("Preview decision = %v, want %v", previewDecision, sdk.ExtractorFallback)
	}
	if previewErr == nil {
		t.Fatal("Preview returned no fallback diagnostic")
	}
	if preview.Content != "" {
		t.Fatalf("fallback populated preview: %q", preview.Content)
	}
}

func TestExtractFallsBackForUnmatchedURL(t *testing.T) {
	html, err := os.ReadFile("testdata/conversation.html")
	if err != nil {
		t.Fatal(err)
	}
	doc := &document.Document{
		URL:  "https://chatgpt.com/g/gpt-123",
		HTML: string(html),
	}

	decision, err := (&ChatGPTExtractor{}).Extract(doc).Unpack()
	if decision != sdk.ExtractorFallback {
		t.Fatalf("Extract decision = %v, want %v", decision, sdk.ExtractorFallback)
	}
	if err == nil {
		t.Fatal("Extract returned no fallback diagnostic")
	}
}

func TestSkipsHiddenAndNestedInternalContent(t *testing.T) {
	doc := &document.Document{
		URL: "https://chatgpt.com/c/visibility-123",
		HTML: `<html><body>
			<div hidden><article data-testid="conversation-turn-hidden-ancestor"><div data-message-author-role="user">Hidden ancestor</div></article></div>
			<article data-testid="conversation-turn-hidden-root" aria-hidden="true"><div data-message-author-role="assistant">Hidden root</div></article>
			<article data-testid="conversation-turn-hidden-role"><div data-message-author-role="assistant" hidden>Hidden role</div></article>
			<article data-testid="conversation-turn-system-parent">
				<div data-message-author-role="system"><div data-message-author-role="user">Misnested user</div></div>
			</article>
			<article data-testid="conversation-turn-visible">
				<div data-message-author-role="assistant">
					<p>Visible answer.</p>
					<div data-message-author-role="system">Nested system content.</div>
					<div data-message-author-role="assistant"><p>Nested duplicate answer.</p></div>
				</div>
			</article>
		</body></html>`,
	}

	decision, err := (&ChatGPTExtractor{}).Extract(doc).Unpack()
	if err != nil || decision != sdk.ExtractorSuccess {
		t.Fatalf("Extract returned decision %v and error %v", decision, err)
	}
	if got, want := doc.Text, "Assistant:\nVisible answer.\n\nNested duplicate answer."; got != want {
		t.Fatalf("indexed text = %q, want %q", got, want)
	}
	for _, unwanted := range []string{"Hidden ancestor", "Hidden root", "Hidden role", "Misnested user", "Nested system content"} {
		if strings.Contains(doc.Text, unwanted) {
			t.Fatalf("indexed text contains hidden or internal content %q: %q", unwanted, doc.Text)
		}
	}
	if got := strings.Count(doc.Text, "Nested duplicate answer."); got != 1 {
		t.Fatalf("nested duplicate content appears %d times, want 1: %q", got, doc.Text)
	}

	preview, previewDecision, err := (&ChatGPTExtractor{}).Preview(doc).Unpack()
	if err != nil || previewDecision != sdk.ExtractorSuccess {
		t.Fatalf("Preview returned decision %v and error %v", previewDecision, err)
	}
	for _, unwanted := range []string{"Hidden ancestor", "Hidden root", "Hidden role", "Misnested user", "Nested system content"} {
		if strings.Contains(preview.Content, unwanted) {
			t.Fatalf("preview contains hidden or internal content %q: %s", unwanted, preview.Content)
		}
	}
}

func TestExtractsPublicShareRoleNodesWithoutArticleWrappers(t *testing.T) {
	html, err := os.ReadFile("testdata/public-share.html")
	if err != nil {
		t.Fatal(err)
	}
	doc := &document.Document{
		URL:  "https://chatgpt.com/share/sleep-123",
		HTML: string(html),
	}

	decision, err := (&ChatGPTExtractor{}).Extract(doc).Unpack()
	if err != nil {
		t.Fatalf("Extract returned an error: %v", err)
	}
	if decision != sdk.ExtractorSuccess {
		t.Fatalf("Extract decision = %v, want %v", decision, sdk.ExtractorSuccess)
	}
	if got, want := doc.Title, "Sleep Optimization Discussion"; got != want {
		t.Fatalf("title = %q, want %q", got, want)
	}

	ordered := []string{
		"How can I improve my sleep quality?",
		"Keep a consistent schedule.",
		"Expose the duplicate marker only once.",
		"Limit caffeine later in the day.",
		"What about exercise timing?",
		"Exercise earlier when possible.",
		"How long should I sleep?",
		"Aim for a regular duration.",
	}
	previous := -1
	for _, want := range ordered {
		position := strings.Index(doc.Text, want)
		if position <= previous {
			t.Fatalf("indexed text is missing or out of order for %q: %q", want, doc.Text)
		}
		previous = position
	}
	if got := strings.Count(doc.Text, "Expose the duplicate marker only once."); got != 1 {
		t.Fatalf("nested same-role content appears %d times, want 1: %q", got, doc.Text)
	}
	for _, unwanted := range []string{
		"Internal system instructions must not be indexed.",
		"Hidden assistant content must not be indexed.",
	} {
		if strings.Contains(doc.Text, unwanted) {
			t.Fatalf("indexed text contains filtered content %q: %q", unwanted, doc.Text)
		}
	}

	preview, previewDecision, err := (&ChatGPTExtractor{}).Preview(doc).Unpack()
	if err != nil {
		t.Fatalf("Preview returned an error: %v", err)
	}
	if previewDecision != sdk.ExtractorSuccess {
		t.Fatalf("Preview decision = %v, want %v", previewDecision, sdk.ExtractorSuccess)
	}
	if got := strings.Count(preview.Content, "<h2>User</h2>"); got != 3 {
		t.Fatalf("preview contains %d user headings, want 3: %s", got, preview.Content)
	}
	if got := strings.Count(preview.Content, "<h2>Assistant</h2>"); got != 4 {
		t.Fatalf("preview contains %d assistant headings, want 4: %s", got, preview.Content)
	}
	for _, unwanted := range []string{
		"Internal system instructions must not be indexed.",
		"Hidden assistant content must not be indexed.",
	} {
		if strings.Contains(preview.Content, unwanted) {
			t.Fatalf("preview contains filtered content %q: %s", unwanted, preview.Content)
		}
	}
}

func TestExtractsCustomGPTRoleNodesWithoutArticleWrappers(t *testing.T) {
	html, err := os.ReadFile("testdata/public-share.html")
	if err != nil {
		t.Fatal(err)
	}
	doc := &document.Document{
		URL:  "https://chatgpt.com/g/gpt-123/c/conv-123",
		HTML: string(html),
	}

	decision, err := (&ChatGPTExtractor{}).Extract(doc).Unpack()
	if err != nil {
		t.Fatalf("Extract returned an error: %v", err)
	}
	if decision != sdk.ExtractorSuccess {
		t.Fatalf("Extract decision = %v, want %v", decision, sdk.ExtractorSuccess)
	}
	if len(doc.ExtraDocuments) != 0 {
		t.Fatalf("ExtraDocuments length = %d, want 0", len(doc.ExtraDocuments))
	}

	roleOrder := []string{"User:", "Assistant:", "User:", "Assistant:", "User:", "Assistant:"}
	position := 0
	for _, role := range roleOrder {
		found := strings.Index(doc.Text[position:], role)
		if found < 0 {
			t.Fatalf("indexed text is missing role %q after byte %d: %q", role, position, doc.Text)
		}
		position += found + len(role)
	}

	ordered := []string{
		"How can I improve my sleep quality?",
		"Keep a consistent schedule.",
		"What about exercise timing?",
		"Exercise earlier when possible.",
		"How long should I sleep?",
		"Aim for a regular duration.",
	}
	previous := -1
	for _, want := range ordered {
		position := strings.Index(doc.Text, want)
		if position <= previous {
			t.Fatalf("indexed text is missing or out of order for %q: %q", want, doc.Text)
		}
		previous = position
	}
}
