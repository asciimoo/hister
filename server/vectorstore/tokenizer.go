// SPDX-License-Identifier: AGPL-3.0-or-later

package vectorstore

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// The word-based token estimate can undercount numbers, identifiers, and
// whitespace. Bound their size as well; endpoint feedback still handles
// differences between this estimate and the model's tokenizer.
const maxBytesPerToken = 8

// TextChunk represents a chunk of text produced by the tokenizer.
type TextChunk struct {
	Text       string
	TokenCount int
}

// isCJKIdeograph returns true for characters from CJK scripts that do not use
// whitespace word separators: CJK Unified Ideographs, Hiragana, Katakana, and
// Hangul Syllables. Each such character is treated as a standalone token so
// that ChunkText produces sensible boundaries for Chinese, Japanese, and Korean
// text.
func isCJKIdeograph(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r)
}

// tokenize splits text into tokens on whitespace and punctuation boundaries.
// Each contiguous run of letters/digits is a token, except CJK ideographs
// which are emitted as individual tokens because those scripts do not use
// whitespace separators. This is a simple approximation that does not match
// any specific model's BPE tokenizer, but is good enough for determining
// chunk boundaries.
func tokenize(text string) []string {
	var tokens []string
	var cur strings.Builder
	for _, r := range text {
		if isCJKIdeograph(r) {
			if cur.Len() > 0 {
				tokens = append(tokens, cur.String())
				cur.Reset()
			}
			tokens = append(tokens, string(r))
		} else if unicode.IsLetter(r) || unicode.IsDigit(r) {
			cur.WriteRune(r)
		} else {
			if cur.Len() > 0 {
				tokens = append(tokens, cur.String())
				cur.Reset()
			}
			if !unicode.IsSpace(r) {
				tokens = append(tokens, string(r))
			}
		}
	}
	if cur.Len() > 0 {
		tokens = append(tokens, cur.String())
	}
	return tokens
}

// cjkTerminators is the set of CJK sentence-ending punctuation characters.
var cjkTerminators = map[rune]struct{}{
	'。': {}, '！': {}, '？': {}, '…': {}, '；': {},
	'｡': {}, '︕': {}, '︖': {},
}

// isClosingPunct reports whether r is a closing bracket or quote that may
// appear directly after a sentence-ending period/!/?.
func isClosingPunct(r rune) bool {
	return r == ')' || r == ']' || r == '}' ||
		r == '"' || r == '\'' || r == '\u201D' || r == '\u2019' ||
		r == '»' || r == '›'
}

// splitSentences splits text into sentences using CJK terminators and Western
// sentence-ending punctuation (. ! ?) as boundaries. Paragraph breaks (\n\n)
// are always boundaries. Repeated terminators and trailing closing punctuation
// are consumed as part of the same sentence.
func splitSentences(text string) []string {
	runes := []rune(text)
	n := len(runes)
	var sentences []string
	sentStart := 0
	i := 0

	for i < n {
		r := runes[i]

		// Paragraph break: two or more consecutive newlines.
		if r == '\n' && i+1 < n && runes[i+1] == '\n' {
			if s := strings.TrimSpace(string(runes[sentStart:i])); s != "" {
				sentences = append(sentences, s)
			}
			for i < n && runes[i] == '\n' {
				i++
			}
			sentStart = i
			continue
		}

		// CJK terminators: always a sentence boundary.
		if _, ok := cjkTerminators[r]; ok {
			if s := strings.TrimSpace(string(runes[sentStart : i+1])); s != "" {
				sentences = append(sentences, s)
			}
			i++
			for i < n && unicode.IsSpace(runes[i]) {
				i++
			}
			sentStart = i
			continue
		}

		// Western terminators: . ! ?
		if r == '.' || r == '!' || r == '?' {
			j := i + 1
			// Consume any run of repeated terminators (e.g. !!, ?!, ...).
			for j < n && (runes[j] == '.' || runes[j] == '!' || runes[j] == '?') {
				j++
			}
			// Consume closing punctuation that may follow (e.g. '."', ')').
			for j < n && isClosingPunct(runes[j]) {
				j++
			}
			if s := strings.TrimSpace(string(runes[sentStart:j])); s != "" {
				sentences = append(sentences, s)
			}
			for j < n && unicode.IsSpace(runes[j]) {
				j++
			}
			sentStart = j
			i = j
			continue
		}

		i++
	}

	// Emit any remaining text as the final sentence.
	if sentStart < n {
		if s := strings.TrimSpace(string(runes[sentStart:])); s != "" {
			sentences = append(sentences, s)
		}
	}
	return sentences
}

