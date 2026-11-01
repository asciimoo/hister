package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/asciimoo/hister/logging"
)

func useReloadLogger(t *testing.T) {
	t.Helper()
	previous, level := log.Logger, zerolog.GlobalLevel()
	log.Logger = log.Logger.Output(logging.Default)
	t.Cleanup(func() {
		if err := logging.Default.Close(); err != nil {
			t.Error(err)
		}
		log.Logger = previous
		zerolog.SetGlobalLevel(level)
	})
}

func readLog(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestFullReloadLogFileAndFormat(t *testing.T) {
	dir := t.TempDir()
	useReloadLogger(t)
	c, write, request := startRuntimeTestServer(t)
	c.App.LogFile = filepath.Join(dir, "hister.log")
	c.App.LogFormat = "json"
	write()
	if status, body := request(c.Server.BaseURL, "/api/reload", "secret", ""); status != http.StatusOK {
		t.Fatalf("configure logging: %d %s", status, body)
	}
	log.Info().Msg("before-rotation")
	for line := range strings.SplitSeq(strings.TrimSpace(readLog(t, c.App.LogFile)), "\n") {
		if !json.Valid([]byte(line)) {
			t.Fatalf("expected JSON log record: %s", line)
		}
	}
	t.Run("rename rotation", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Windows does not allow renaming an open log file")
		}
		rotated := c.App.LogFile + ".1"
		if err := os.Rename(c.App.LogFile, rotated); err != nil {
			t.Fatal(err)
		}
		if status, body := request(c.Server.BaseURL, "/api/reload", "secret", ""); status != http.StatusOK {
			t.Fatalf("reopen rotated log: %d %s", status, body)
		}
		log.Info().Msg("after-rotation")
		if raw := readLog(t, rotated); !strings.Contains(raw, "before-rotation") || strings.Contains(raw, "after-rotation") {
			t.Fatalf("unexpected rotated log: %s", raw)
		}
		if raw := readLog(t, c.App.LogFile); !strings.Contains(raw, "after-rotation") || strings.Contains(raw, "before-rotation") {
			t.Fatalf("log path was not reopened: %s", raw)
		}
	})
	previous := c.App.LogFile
	c.App.LogFile = filepath.Join(dir, "replacement.log")
	c.App.LogFormat = "text"
	write()
	if status, body := request(c.Server.BaseURL, "/api/reload", "secret", ""); status != http.StatusOK {
		t.Fatalf("switch file and format: %d %s", status, body)
	}
	log.Info().Msg("text-output")
	if raw := readLog(t, c.App.LogFile); !strings.Contains(raw, "text-output") || strings.Contains(raw, `"message"`) || strings.Contains(raw, "\x1b[") {
		t.Fatalf("expected plain text file logging: %s", raw)
	}
	if raw := readLog(t, previous); strings.Contains(raw, "text-output") {
		t.Fatalf("still writing to old log: %s", raw)
	}
}

func TestFullReloadRetainsLoggingOnFailure(t *testing.T) {
	for _, failure := range []string{"log file", "database"} {
		t.Run(failure, func(t *testing.T) {
			dir := t.TempDir()
			useReloadLogger(t)
			c, write, request := startRuntimeTestServer(t)
			original := filepath.Join(dir, "original.log")
			c.App.LogFile, c.App.LogFormat = original, "json"
			write()
			if status, body := request(c.Server.BaseURL, "/api/reload", "secret", ""); status != http.StatusOK {
				t.Fatalf("initial logging: %d %s", status, body)
			}
			c.App.Title = "Must not apply"
			c.App.LogLevel = "error"
			c.App.LogFile = filepath.Join(dir, "candidate.log")
			want := http.StatusInternalServerError
			if failure == "log file" {
				c.App.LogFile = filepath.Join(dir, "missing", "hister.log")
				want = http.StatusBadRequest
			} else {
				c.Server.Database = filepath.Join("missing", "db.sqlite3")
			}
			write()
			if status, body := request(c.Server.BaseURL, "/api/reload", "secret", ""); status != want {
				t.Fatalf("failed reload: %d %s", status, body)
			}
			log.Info().Msg("retained-logger")
			if raw := readLog(t, original); !strings.Contains(raw, `"message":"retained-logger"`) {
				t.Fatalf("previous logger was not retained: %s", raw)
			}
			if status, body := request(c.Server.BaseURL, "/api/config", "secret", ""); status != http.StatusOK || strings.Contains(body, "Must not apply") {
				t.Fatalf("previous configuration was not retained: %d %s", status, body)
			}
			if failure == "database" && strings.Contains(readLog(t, c.App.LogFile), "retained-logger") {
				t.Fatal("failed reload activated candidate logger")
			}
		})
	}
}
