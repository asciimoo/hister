// SPDX-FileContributor: Adam Tauber <asciimoo@gmail.com>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package model

import (
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

type migration struct {
	pre  func() error
	post func() error
}

var migrations = []migration{
	{
		post: func() error {
			// UpdateColumn (not Update) so the backfill does not bump UpdatedAt.
			return DB.Model(&HistoryLink{}).Where("pinned = ?", false).UpdateColumn("pinned", true).Error
		},
	},
	{
		post: func() error {
			// Earlier server side sessions used a required last_seen_at column.
			// Rolling expiration no longer uses it, and leaving the required column
			// behind makes inserts from the current model fail on upgraded databases.
			if !DB.Migrator().HasColumn("web_sessions", "last_seen_at") {
				return nil
			}
			return DB.Transaction(func(tx *gorm.DB) error {
				if err := tx.Exec("DROP INDEX IF EXISTS idx_web_sessions_last_seen_at").Error; err != nil {
					return err
				}
				return tx.Exec("ALTER TABLE web_sessions DROP COLUMN last_seen_at").Error
			})
		},
	},
	{
		post: func() error {
			// An earlier revision of the CrawlURL ETag field lacked an explicit
			// gorm column tag, so AutoMigrate created an "e_tag" column instead
			// of "etag". Rename it if present so queries against "etag" work.
			// AutoMigrate has already added the correctly-named column, so this
			// step only cleans up the stale one.
			if !DB.Migrator().HasColumn("crawl_urls", "e_tag") {
				return nil
			}
			// Copy any data across before dropping. In practice the buggy schema
			// never accepted writes (queries referenced "etag"), but do this
			// defensively in case some path did populate it.
			if err := DB.Exec("UPDATE crawl_urls SET etag = e_tag WHERE etag = '' AND e_tag != ''").Error; err != nil {
				return err
			}
			return DB.Exec("ALTER TABLE crawl_urls DROP COLUMN e_tag").Error
		},
	},
}

func migrationVersion() (int64, bool) {
	var dbVer int64
	if err := DB.Model(&Database{}).Select("version").First(&dbVer).Error; err != nil {
		return 0, false
	}
	return dbVer, true
}

func migratePre(dbVer int64) error {
	for i := dbVer; i < int64(len(migrations)); i++ {
		if migrations[i].pre == nil {
			continue
		}
		log.Info().Msgf("Running pre-migration for DB version %d", i+1)
		if err := migrations[i].pre(); err != nil {
			return err
		}
	}
	return nil
}

func migratePost(dbVer int64) error {
	migCount := 0
	for i := dbVer; i < int64(len(migrations)); i++ {
		if migrations[i].post != nil {
			log.Info().Msgf("Running post-migration for DB version %d", i+1)
			if err := migrations[i].post(); err != nil {
				return err
			}
		}
		if err := DB.Model(&Database{}).Where("id = 1").Update("version", i+1).Error; err != nil {
			return err
		}
		migCount++
	}
	if migCount > 0 {
		log.Debug().Int("Migrations performed", migCount).Msg("DB migrations completed")
	}
	return nil
}