// naiveChunkTokens splits a pre-tokenized slice into overlapping token-window
// chunks. This is the fallback path used when sentence boundaries are absent.
func naiveChunkTokens(tokens []string, maxTokens, overlap int) []TextChunk {
	if len(tokens) == 0 {
		return nil
	}
	step := maxTokens - overlap
	if step <= 0 {
		step = 1
	}
	var chunks []TextChunk
	for start := 0; start < len(tokens); start += step {
		end := min(start+maxTokens, len(tokens))
		chunkTokens := tokens[start:end]
		chunks = append(chunks, TextChunk{
			Text:       strings.Join(chunkTokens, " "),
			TokenCount: len(chunkTokens),
		})
		if end == len(tokens) {
			break
		}
	}
	return chunks
}

func isFenceLine(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	for _, marker := range []string{"```", "~~~"} {
		if strings.HasPrefix(trimmed, marker) {
			return marker, true
		}
	}
	return "", false
}

func isHeadingLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < 2 || trimmed[0] != '#' {
		return false
	}
	i := 0
	for i < len(trimmed) && trimmed[i] == '#' {
		i++
	}
	return i <= 6 && i < len(trimmed) && unicode.IsSpace(rune(trimmed[i]))
}

func isListLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	for _, prefix := range []string{"- ", "* ", "+ ", "> "} {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	i := 0
	for i < len(trimmed) && unicode.IsDigit(rune(trimmed[i])) {
		i++
	}
	return i > 0 && i+1 < len(trimmed) &&
		(trimmed[i] == '.' || trimmed[i] == ')') && unicode.IsSpace(rune(trimmed[i+1]))
}

func isIndentedCodeLine(line string) bool {
	return strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "    ")
}

// splitStructuralBlocks separates text at paragraph and lightweight markup
// boundaries. Fenced code is kept together, as are consecutive indented code
// lines. Headings and list items become independent blocks so chunk boundaries
// do not split their contents.
func splitStructuralBlocks(text string) []string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var blocks []string
	var current []string
	inFence := false
	fenceMarker := ""
	currentIndented := false

	flush := func() {
		block := strings.Join(current, "\n")
		if currentIndented || inFence {
			block = strings.Trim(block, "\r\n")
		} else {
			block = strings.TrimSpace(block)
		}
		if block != "" {
			blocks = append(blocks, block)
		}
		current = nil
		currentIndented = false
	}

	for _, line := range lines {
		if marker, fence := isFenceLine(line); fence {
			if !inFence {
				flush()
				inFence = true
				fenceMarker = marker
				current = append(current, line)
				continue
			}
			current = append(current, line)
			if marker == fenceMarker {
				flush()
				inFence = false
				fenceMarker = ""
			}
			continue
		}
		if inFence {
			current = append(current, line)
			continue
		}
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		if isHeadingLine(line) || isListLine(line) {
			flush()
			blocks = append(blocks, strings.TrimSpace(line))
			continue
		}
		if isIndentedCodeLine(line) {
			if len(current) > 0 && !currentIndented {
				flush()
			}
			currentIndented = true
			current = append(current, line)
			continue
		}
		if currentIndented {
			flush()
		}
		current = append(current, line)
	}
	flush()
	return blocks
}

type chunkUnit struct {
	text       string
	tokenCount int
	block      int
}

