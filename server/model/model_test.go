package model_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/asciimoo/hister/server/model"
	"github.com/asciimoo/hister/server/testutil"

	"github.com/mattn/go-sqlite3"
)

func TestInitReadOnly(t *testing.T) {
	cfg := testutil.Config(t)
	cfg.Server.Database = "database # %.sqlite3"
	testutil.InitModelWithConfig(t, cfg)
	if err := model.CreateCrawlJob("test", "https://example.com", "{}", "test"); err != nil {
		t.Fatal(err)
	}
	// Leave an older schema that normal initialization would migrate.
	if err := model.DB.Exec("UPDATE databases SET version = 0").Error; err != nil {
		t.Fatal(err)
	}
	if err := model.DB.Migrator().DropTable(&model.WebSession{}); err != nil {
		t.Fatal(err)
	}
	writable, err := model.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	conn, err := writable.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			t.Errorf("close writable connection: %v", err)
		}
	}()
	if _, err := conn.ExecContext(context.Background(), "BEGIN EXCLUSIVE"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := conn.ExecContext(context.Background(), "ROLLBACK"); err != nil {
			t.Errorf("roll back exclusive transaction: %v", err)
		}
	}()

	if err := model.InitReadOnly(cfg); err != nil {
		t.Fatal(err)
	}
	readOnly, err := model.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := readOnly.Close(); err != nil {
			t.Errorf("close read only database: %v", err)
		}
	}()

	jobs, err := model.ListCrawlJobs()
	if err != nil {
		t.Fatalf("read while another connection holds an exclusive lock: %v", err)
	}
	if len(jobs) != 1 || jobs[0].ID != "test" {
		t.Fatalf("jobs = %#v, want the existing crawl job", jobs)
	}
	var version model.Database
	if err := model.DB.First(&version).Error; err != nil {
		t.Fatal(err)
	}
	if version.Version != 0 || model.DB.Migrator().HasTable(&model.WebSession{}) {
		t.Fatal("read only initialization migrated the database")
	}
	err = model.DeleteCrawlJob("test")
	var sqliteErr sqlite3.Error
	if !errors.As(err, &sqliteErr) || sqliteErr.Code != sqlite3.ErrReadonly {
		t.Fatalf("write error = %v, want SQLITE_READONLY", err)
	}
}

func TestInitReadOnlyDoesNotCreateDatabase(t *testing.T) {
	cfg := testutil.Config(t)
	if err := model.InitReadOnly(cfg); err == nil {
		if db, err := model.DB.DB(); err == nil {
			_ = db.Close()
		}
		t.Fatal("opening a missing database succeeded")
	}
	_, path := cfg.DatabaseConnection()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("database stat error = %v, want os.ErrNotExist", err)
	}
}

func TestLegacyIndexerMetadata(t *testing.T) {
	testutil.InitModel(t)

	metadata, err := model.GetLegacyIndexerMetadata()
	if err != nil {
		t.Fatal(err)
	}
	if metadata != nil {
		t.Fatalf("legacy metadata = %#v, want nil", metadata)
	}

	if err := model.DB.Exec(`
		CREATE TABLE indexer_versions (
			version INTEGER,
			analyzer_fingerprint TEXT
		)
	`).Error; err != nil {
		t.Fatal(err)
	}
	if err := model.DB.Exec(
		"INSERT INTO indexer_versions (version, analyzer_fingerprint) VALUES (?, ?)",
		8,
		"fingerprint",
	).Error; err != nil {
		t.Fatal(err)
	}

	metadata, err = model.GetLegacyIndexerMetadata()
	if err != nil {
		t.Fatal(err)
	}
	if metadata == nil {
		t.Fatal("legacy metadata is nil")
	}
	if metadata.Version != 8 || metadata.AnalyzerFingerprint != "fingerprint" {
		t.Fatalf("legacy metadata = %#v", metadata)
	}
}
