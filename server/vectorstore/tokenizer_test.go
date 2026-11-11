// SPDX-License-Identifier: AGPL-3.0-or-later

package vectorstore

import (
	"strings"
	"testing"
	"unicode/utf8"

	_ "github.com/mattn/go-sqlite3" // provides sqlite3 symbols needed by sqlitevec
)

// ---- splitSentences tests ----

func TestSplitSentences_BasicTwoSentences(t *testing.T) {
	got := splitSentences("Hello world. This is a test.")
	want := []string{"Hello world.", "This is a test."}
	assertSentences(t, got, want)
}

func TestSplitSentences_QuestionMark(t *testing.T) {
	got := splitSentences("Is this a test? Yes it is.")
	want := []string{"Is this a test?", "Yes it is."}
	assertSentences(t, got, want)
}

func TestSplitSentences_Exclamation(t *testing.T) {
	got := splitSentences("Wow! That's great.")
	want := []string{"Wow!", "That's great."}
	assertSentences(t, got, want)
}

func TestSplitSentences_MultipleExclamations(t *testing.T) {
	got := splitSentences("Amazing!!! Really stunning.")
	want := []string{"Amazing!!!", "Really stunning."}
	assertSentences(t, got, want)
}

func TestSplitSentences_ParagraphBreak(t *testing.T) {
	got := splitSentences("First paragraph.\n\nSecond paragraph.")
	want := []string{"First paragraph.", "Second paragraph."}
	assertSentences(t, got, want)
}

func TestSplitSentences_MultipleNewlines(t *testing.T) {
	got := splitSentences("First paragraph.\n\n\n\nSecond paragraph.")
	want := []string{"First paragraph.", "Second paragraph."}
	assertSentences(t, got, want)
}

func TestSplitSentences_CJK(t *testing.T) {
	got := splitSentences("这是一个测试。另一句话。")
	want := []string{"这是一个测试。", "另一句话。"}
	assertSentences(t, got, want)
}

func TestSplitSentences_SingleSentence(t *testing.T) {
	got := splitSentences("Just one sentence.")
	want := []string{"Just one sentence."}
	assertSentences(t, got, want)
}

func TestSplitSentences_NoBoundary(t *testing.T) {
	// No punctuation → single sentence
	got := splitSentences("just text without a boundary anywhere")
	want := []string{"just text without a boundary anywhere"}
	assertSentences(t, got, want)
}

