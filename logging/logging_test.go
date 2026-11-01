package logging

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/asciimoo/hister/config"
)

func testManager(t *testing.T, stderr io.Writer) *Manager {
	t.Helper()
	level := zerolog.GlobalLevel()
	m := New(stderr)
	t.Cleanup(func() {
		if err := m.Close(); err != nil {
			t.Error(err)
		}
		zerolog.SetGlobalLevel(level)
	})
	return m
}

func applyLogging(t *testing.T, m *Manager, c config.App) {
	t.Helper()
	change, err := m.Prepare(c)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := change.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := change.Apply(); err != nil {
		t.Fatal(err)
	}
}

func TestClearLogFileReturnsToStderr(t *testing.T) {
	var stderr bytes.Buffer
	m := testManager(t, &stderr)
	logger := zerolog.New(m)
	path := filepath.Join(t.TempDir(), "hister.log")
	applyLogging(t, m, config.App{LogFile: path, LogFormat: "json", LogLevel: "info"})
	logger.Info().Msg("file-message")
	applyLogging(t, m, config.App{LogFormat: "text", LogLevel: "info"})
	logger.Info().Msg("stderr-message")
	if got := stderr.String(); !strings.Contains(got, "stderr-message") || strings.Contains(got, "file-message") || strings.Contains(got, "\x1b[") {
		t.Fatalf("unexpected stderr output: %q", got)
	}
	raw, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(raw), "file-message") || strings.Contains(string(raw), "stderr-message") {
		t.Fatalf("unexpected file output: %s (%v)", raw, err)
	}
}

type blockedOutput struct{ entered, release chan struct{} }

func (w *blockedOutput) Write(p []byte) (int, error) {
	close(w.entered)
	<-w.release
	return len(p), nil
}

func TestApplyWaitsForActiveWrite(t *testing.T) {
	dir := t.TempDir()
	output := &blockedOutput{make(chan struct{}), make(chan struct{})}
	m := testManager(t, output)
	applyLogging(t, m, config.App{LogFormat: "json", LogLevel: "info"})
	written := make(chan struct{})
	go func() {
		defer close(written)
		logger := zerolog.New(m)
		logger.Info().Msg("pending")
	}()
	<-output.entered
	change, err := m.Prepare(config.App{LogFile: filepath.Join(dir, "next.log"), LogFormat: "json", LogLevel: "info"})
	if err != nil {
		close(output.release)
		t.Fatal(err)
	}
	defer func() {
		if err := change.Close(); err != nil {
			t.Error(err)
		}
	}()
	applied := make(chan error, 1)
	go func() { applied <- change.Apply() }()
	select {
	case err := <-applied:
		close(output.release)
		t.Fatalf("logging changed before active write finished: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(output.release)
	<-written
	if err := <-applied; err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentLoggingSurvivesOutputChanges(t *testing.T) {
	dir := t.TempDir()
	m := testManager(t, io.Discard)
	paths := []string{filepath.Join(dir, "one.log"), filepath.Join(dir, "two.log")}
	applyLogging(t, m, config.App{LogFile: paths[0], LogFormat: "json", LogLevel: "info"})
	logger := zerolog.New(m)
	derived := logger.With().Str("source", "derived").Logger()
	var writers sync.WaitGroup
	for worker := range 4 {
		writers.Go(func() {
			for n := range 100 {
				derived.Info().Msg(fmt.Sprintf("%d:%d", worker, n))
			}
		})
	}
	for n := range 20 {
		applyLogging(t, m, config.App{LogFile: paths[n%2], LogFormat: "json", LogLevel: "info"})
	}
	writers.Wait()
	seen := make(map[string]bool)
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for line := range strings.SplitSeq(strings.TrimSpace(string(raw)), "\n") {
			if line == "" {
				continue
			}
			var event struct{ Message, Source string }
			if err := json.Unmarshal([]byte(line), &event); err != nil || event.Source != "derived" || seen[event.Message] {
				t.Fatalf("corrupt or duplicate log record %q: %v", line, err)
			}
			seen[event.Message] = true
		}
	}
	if len(seen) != 400 {
		t.Fatalf("received %d records, want 400", len(seen))
	}
}

func TestDiscardedChangeRetainsOutputAndLevel(t *testing.T) {
	var stderr bytes.Buffer
	m := testManager(t, &stderr)
	applyLogging(t, m, config.App{LogFormat: "json", LogLevel: "info"})
	path := filepath.Join(t.TempDir(), "unused.log")
	change, err := m.Prepare(config.App{LogFile: path, LogFormat: "text", LogLevel: "error"})
	if err != nil {
		t.Fatal(err)
	}
	if err := change.Close(); err != nil {
		t.Fatal(err)
	}
	logger := zerolog.New(m)
	logger.Info().Msg("retained")
	if got := stderr.String(); !strings.Contains(got, `"message":"retained"`) {
		t.Fatalf("preparing or discarding changed active logging: %s", got)
	}
	if raw, err := os.ReadFile(path); err != nil || len(raw) != 0 {
		t.Fatalf("discarded output received logs: %s (%v)", raw, err)
	}
}
