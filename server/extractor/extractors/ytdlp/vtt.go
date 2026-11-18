package ytdlp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"
)

// fetchSubtitleText dispatches subtitle downloading based on the sub_language config:
//   - "auto": use the video's original language reported by yt-dlp
//   - single value (e.g. "de"): download subtitles in that language if available
//   - comma-separated list (e.g. "fr,en"): try each language in order, return the
//     first one for which subtitles are available
func (e *YtdlpExtractor) fetchSubtitleText(ctx context.Context, info *videoInfo) string {
	langSpec := e.subLanguage()

	if langSpec == "auto" {
		if info.Language == "" {
			return ""
		}
		return downloadSubtitleForLang(ctx, e, info, info.Language)
	}

	langs := strings.Split(langSpec, ",")
	for i := range langs {
		langs[i] = strings.TrimSpace(langs[i])
	}

	for _, lang := range langs {
		if lang == "" {
			continue
		}
		if text := downloadSubtitleForLang(ctx, e, info, lang); text != "" {
			return text
		}
	}
	return ""
}

// downloadSubtitleForLang downloads subtitles for a single language code and returns
// the plain transcript text. It prefers manual subtitles over auto-captions.
func downloadSubtitleForLang(ctx context.Context, e *YtdlpExtractor, info *videoInfo, lang string) string {
	if ctx.Err() != nil {
		return ""
	}
	// Check whether any subtitles are available at all.
	hasManual := len(info.Subtitles[lang]) > 0
	hasAuto := len(info.AutomaticCaptions[lang]) > 0
	if !hasManual && !hasAuto {
		return ""
	}

	dir, err := os.MkdirTemp("", "nogon-subs-*")
	if err != nil {
		return ""
	}
	defer os.RemoveAll(dir) //nolint:errcheck // best-effort cleanup

	outTpl := filepath.Join(dir, "sub")
	args := []string{
		"--skip-download",
		"--no-playlist",
		"--no-warnings",
		"--sub-lang", lang,
		"--convert-subs", "vtt",
		"-o", outTpl,
	}
	if hasManual {
		args = append(args, "--write-sub")
	} else {
		args = append(args, "--write-auto-sub")
	}
	args = append(args, e.cookieArgs()...)
	args = append(args, info.WebpageURL)

	err = e.runJob(ctx, func() error {
		ctx, cancel := context.WithTimeout(ctx, e.timeout())
		defer cancel()

		// #nosec G204 -- binary path and args are admin-configured, not user input.
		cmd := exec.CommandContext(ctx, e.binary(), args...)
		return cmd.Run()
	})
	if err != nil {
		return ""
	}

	// yt-dlp writes the subtitle file as sub.<lang>.vtt in the temp dir.
	matches, _ := filepath.Glob(filepath.Join(dir, "*.vtt"))
	if len(matches) == 0 {
		return ""
	}

	data, err := os.ReadFile(matches[0])
	if err != nil {
		return ""
	}

	return parseVTT(string(data))
}

// parseVTT extracts plain text lines from WebVTT content,
// stripping headers, timestamps, and deduplicating repeated lines.
func parseVTT(raw string) string {
	var lines []string
	seen := make(map[string]struct{})

	for line := range strings.SplitSeq(raw, "\n") {
		line = strings.TrimSpace(line)
		// Skip empty lines, WEBVTT header, NOTE blocks, timestamp lines,
		// and numeric cue identifiers.
		if line == "" || strings.HasPrefix(line, "WEBVTT") ||
			strings.HasPrefix(line, "NOTE") || strings.HasPrefix(line, "Kind:") ||
			strings.HasPrefix(line, "Language:") || strings.Contains(line, " --> ") ||
			isNumeric(line) {
			continue
		}
		// Strip VTT tags like <c>, </c>, <00:00:01.234>, etc.
		clean := stripVTTTags(line)
		clean = strings.TrimSpace(clean)
		if clean == "" {
			continue
		}
		if _, dup := seen[clean]; !dup {
			seen[clean] = struct{}{}
			lines = append(lines, clean)
		}
	}

	return strings.Join(lines, " ")
}

// isNumeric reports whether s consists entirely of digits (WebVTT cue identifiers).
func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// stripVTTTags removes VTT/HTML-style tags from text.
func stripVTTTags(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			continue
		}
		if !inTag {
			b.WriteRune(r)
		}
	}
	return b.String()
}