func chunkUnits(text string, maxTokens int) []chunkUnit {
	blocks := splitStructuralBlocks(text)
	units := make([]chunkUnit, 0, len(blocks))
	for blockIdx, block := range blocks {
		count := len(tokenize(block))
		if count <= maxTokens {
			units = append(units, chunkUnit{text: block, tokenCount: count, block: blockIdx})
			continue
		}
		sentences := splitSentences(block)
		if len(sentences) <= 1 {
			units = append(units, chunkUnit{text: block, tokenCount: count, block: blockIdx})
			continue
		}
		for _, sentence := range sentences {
			units = append(units, chunkUnit{
				text:       sentence,
				tokenCount: len(tokenize(sentence)),
				block:      blockIdx,
			})
		}
	}
	return units
}

func joinChunkUnits(units []chunkUnit) string {
	var b strings.Builder
	for i, unit := range units {
		if i > 0 {
			if unit.block == units[i-1].block {
				b.WriteByte(' ')
			} else {
				b.WriteString("\n\n")
			}
		}
		b.WriteString(unit.text)
	}
	return b.String()
}

// chunkByStructure packs complete structural or sentence units into chunks.
// Overlap reuses complete trailing units when they fit within the configured
// allowance. A unit larger than maxTokens uses the token window fallback.
func chunkByStructure(units []chunkUnit, maxTokens, overlap int) []TextChunk {
	var chunks []TextChunk
	for start := 0; start < len(units); {
		if units[start].tokenCount > maxTokens {
			chunks = append(chunks, naiveChunkTokens(tokenize(units[start].text), maxTokens, overlap)...)
			start++
			continue
		}

		total := 0
		end := start
		for end < len(units) && total+units[end].tokenCount <= maxTokens {
			total += units[end].tokenCount
			end++
		}
		chunks = append(chunks, TextChunk{
			Text:       joinChunkUnits(units[start:end]),
			TokenCount: total,
		})
		if end == len(units) {
			break
		}

		next := end
		overlapTokens := 0
		for candidate := end - 1; candidate > start; candidate-- {
			if overlapTokens+units[candidate].tokenCount > overlap {
				break
			}
			if overlapTokens+units[candidate].tokenCount+units[end].tokenCount > maxTokens {
				break
			}
			overlapTokens += units[candidate].tokenCount
			next = candidate
		}
		start = next
	}
	return chunks
}

// ChunkText splits text into chunks of at most maxTokens tokens, preferring
// paragraphs, headings, list items, code blocks, and sentence boundaries.
// Complete trailing units are reused to provide overlap without cutting those
// structures. Oversized indivisible units use the token window fallback.
// A separate byte limit bounds text that the token estimate undercounts.
func ChunkText(text string, maxTokens, overlap int) []TextChunk {
	if maxTokens <= 0 {
		maxTokens = 2048
	}
	if overlap < 0 {
		overlap = 0
	}
	if overlap >= maxTokens {
		overlap = maxTokens / 10
	}

	tokens := tokenize(text)
	if len(tokens) == 0 {
		return nil
	}
	var chunks []TextChunk
	if len(tokens) <= maxTokens {
		chunks = []TextChunk{{Text: text, TokenCount: len(tokens)}}
	} else {
		chunks = chunkByStructure(chunkUnits(text, maxTokens), maxTokens, overlap)
	}

	return boundChunkBytes(chunks, maxTokens, overlap)
}

func boundChunkBytes(chunks []TextChunk, maxTokens, overlap int) []TextChunk {
	var bounded []TextChunk
	for _, chunk := range chunks {
		// Divide first so even a very large configured limit cannot overflow.
		if (len(chunk.Text)-1)/maxBytesPerToken < maxTokens {
			bounded = append(bounded, chunk)
			continue
		}
		maxBytes := maxTokens * maxBytesPerToken
		overlapBytes := overlap * maxBytesPerToken
		for start := 0; start < len(chunk.Text); {
			end := start + min(maxBytes, len(chunk.Text)-start)
			for end < len(chunk.Text) && !utf8.RuneStart(chunk.Text[end]) {
				end--
			}
			text := chunk.Text[start:end]
			bounded = append(bounded, TextChunk{Text: text, TokenCount: len(tokenize(text))})
			if end == len(chunk.Text) {
				break
			}
			start = end - overlapBytes
			for !utf8.RuneStart(chunk.Text[start]) {
				start++
			}
		}
	}
	return bounded
}
