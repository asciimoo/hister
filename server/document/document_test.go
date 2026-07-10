package document

import (
	"errors"
	"testing"
)

// fakeDetector records the text it was asked to detect and returns a fixed
// language, so tests can assert whether detection ran.
type fakeDetector struct {
	called   bool
	gotText  string
	language string
}

func (f *fakeDetector) DetectLanguage(s string) string {
	f.called = true
	f.gotText = s
	return f.language
}

const sampleHTML = `<html><head><title>Raw Title</title></head><body><p>raw body</p></body></html>`

func TestProcessSkipsExtractionWhenTextPresent(t *testing.T) {
	d := &Document{
		URL:   "https://example.com/a",
		HTML:  sampleHTML,
		Text:  "pre-extracted text",
		Title: "Imported Title",
	}
	ld := &fakeDetector{language: "fr"}

	extractCalled := false
	extractFn := func(*Document) error {
		extractCalled = true
		return nil
	}
	if err := d.Process(ld, extractFn); err != nil {
		t.Fatalf("Process() unexpected error: %v", err)
	}
	if extractCalled {
		t.Fatalf("extraction should be skipped when Text is already set")
	}
	if d.Text != "pre-extracted text" {
		t.Errorf("Text = %q, want %q", d.Text, "pre-extracted text")
	}
	if d.Title != "Imported Title" {
		t.Errorf("Title = %q, want %q", d.Title, "Imported Title")
	}
}

func TestProcessRunsExtractionWhenTextAbsent(t *testing.T) {
	d := &Document{
		URL:  "https://example.com/a",
		HTML: sampleHTML,
	}
	ld := &fakeDetector{language: "en"}

	extractCalled := false
	extractFn := func(doc *Document) error {
		extractCalled = true
		doc.Text = "extracted text"
		doc.Title = "Extracted Title"
		return nil
	}
	if err := d.Process(ld, extractFn); err != nil {
		t.Fatalf("Process() unexpected error: %v", err)
	}
	if !extractCalled {
		t.Fatalf("extraction should run when Text is empty")
	}
	if d.Text != "extracted text" {
		t.Errorf("Text = %q, want %q", d.Text, "extracted text")
	}
}

func TestProcessExtractionErrorPropagated(t *testing.T) {
	d := &Document{
		URL:  "https://example.com/a",
		HTML: sampleHTML,
	}
	sentinel := errors.New("boom")
	extractFn := func(*Document) error { return sentinel }
	if err := d.Process(&fakeDetector{language: "en"}, extractFn); err != sentinel {
		t.Fatalf("Process() error = %v, want %v", err, sentinel)
	}
}

func TestProcessPreservesPresetLanguage(t *testing.T) {
	d := &Document{
		URL:      "https://example.com/a",
		Text:     "some content",
		Language: "de",
	}
	ld := &fakeDetector{language: "en"}
	if err := d.Process(ld, func(*Document) error { return nil }); err != nil {
		t.Fatalf("Process() unexpected error: %v", err)
	}
	if ld.called {
		t.Fatalf("language detection should be skipped when Language is set")
	}
	if d.Language != "de" {
		t.Errorf("Language = %q, want %q", d.Language, "de")
	}
}

func TestProcessDetectsLanguageWhenAbsent(t *testing.T) {
	d := &Document{
		URL:  "https://example.com/a",
		Text: "some content",
	}
	ld := &fakeDetector{language: "en"}
	if err := d.Process(ld, func(*Document) error { return nil }); err != nil {
		t.Fatalf("Process() unexpected error: %v", err)
	}
	if !ld.called {
		t.Fatalf("language detection should run when Language is empty")
	}
	if d.Language != "en" {
		t.Errorf("Language = %q, want %q", d.Language, "en")
	}
}

func TestProcessRedetectsWhenLanguageUnknown(t *testing.T) {
	d := &Document{
		URL:      "https://example.com/a",
		Text:     "some content",
		Language: UnknownLanguage,
	}
	ld := &fakeDetector{language: "en"}
	if err := d.Process(ld, func(*Document) error { return nil }); err != nil {
		t.Fatalf("Process() unexpected error: %v", err)
	}
	if !ld.called {
		t.Fatalf("language detection should run when Language is unknown")
	}
	if d.Language != "en" {
		t.Errorf("Language = %q, want %q", d.Language, "en")
	}
}

func TestProcessPreservesPresetAdded(t *testing.T) {
	const orig = int64(1_700_000_000)
	d := &Document{
		URL:   "https://example.com/a",
		Text:  "content",
		Added: orig,
	}
	if err := d.Process(&fakeDetector{language: "en"}, func(*Document) error { return nil }); err != nil {
		t.Fatalf("Process() unexpected error: %v", err)
	}
	if d.Added != orig {
		t.Errorf("Added = %d, want %d", d.Added, orig)
	}
}

func TestProcessSetsAddedWhenZero(t *testing.T) {
	d := &Document{
		URL:  "https://example.com/a",
		Text: "content",
	}
	if err := d.Process(&fakeDetector{language: "en"}, func(*Document) error { return nil }); err != nil {
		t.Fatalf("Process() unexpected error: %v", err)
	}
	if d.Added == 0 {
		t.Fatalf("Added should be set when zero")
	}
}
