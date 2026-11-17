package model

import (
	"context"

	"github.com/asciimoo/nogon/server/types"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Purge removes selected retained records in one transaction. It never changes
// current indexed documents or their assets, embeddings, or pending work.
func Purge(ctx context.Context, request types.PurgeRequest) (types.PurgeResult, error) {
	result := types.PurgeResult{DryRun: request.DryRun}
	if err := request.Validate(); err != nil {
		return result, err
	}
	request.Before = request.Before.UTC()
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if request.Versions {
			query := tx.Model(&DocumentVersion{}).Where("created_at < ?", request.Before)
			if err := purgeRows(query, &DocumentVersion{}, request.DryRun, &result.Versions); err != nil {
				return err
			}
		}
		if request.History {
			entries := tx.Model(&HistoryLink{}).Where("updated_at < ? AND pinned = ?", request.Before, false)
			// Preview orphan counts after the proposed entry removal, without writes.
			retained := tx.Model(&HistoryLink{}).Where("id NOT IN (?)", entries.Session(&gorm.Session{}).Select("id"))
			queries := tx.Model(&History{}).Where("updated_at < ?", request.Before).
				Where("id NOT IN (?)", retained.Session(&gorm.Session{}).Select("history_id"))
			urls := tx.Model(&Link{}).Where("updated_at < ?", request.Before).
				Where("id NOT IN (?)", retained.Session(&gorm.Session{}).Select("link_id"))
			if err := purgeRows(entries, &HistoryLink{}, request.DryRun, &result.HistoryEntries); err != nil {
				return err
			}
			if err := purgeRows(queries, &History{}, request.DryRun, &result.HistoryQueries); err != nil {
				return err
			}
			if err := purgeRows(urls, &Link{}, request.DryRun, &result.HistoryURLs); err != nil {
				return err
			}
		}
		if request.CrawlJobs {
			jobs := tx.Model(&CrawlJob{}).Where("status = ? AND updated_at < ?", CrawlJobCompleted, request.Before).
				Where("NOT EXISTS (?)", tx.Model(&CrawlURL{}).Select("1").
					Where("crawl_urls.job_id = crawl_jobs.id AND status IN ?", []string{CrawlURLPending, CrawlURLInProgress}))
			if request.DryRun {
				if err := jobs.Session(&gorm.Session{}).Count(&result.CrawlJobs).Error; err != nil {
					return err
				}
				return tx.Model(&CrawlURL{}).Where("job_id IN (?)", jobs.Select("id")).Count(&result.CrawlURLs).Error
			}
			for {
				var ids []string
				if err := jobs.Session(&gorm.Session{}).Clauses(clause.Locking{Strength: "UPDATE"}).Order("id").Limit(200).Pluck("id", &ids).Error; err != nil {
					return err
				}
				if len(ids) == 0 {
					break
				}
				urls := tx.Where("job_id IN ?", ids).Delete(&CrawlURL{})
				if urls.Error != nil {
					return urls.Error
				}
				deleted := tx.Where("id IN ?", ids).Delete(&CrawlJob{})
				if deleted.Error != nil {
					return deleted.Error
				}
				result.CrawlURLs += urls.RowsAffected
				result.CrawlJobs += deleted.RowsAffected
			}
		}
		return nil
	})
	if err != nil {
		return types.PurgeResult{}, err
	}
	return result, nil
}

func purgeRows(query *gorm.DB, value any, dryRun bool, count *int64) error {
	if dryRun {
		return query.Count(count).Error
	}
	deleted := query.Unscoped().Delete(value)
	*count = deleted.RowsAffected
	return deleted.Error
}