func TestSplitSentences_Empty(t *testing.T) {
	got := splitSentences("")
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

func TestSplitSentences_ClosingQuoteAfterTerminator(t *testing.T) {
	// Closing quote after ! before next word
	got := splitSentences(`He said "Stop!" Then left.`)
	want := []string{`He said "Stop!"`, "Then left."}
	assertSentences(t, got, want)
}

// ---- ChunkText tests ----

func TestChunkText_Empty(t *testing.T) {
	chunks := ChunkText("", 10, 2)
	if len(chunks) != 0 {
		t.Errorf("expected no chunks for empty input, got %d", len(chunks))
	}
}

func TestChunkText_FitsInOneChunk(t *testing.T) {
	text := "Short text."
	chunks := ChunkText(text, 100, 10)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Text != text {
		t.Errorf("expected verbatim text, got %q", chunks[0].Text)
	}
}

func TestChunkTextBoundsUnderestimatedText(t *testing.T) {
	for name, text := range map[string]string{
		"number":     strings.Repeat("1234567890", 2000),
		"unicode":    strings.Repeat("éअ𝒜", 2000),
		"whitespace": "start" + strings.Repeat(" ", 2000) + "end",
	} {
		t.Run(name, func(t *testing.T) {
			chunks := ChunkText(text, 16, 0)
			var joined strings.Builder
			for _, chunk := range chunks {
				if len(chunk.Text) > 128 {
					t.Fatalf("chunk has %d bytes, want at most 128", len(chunk.Text))
				}
				if !utf8.ValidString(chunk.Text) {
					t.Fatal("chunk split a UTF-8 character")
				}
				joined.WriteString(chunk.Text)
			}
			if joined.String() != text {
				t.Fatal("chunking lost or duplicated text without overlap")
			}
		})
	}
}

func TestChunkTextOverlapsUnderestimatedText(t *testing.T) {
	text := strings.Repeat("1234567890", 40)
	chunks := ChunkText(text, 16, 4)
	if len(chunks) < 2 {
		t.Fatal("expected multiple chunks")
	}
	joined := chunks[0].Text
	for i := 1; i < len(chunks); i++ {
		previous := chunks[i-1].Text
		if len(chunks[i].Text) <= 32 || chunks[i].Text[:32] != previous[len(previous)-32:] {
			t.Fatalf("chunk %d does not overlap the previous chunk", i)
		}
		joined += chunks[i].Text[32:]
	}
	if joined != text {
		t.Fatal("overlapping chunks lost or duplicated content")
	}
}

func TestChunkText_SentenceBoundaryChunking(t *testing.T) {
	// 5 short sentences, maxTokens=6 (each sentence ~3 tokens: Word word .)
	sentences := []string{
		"Go left.",
		"Turn right.",
		"Stop here.",
		"Look up.",
		"Move forward.",
	}
	text := strings.Join(sentences, " ")
	chunks := ChunkText(text, 6, 0)

	// Must produce more than 1 chunk (text is longer than maxTokens).
	if len(chunks) < 2 {
		t.Errorf("expected multiple chunks, got %d: %v", len(chunks), chunks)
	}

	// Every chunk must contain complete sentences (no mid-sentence cuts).
	for _, c := range chunks {
		if !hasSentenceBoundary(c.Text) {
			t.Errorf("chunk %q looks like it was cut mid-sentence", c.Text)
		}
	}

	// All sentences must be covered across the chunks (no data loss).
	covered := make(map[string]bool)
	for _, c := range chunks {
		for _, s := range splitSentences(c.Text) {
			covered[s] = true
		}
	}
	for _, s := range sentences {
		if !covered[s] {
			t.Errorf("sentence %q missing from chunks", s)
		}
	}
}

func TestChunkText_SentenceBoundaryOverlap(t *testing.T) {
	text := "First one. Second two. Third three. Fourth four."
	chunks := ChunkText(text, 6, 3)
	if len(chunks) != 3 {
		t.Fatalf("expected 3 overlapping chunks, got %d: %#v", len(chunks), chunks)
	}
	want := []string{
		"First one. Second two.",
		"Second two. Third three.",
		"Third three. Fourth four.",
	}
	for i := range want {
		if chunks[i].Text != want[i] {
			t.Errorf("chunk %d: got %q, want %q", i, chunks[i].Text, want[i])
		}
	}
}

func TestSplitStructuralBlocks(t *testing.T) {
	text := strings.Join([]string{
		"# Heading",
		"",
		"A prose paragraph",
		"continued on another line.",
		"",
		"- first item",
		"- second item",
		"",
		"```go",
		"fmt.Println(\"hello\")",
		"```",
		"",
		"    indented()",
		"    code()",
	}, "\n")
	want := []string{
		"# Heading",
		"A prose paragraph\ncontinued on another line.",
		"- first item",
		"- second item",
		"```go\nfmt.Println(\"hello\")\n```",
		"    indented()\n    code()",
	}
	got := splitStructuralBlocks(text)
	assertSentences(t, got, want)
}

func TestChunkText_PreservesStructuralBoundaries(t *testing.T) {
	text := strings.Join([]string{
		"# Search",
		"",
		"Semantic retrieval finds related passages.",
		"",
		"- preserve list items",
		"- preserve code blocks",
		"",
		"```go",
		"result := search(query)",
		"```",
	}, "\n")
	chunks := ChunkText(text, 18, 4)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple structural chunks, got %d", len(chunks))
	}
	for _, expected := range []string{
		"# Search",
		"Semantic retrieval finds related passages.",
		"- preserve list items",
		"- preserve code blocks",
		"```go\nresult := search(query)\n```",
	} {
		found := false
		for _, chunk := range chunks {
			if strings.Contains(chunk.Text, expected) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("structural unit %q was split or lost: %#v", expected, chunks)
		}
	}
}

