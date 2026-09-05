// SPDX-FileContributor: Adam Tauber <asciimoo@gmail.com>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package model

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"time"

	"github.com/asciimoo/hister/config"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
)

// ErrDBType is returned when an unknown database type is encountered.
var ErrDBType = errors.New("unknown database type")

// DB is the global database instance.
var DB *gorm.DB

// AccessMode controls database access and schema migration behavior.
type AccessMode uint8

const (
	ReadWrite AccessMode = iota
	ReadOnly
)

// Init initializes the database connection and runs migrations.
func Init(c *config.Config) error {
	return initDatabase(c, ReadWrite)
}

// InitReadOnly opens an existing database without running migrations.
// SQLite connections use read only access without file locking.
func InitReadOnly(c *config.Config) error {
	return initDatabase(c, ReadOnly)
}

func initDatabase(c *config.Config, accessMode AccessMode) error {
	dbCfg := &gorm.Config{}
	if c.App.DebugSQL {
		dbCfg.Logger = logger.Default.LogMode(logger.Info)
	} else {
		dbCfg.Logger = logger.Default.LogMode(logger.Silent)
	}
	dbt, dsn := c.DatabaseConnection()
	var err error
	switch dbt {
	case config.Psql:
		DB, err = gorm.Open(postgres.Open(dsn), dbCfg)
		if err != nil {
			return err
		}
	case config.Sqlite:
		if accessMode == ReadOnly {
			path := (&url.URL{Path: filepath.ToSlash(dsn)}).EscapedPath()
			dsn = "file:" + path + "?mode=ro&nolock=1"
		}
		DB, err = gorm.Open(sqlite.Open(dsn), dbCfg)
		if err != nil {
			return err
		}
	default:
		return ErrDBType
	}
	if err = DB.SetupJoinTable(&History{}, "Links", &HistoryLink{}); err != nil {
		return fmt.Errorf("failed to setup join table for URL history: %w", err)
	}
	if accessMode == ReadOnly {
		return nil
	}
	dbVer, initialized := migrationVersion()
	if initialized {
		if err = migratePre(dbVer); err != nil {
			return fmt.Errorf("pre-automigrate migration of database '%s' has failed: %w", dsn, err)
		}
	}
	if err = automigrate(); err != nil {
		return fmt.Errorf("auto migration of database '%s' has failed: %w", dsn, err)
	}
	if initialized {
		if err = migratePost(dbVer); err != nil {
			return fmt.Errorf("post-automigrate migration of database '%s' has failed: %w", dsn, err)
		}
	} else {
		// Fresh database: AutoMigrate just created the latest schema, so record
		// the current version to avoid replaying historical migrations against it.
		DB.Save(&Database{Version: uint(len(migrations))})
	}
	return nil
}

func automigrate() error {
	return DB.AutoMigrate(
		&Database{},
		&History{},
		&Link{},
		&HistoryLink{},
		&User{},
		&CrawlJob{},
		&CrawlURL{},
		&DocumentVersion{},
		&EmbeddingJob{},
		&WebSession{},
	)
}

// Database represents the database version tracking table.
type Database struct {
	ID      uint `gorm:"primaryKey"`
	Version uint
}

// CommonFields contains fields common to all models.
type CommonFields struct {
	ID        uint       `gorm:"primary_key" json:"id"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"deleted_at"`
}

type LegacyIndexerMetadata struct {
	Version             int    `json:"version"`
	AnalyzerFingerprint string `json:"analyzer_fingerprint"`
}

func (LegacyIndexerMetadata) TableName() string {
	return "indexer_versions"
}

// GetLegacyIndexerMetadata reads index metadata written by versions that kept
// it in SQL. New installations do not create this table.
func GetLegacyIndexerMetadata() (*LegacyIndexerMetadata, error) {
	if !DB.Migrator().HasTable(&LegacyIndexerMetadata{}) {
		return nil, nil
	}
	var metadata LegacyIndexerMetadata
	if err := DB.Model(&LegacyIndexerMetadata{}).First(&metadata).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &metadata, nil
}
