// SPDX-FileContributor: Adam Tauber <asciimoo@gmail.com>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package indexer

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/asciimoo/nogon/server/document"
	"github.com/asciimoo/nogon/server/timeline"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
)

func (i *Indexer) GetLatestDocuments(limit int, latest string, userID uint) *Results {
	return i.GetLatestDocumentsFiltered(limit, latest, userID, "")
}

func (i *Indexer) GetLatestDocumentsFiltered(limit int, latest string, userID uint, filter string) *Results {
	return i.GetLatestDocumentsFilteredByDate(limit, latest, userID, filter, 0, 0)
}

func (i *Indexer) GetLatestDocumentsFilteredByDate(limit int, latest string, userID uint, filter string, dateFrom, dateTo int64) *Results {
	q := latestDocumentsQuery(userID, filter, dateFrom, dateTo)
	req := bleve.NewSearchRequest(q)
	req.Fields = []string{"url", "title", "added", "updated", "add_count", "favicon_key", "favicon"}
	req.Size = limit
	req.SortBy([]string{"-updated", "_id"})
	if latest != "" {
		var after []string
		if err := json.Unmarshal([]byte(latest), &after); err == nil {
			req.SetSearchAfter(after)
		}
	}
	res, err := i.searchIndexes(req)
	if err != nil || len(res.Hits) < 1 {
		return nil
	}
	docs := make([]*document.Document, len(res.Hits))
	for i, h := range res.Hits {
		d := &document.Document{
			DocumentID: h.ID,
			Title:      h.Fields["title"].(string),
			URL:        h.Fields["url"].(string),
		}
		if n, ok := h.Fields["added"].(float64); ok {
			d.Added = int64(n)
		}
		if n, ok := h.Fields["updated"].(float64); ok {
			d.Updated = int64(n)
		} else {
			d.Updated = d.Added
		}
		if n, ok := h.Fields["add_count"].(float64); ok {
			d.AddCount = uint(n)
		}
		if s, ok := h.Fields["favicon_key"].(string); ok {
			d.FaviconKey = s
		} else if s, ok := h.Fields["favicon"].(string); ok {
			// backward compat: old documents still have favicon inline in Bleve
			d.Favicon = s
		}
		if d.AddCount < 1 {
			d.AddCount = 1
		}
		docs[i] = d
	}
	r := &Results{Documents: docs}
	if pk, err := json.Marshal(res.Hits[len(res.Hits)-1].Sort); err == nil {
		r.PageKey = string(pk)
	}
	return r
}

func latestDocumentsQuery(userID uint, filter string, dateFrom, dateTo int64) query.Query {
	var q query.Query
	if userID > 0 {
		uid := float64(userID)
		userQuery := bleve.NewNumericRangeInclusiveQuery(&uid, &uid, new(true), new(true))
		userQuery.SetField("user_id")
		zeroF := float64(0)
		globalQuery := bleve.NewNumericRangeInclusiveQuery(&zeroF, &zeroF, new(true), new(true))
		globalQuery.SetField("user_id")
		q = bleve.NewDisjunctionQuery(userQuery, globalQuery)
	} else {
		q = query.NewMatchAllQuery()
	}
	if filter = strings.TrimSpace(filter); filter != "" {
		pattern := "(?i).*" + regexp.QuoteMeta(filter) + ".*"
		urlQuery := bleve.NewRegexpQuery(pattern)
		urlQuery.SetField("url")
		titlePhraseQuery := bleve.NewMatchPhraseQuery(filter)
		titlePhraseQuery.SetField("title")
		filterQueries := []query.Query{urlQuery, titlePhraseQuery}
		if !strings.ContainsAny(filter, " \t\r\n") {
			titleRegexpQuery := bleve.NewRegexpQuery(pattern)
			titleRegexpQuery.SetField("title")
			filterQueries = append(filterQueries, titleRegexpQuery)
		}
		q = bleve.NewConjunctionQuery(q, bleve.NewDisjunctionQuery(filterQueries...))
	}
	if dateFrom != 0 || dateTo != 0 {
		var min, max *float64
		if dateFrom != 0 {
			min = new(float64)
			*min = float64(dateFrom)
		}
		if dateTo != 0 {
			max = new(float64)
			*max = float64(dateTo)
		}
		dateQuery := bleve.NewNumericRangeInclusiveQuery(min, max, new(true), new(false))
		dateQuery.SetField("updated")
		q = bleve.NewConjunctionQuery(q, dateQuery)
	}
	return q
}

func (i *Indexer) GetHistoryTimeline(userID uint, filter string, loc *time.Location) (*timeline.Result, error) {
	q := latestDocumentsQuery(userID, filter, 0, 0)
	var oldest int64
	oldestRequest := bleve.NewSearchRequest(q)
	oldestRequest.Size = 1
	oldestRequest.Fields = []string{"updated"}
	oldestRequest.SortBy([]string{"updated", "_id"})
	oldestResult, err := i.searchIndexes(oldestRequest)
	if err != nil {
		return nil, err
	}
	if len(oldestResult.Hits) > 0 {
		if value, ok := oldestResult.Hits[0].Fields["updated"].(float64); ok {
			oldest = int64(value)
		}
	}

	result := timeline.New(time.Now(), loc, oldest)
	if err := i.populateTimelineFacet(q, result); err != nil {
		return nil, err
	}
	return result, nil
}

func (i *Indexer) GetHistoryTimelineDays(userID uint, filter string, loc *time.Location, dateFrom, dateTo int64) (*timeline.DailyResult, error) {
	result := timeline.NewDays(dateFrom, dateTo, loc)
	if len(result.Days) == 0 {
		return result, nil
	}

	q := latestDocumentsQuery(userID, filter, dateFrom, dateTo)
	if err := i.populateTimelineFacet(q, result); err != nil {
		return nil, err
	}
	return result, nil
}

type timelineFacetBuckets interface {
	Buckets() []*timeline.Bucket
	SetCount(string, int)
}

func (i *Indexer) populateTimelineFacet(q query.Query, result timelineFacetBuckets) error {
	buckets := result.Buckets()
	if len(buckets) == 0 {
		return nil
	}
	facet := bleve.NewFacetRequest("updated", len(buckets))
	for _, bucket := range buckets {
		var min *float64
		if bucket.From != 0 {
			value := float64(bucket.From)
			min = &value
		}
		max := float64(bucket.To)
		facet.AddNumericRange(bucket.Key, min, &max)
	}
	req := bleve.NewSearchRequest(q)
	req.Size = 0
	req.AddFacet("history_timeline", facet)
	res, err := i.searchIndexes(req)
	if err != nil {
		return err
	}
	if facetResult := res.Facets["history_timeline"]; facetResult != nil {
		for _, bucket := range facetResult.NumericRanges {
			result.SetCount(bucket.Name, bucket.Count)
		}
	}
	return nil
}