func TestChunkByStructureDoesNotEmitOverlapOnlyChunk(t *testing.T) {
	units := []chunkUnit{
		{text: "first", tokenCount: 4, block: 0},
		{text: "second", tokenCount: 4, block: 1},
		{text: "large next block", tokenCount: 10, block: 2},
	}
	chunks := chunkByStructure(units, 10, 4)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks without an overlap-only chunk, got %d: %#v", len(chunks), chunks)
	}
	if chunks[1].Text != "large next block" {
		t.Fatalf("unexpected second chunk: %q", chunks[1].Text)
	}
}

func TestChunkText_NaiveFallbackWhenNoSentences(t *testing.T) {
	// Text with no sentence boundaries: continuous words without periods.
	words := make([]string, 30)
	for i := range words {
		words[i] = "word"
	}
	text := strings.Join(words, " ") // 30 tokens, no sentence boundary
	chunks := ChunkText(text, 10, 2)

	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks from naive fallback, got %d", len(chunks))
	}
	// Each chunk must be ≤ maxTokens.
	for _, c := range chunks {
		if c.TokenCount > 10 {
			t.Errorf("chunk exceeds maxTokens: %d tokens in %q", c.TokenCount, c.Text)
		}
	}
}

func TestChunkText_OversizedSingleSentenceFallback(t *testing.T) {
	// Build a single very long sentence (no period until the end).
	words := make([]string, 20)
	for i := range words {
		words[i] = "word"
	}
	// End with a period so splitSentences returns exactly 1 sentence.
	text := strings.Join(words, " ") + "."
	// maxTokens = 5: the single sentence (~21 tokens) is too large.
	chunks := ChunkText(text, 5, 1)
	if len(chunks) < 2 {
		t.Fatalf("expected naive chunking for oversized single sentence, got %d chunks", len(chunks))
	}
	for _, c := range chunks {
		if c.TokenCount > 5 {
			t.Errorf("chunk exceeds maxTokens: %d tokens", c.TokenCount)
		}
	}
}

func TestChunkText_TokenCountAccurate(t *testing.T) {
	text := "Hello world. This is a test. Another sentence here."
	chunks := ChunkText(text, 100, 0)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	expected := len(tokenize(text))
	if chunks[0].TokenCount != expected {
		t.Errorf("token count: got %d, want %d", chunks[0].TokenCount, expected)
	}
}

func TestChunkText_CJKSentenceChunking(t *testing.T) {
	// CJK text with 3 clear sentences (each 5 CJK chars + terminator = ~6 tokens).
	text := "这是第一句话。这是第二句话。这是第三句话。"
	chunks := ChunkText(text, 7, 0)
	toks := len(tokenize(text))
	if toks <= 7 {
		t.Skip("text fits in one chunk, nothing to test for splitting")
	}
	if len(chunks) < 2 {
		t.Errorf("expected sentence-level splitting of CJK text, got %d chunk(s)", len(chunks))
	}
}

// helpers

func assertSentences(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("sentence count: got %d %v, want %d %v", len(got), got, len(want), want)
		return
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sentence[%d]: got %q, want %q", i, got[i], want[i])
		}
	}
}

// hasSentenceBoundary reports whether s ends with typical sentence-ending punctuation.
func hasSentenceBoundary(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	last := rune(s[len(s)-1])
	return last == '.' || last == '!' || last == '?' ||
		last == '。' || last == '！' || last == '？'
}
