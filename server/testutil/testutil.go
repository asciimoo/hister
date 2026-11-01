package testutil

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/model"
)

func Config(t testing.TB) *config.Config {
	t.Helper()
	cfg := config.CreateDefaultConfig()
	cfg.App.Directory = t.TempDir()
	return cfg
}

func ModelConfig(t testing.TB) *config.Config {
	t.Helper()
	cfg := Config(t)
	cfg.Server.Database = "test.sqlite3"
	return cfg
}

func InitModel(t testing.TB) *config.Config {
	t.Helper()
	cfg := ModelConfig(t)
	InitModelWithConfig(t, cfg)
	return cfg
}

func InitModelWithConfig(t testing.TB, cfg *config.Config) {
	t.Helper()
	if err := model.Init(cfg); err != nil {
		t.Fatalf("failed to init test DB: %v", err)
	}
	if db, err := model.DB.DB(); err == nil {
		t.Cleanup(func() {
			_ = db.Close()
		})
	}
}

func CreateUser(t testing.TB, username string) *model.User {
	t.Helper()
	u, err := model.CreateUser(username, "password123", false)
	if err != nil {
		t.Fatalf("failed to create test user %q: %v", username, err)
	}
	return u
}

func WriteFile(t testing.TB, dir, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("failed to write test file %q: %v", path, err)
	}
	return path
}

func ServeHTTP(t testing.TB, handler http.Handler, method, target string, body io.Reader, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, body)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}
