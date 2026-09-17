// SPDX-License-Identifier: AGPL-3.0-or-later

package model

import (
	"fmt"
	"strings"
	"time"

	sqlite3 "github.com/mattn/go-sqlite3"
	"gorm.io/gorm"
)

func migrateTimestampsToUTC() error {
	if DB.Name() != "sqlite" {
		return nil
	}

	// Keep this list tied to the schema at the time of this migration.
	tables := []struct {
		name    string
		columns []string
	}{
		{"histories", []string{"created_at", "updated_at", "deleted_at"}},
		{"links", []string{"created_at", "updated_at", "deleted_at"}},
		{"history_links", []string{"created_at", "updated_at", "deleted_at"}},
		{"users", []string{"created_at", "updated_at", "deleted_at"}},
		{"crawl_jobs", []string{"created_at", "updated_at"}},
		{"crawl_urls", []string{"created_at", "updated_at"}},
		{"document_versions", []string{"created_at"}},
		{"embedding_jobs", []string{"created_at", "updated_at", "available_at"}},
		{"web_sessions", []string{"created_at", "updated_at", "expires_at"}},
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		for _, table := range tables {
			for _, column := range table.columns {
				if err := migrateSQLiteTimestampColumn(tx, table.name, column); err != nil {
					return fmt.Errorf("normalize %s.%s: %w", table.name, column, err)
				}
			}
		}
		return nil
	})
}

func migrateSQLiteTimestampColumn(tx *gorm.DB, table, column string) error {
	const batchSize = 250
	var lastRowID int64
	firstBatch := true
	for {
		// Read text explicitly so an invalid timestamp cannot be silently
		// converted to the zero time by the driver. All these tables have
		// SQLite rowids, including the tables with text primary keys.
		var rows []struct {
			RowID int64
			Value string
		}
		q := tx.Table(table).
			Select(fmt.Sprintf("rowid AS row_id, CAST(`%s` AS TEXT) AS value", column)).
			Where(fmt.Sprintf("`%s` IS NOT NULL", column)).
			Order("rowid").Limit(batchSize)
		if !firstBatch {
			q = q.Where("rowid > ?", lastRowID)
		}
		if err := q.Scan(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			stamp, err := parseSQLiteTimestamp(row.Value)
			if err != nil {
				return fmt.Errorf("rowid %d: %w", row.RowID, err)
			}
			if row.Value == stamp.Format(sqlite3.SQLiteTimestampFormats[0]) {
				continue
			}
			// UpdateColumn preserves the other timestamps and avoids callbacks.
			if err := tx.Table(table).Where("rowid = ?", row.RowID).UpdateColumn(column, stamp).Error; err != nil {
				return err
			}
		}
		if len(rows) < batchSize {
			return nil
		}
		lastRowID = rows[len(rows)-1].RowID
		firstBatch = false
	}
}

func parseSQLiteTimestamp(value string) (time.Time, error) {
	// Match the driver's accepted formats and interpretation of timestamps
	// without offsets. Parse in Go to retain the full fractional precision.
	for _, layout := range sqlite3.SQLiteTimestampFormats {
		if stamp, err := time.ParseInLocation(layout, strings.TrimSuffix(value, "Z"), time.UTC); err == nil {
			return stamp.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid timestamp %q", value)
}
