// SPDX-FileContributor: Adam Tauber <asciimoo@gmail.com>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package model

import (
	"testing"
	"time"

	"github.com/asciimoo/nogon/config"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestMigrationDropsRequiredWebSessionLastSeenColumn(t *testing.T) {
	cfg := config.CreateDefaultConfig()
	cfg.App.Directory = t.TempDir()
	cfg.Server.Database = "legacy.sqlite3"
	_, dsn := cfg.DatabaseConnection()

	legacyDB, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := legacyDB.Exec("CREATE TABLE databases (id integer PRIMARY KEY AUTOINCREMENT, version integer)").Error; err != nil {
		t.Fatal(err)
	}
	if err := legacyDB.Exec("INSERT INTO databases (id, version) VALUES (1, 1)").Error; err != nil {
		t.Fatal(err)
	}
	if err := legacyDB.Exec(`CREATE TABLE web_sessions (
		id integer PRIMARY KEY AUTOINCREMENT,
		token_hash text NOT NULL,
		data blob NOT NULL,
		created_at datetime,
		updated_at datetime,
		last_seen_at datetime NOT NULL,
		expires_at datetime NOT NULL
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := legacyDB.Exec("CREATE INDEX idx_web_sessions_last_seen_at ON web_sessions(last_seen_at)").Error; err != nil {
		t.Fatal(err)
	}
	legacySQLDB, err := legacyDB.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := legacySQLDB.Close(); err != nil {
		t.Fatal(err)
	}

	if err := Init(cfg); err != nil {
		t.Fatal(err)
	}
	currentSQLDB, err := DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = currentSQLDB.Close() })

	if DB.Migrator().HasColumn("web_sessions", "last_seen_at") {
		t.Fatal("web_sessions.last_seen_at still exists after migration")
	}
	if err := CreateWebSession(&WebSession{
		TokenHash: "2b7847b6ff9b65017f19fbdc4019f3a287b0a16669b4370d03ab8d4a899a0f47",
		Data:      []byte("session data"),
		ExpiresAt: time.Now().Add(30 * 24 * time.Hour),
	}); err != nil {
		t.Fatalf("create web session after migration: %v", err)
	}
	dbVersion, initialized := migrationVersion()
	if !initialized || dbVersion != int64(len(migrations)) {
		t.Fatalf("database version = %d, initialized = %v, want %d and true", dbVersion, initialized, len(migrations))
	}
}

func TestMigrationAddsUserVersions(t *testing.T) {
	cfg := config.CreateDefaultConfig()
	cfg.App.Directory = t.TempDir()
	cfg.Server.Database = "legacy-users.sqlite3"
	_, dsn := cfg.DatabaseConnection()
	legacyDB, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	db, err := legacyDB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := legacyDB.Exec(`CREATE TABLE users (
		id integer PRIMARY KEY AUTOINCREMENT, username text, password text,
		token text, is_admin numeric, created_at datetime, updated_at datetime,
		deleted_at datetime, rules_json text DEFAULT '{}', o_auth_id text
	)`).Error; err != nil {
		t.Fatal(err)
	}
	updatedAt := time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC)
	if err := legacyDB.Exec(`INSERT INTO users (id, username, password, token, is_admin, updated_at)
		VALUES (1, ?, ?, ?, ?, ?)`, "alice", "existing-hash", "existing-token", true, updatedAt).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := Init(cfg); err != nil {
		t.Fatal(err)
	}
	currentDB, err := DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := currentDB.Close(); err != nil {
			t.Error(err)
		}
	})
	var version uint
	if err := DB.Model(&User{}).Select("session_version").Where("id = ?", 1).Scan(&version).Error; err != nil {
		t.Fatalf("existing account has no session version: %v", err)
	}
	if version != 0 {
		t.Fatalf("existing account version = %d, want 0 for session compatibility", version)
	}
	var revision uint64
	if err := DB.Model(&User{}).Select("revision").Where("id = ?", 1).Scan(&revision).Error; err != nil || revision != 1 {
		t.Fatalf("existing account revision = %d, want 1, error = %v", revision, err)
	}
	user, err := GetUserByID(1)
	if err != nil {
		t.Fatal(err)
	}
	if user.Username != "alice" || user.Password != "existing-hash" || user.Token != "existing-token" || !user.IsAdmin || !user.UpdatedAt.Equal(updatedAt) {
		t.Fatal("adding session version changed existing account data")
	}
}
