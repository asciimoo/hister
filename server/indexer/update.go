// SPDX-License-Identifier: AGPL-3.0-or-later

package indexer

import (
	"errors"
	"fmt"
	"strings"

	"github.com/asciimoo/nogon/server/document"
	"github.com/asciimoo/nogon/server/indexer/querybuilder"
	servertypes "github.com/asciimoo/nogon/server/types"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
)

var (
	ErrEmptyUpdate     = errors.New("at least one document attribute must be changed")
	ErrInvalidLanguage = errors.New("invalid document language")
)

const updatePageSize = 200

type pendingDocumentUpdate struct {
	document              *document.Document
	originalID            string
	originalUserID        uint
	embeddingContextDirty bool
}

func documentMutationQuery(text string, userID *uint) (query.Query, error) {
	if strings.TrimSpace(text) == "" {
		return nil, ErrEmptyFilter
	}
	q, err := querybuilder.BuildValidated(text)
	if err != nil {
		return nil, err
	}
	if userID == nil {
		return q, nil
	}
	uid := float64(*userID)
	userQ := bleve.NewNumericRangeInclusiveQuery(&uid, &uid, new(true), new(true))
	userQ.SetField("user_id")
	return bleve.NewConjunctionQuery(q, userQ), nil
}

func normalizeDocumentChanges(changes *servertypes.DocumentChanges) error {
	if changes.Empty() {
		return ErrEmptyUpdate
	}
	if changes.Language == nil {
		return nil
	}
	language := strings.ToLower(strings.TrimSpace(*changes.Language))
	if language == "" {
		return fmt.Errorf("%w: use %q for an unknown language", ErrInvalidLanguage, document.UnknownLanguage)
	}
	if language != "auto" {
		var err error
		language, err = normalizeLanguageOverride(language)
		if err != nil {
			return err
		}
	}
	changes.Language = &language
	return nil
}

func (i *Indexer) documentsForMutation(q query.Query, include resultInclude) ([]*document.Document, error) {
	var documents []*document.Document
	seen := make(map[string]struct{})
	var searchAfter []string
	for {
		req := bleve.NewSearchRequest(q)
		req.Fields = allFields
		req.Size = updatePageSize
		req.SortBy([]string{"_id"})
		if len(searchAfter) > 0 {
			req.SetSearchAfter(searchAfter)
		}
		res, err := i.searchIndexes(req)
		if err != nil {
			return nil, fmt.Errorf("find documents to update: %w", err)
		}
		if len(res.Hits) == 0 {
			return documents, nil
		}
		for _, hit := range res.Hits {
			if _, found := seen[hit.ID]; found {
				continue
			}
			seen[hit.ID] = struct{}{}
			d := i.resFromHit(hit, include)
			if d.HTMLKey == "" {
				d.HTML, _ = hit.Fields["html"].(string)
			}
			documents = append(documents, d)
		}
		searchAfter = res.Hits[len(res.Hits)-1].Sort
	}
}

func (i *Indexer) applyDocumentChanges(d *document.Document, changes servertypes.DocumentChanges) (changed, embeddingContextDirty bool) {
	if changes.UserID != nil && d.UserID != *changes.UserID {
		d.UserID = *changes.UserID
		changed = true
		embeddingContextDirty = true
	}
	if changes.Label != nil && d.Label != *changes.Label {
		d.Label = *changes.Label
		changed = true
	}
	titleChanged := changes.Title != nil && d.Title != *changes.Title
	if titleChanged {
		d.Title = *changes.Title
		changed = true
		embeddingContextDirty = true
	}
	if changes.Language != nil {
		override := *changes.Language
		if override == "auto" {
			override = ""
		}
		if d.LanguageOverride != override {
			d.LanguageOverride = override
			changed = true
		}
		previous := d.Language
		d.ResolveLanguage(i.langDetector)
		if d.Language != previous {
			changed = true
			embeddingContextDirty = true
		}
	} else if titleChanged && d.Type == document.Web && d.LanguageOverride == "" {
		// Reevaluate the content because the title can supply a language when
		// the body is too short. A manual pin remains authoritative.
		d.ResolveLanguage(i.langDetector)
	}
	return changed, embeddingContextDirty
}

// UpdateByQuery changes mutable attributes of every document selected by a
// search query. When sourceUserID is set, only documents owned by that user are
// eligible. Ownership moves that would overwrite an existing destination are
// counted as conflicts and skipped.
func (i *Indexer) UpdateByQuery(
	text string,
	sourceUserID *uint,
	changes servertypes.DocumentChanges,
	dryRun bool,
) (servertypes.UpdateDocumentsResult, error) {
	result := servertypes.UpdateDocumentsResult{}
	if err := normalizeDocumentChanges(&changes); err != nil {
		return result, err
	}
	q, err := documentMutationQuery(text, sourceUserID)
	if err != nil {
		return result, err
	}
	include := resultIncludeText
	if changes.Language != nil || changes.Title != nil {
		include |= resultIncludeHTML
	}
	documents, err := i.documentsForMutation(q, include)
	if err != nil {
		return result, err
	}
	result.Matched = len(documents)

	pending := make([]pendingDocumentUpdate, 0, len(documents))
	destinations := make(map[string]string)
	for _, d := range documents {
		originalID := d.ID()
		originalUserID := d.UserID
		changed, embeddingContextDirty := i.applyDocumentChanges(d, changes)
		if !changed {
			result.Unchanged++
			continue
		}
		if d.UserID != originalUserID {
			if d.Type == document.Local {
				if err := i.validateFileDocument(d); err != nil {
					return servertypes.UpdateDocumentsResult{}, fmt.Errorf("change owner of %s: %w", d.URL, err)
				}
			}
			destinationID := d.ID()
			if existingSourceID, found := destinations[destinationID]; found && existingSourceID != originalID {
				result.Conflicts++
				continue
			}
			if destinationID != originalID && i.getStoredDocumentState(destinationID).found {
				result.Conflicts++
				continue
			}
			destinations[destinationID] = originalID
		}
		pending = append(pending, pendingDocumentUpdate{
			document:              d,
			originalID:            originalID,
			originalUserID:        originalUserID,
			embeddingContextDirty: embeddingContextDirty,
		})
	}
	result.Updated = len(pending)
	if dryRun || len(pending) == 0 {
		return result, nil
	}

	for start := 0; start < len(pending); start += updatePageSize {
		end := min(start+updatePageSize, len(pending))
		batch := newMultiBatch(i)
		for _, update := range pending[start:end] {
			d := update.document
			state := i.getStoredDocumentState(d.ID())
			plan, err := i.prepareStorageWrite(d, state)
			if err != nil {
				batch.Discard()
				return result, fmt.Errorf("prepare update for %s: %w", d.URL, err)
			}
			// Storage can normalize a legacy language even for a label
			// update. Keep the language included in embeddings consistent with it.
			plan.needsEmbedding = i.embedder != nil && i.vectorStore != nil &&
				(update.embeddingContextDirty || state.embeddingContentChanged(d))
			if err := batch.applyDocumentWrite(d, plan); err != nil {
				batch.Discard()
				return result, fmt.Errorf("stage update for %s: %w", d.URL, err)
			}
			if update.originalID != d.ID() {
				batch.Delete(update.originalID)
				result.OwnershipChanges = append(result.OwnershipChanges, servertypes.DocumentOwnershipChange{
					URL:        d.URL,
					FromUserID: update.originalUserID,
					ToUserID:   d.UserID,
				})
			}
		}
		if err := batch.Save(); err != nil {
			return result, fmt.Errorf("save document updates: %w", err)
		}
	}
	return result, nil
}
