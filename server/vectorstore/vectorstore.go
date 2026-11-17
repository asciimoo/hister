// SPDX-License-Identifier: AGPL-3.0-or-later

package vectorstore

import (
	"sort"

	"github.com/asciimoo/nogon/config"
)

const (
	searchCandidateMultiplier = 4
	maxChunksPerDocument      = 2
)

// Result represents a single semantic search hit at the chunk level.
type Result struct {
	DocID      string  `json:"doc_id"`
	ChunkIdx   int     `json:"chunk_idx"`
	ChunkText  string  `json:"chunk_text"`
	Similarity float64 `json:"similarity"`
}

// Chunk holds a text chunk and its precomputed embedding, ready for storage.
type Chunk struct {
	Index     int
	Text      string
	Embedding []float32
}

func mergeSearchResults(topK int, groups ...[]Result) []Result {
	nonEmpty := make([][]Result, 0, len(groups))
	for _, group := range groups {
		if len(group) > 0 {
			nonEmpty = append(nonEmpty, group)
		}
	}
	if len(nonEmpty) == 0 {
		return nil
	}
	if len(nonEmpty) == 1 {
		if topK > 0 && len(nonEmpty[0]) > topK {
			return nonEmpty[0][:topK]
		}
		return nonEmpty[0]
	}

	type resultKey struct {
		docID    string
		chunkIdx int
	}
	best := make(map[resultKey]Result)
	for _, group := range nonEmpty {
		for _, r := range group {
			key := resultKey{docID: r.DocID, chunkIdx: r.ChunkIdx}
			if prev, ok := best[key]; !ok || r.Similarity > prev.Similarity {
				best[key] = r
			}
		}
	}
	results := make([]Result, 0, len(best))
	for _, r := range best {
		results = append(results, r)
	}
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Similarity > results[j].Similarity
	})
	if topK > 0 && len(results) > topK {
		results = results[:topK]
	}
	return results
}

func searchCandidateLimit(documentLimit int) int {
	if documentLimit <= 0 {
		return documentLimit
	}
	return documentLimit * searchCandidateMultiplier
}

// diversifySearchResults selects the best matching documents while retaining
// a small number of their strongest chunks. Results must be ordered by
// descending similarity. Limiting chunks per document prevents long documents
// from consuming the semantic candidate pool returned to the indexer.
func diversifySearchResults(results []Result, documentLimit, chunkLimit int) []Result {
	if documentLimit <= 0 || chunkLimit <= 0 || len(results) == 0 {
		return nil
	}

	selectedDocuments := make(map[string]struct{}, documentLimit)
	chunkCounts := make(map[string]int, documentLimit)
	diverse := make([]Result, 0, min(len(results), documentLimit*chunkLimit))
	for _, result := range results {
		if _, selected := selectedDocuments[result.DocID]; !selected {
			if len(selectedDocuments) >= documentLimit {
				continue
			}
			selectedDocuments[result.DocID] = struct{}{}
		}
		if chunkCounts[result.DocID] >= chunkLimit {
			continue
		}
		diverse = append(diverse, result)
		chunkCounts[result.DocID]++
	}
	return diverse
}

// VectorStore is the interface for vector similarity backends.
type VectorStore interface {
	// Init creates tables/extensions if missing. Safe to call on every startup.
	Init() error

	// PutChunks upserts all chunk embeddings for a document, replacing any
	// previous chunks. docID matches the Bleve document ID (URL).
	// userID scopes the embeddings to a specific user (0 in single-user mode).
	PutChunks(docID string, userID uint, chunks []Chunk) error

	// Delete removes all chunk embeddings for a document.
	Delete(docID string) error

	// Search returns candidates for up to topK documents whose chunk embeddings
	// are closest to the query vector, with similarity >= threshold, scoped to
	// the given userID. A backend may return more than one chunk per document.
	Search(vector []float32, topK int, threshold float64, userID uint) ([]Result, error)

	// Clear removes all embeddings. Used during reindex to rebuild from scratch.
	Clear() error

	// Close releases resources.
	Close() error
}

// New creates a VectorStore implementation based on the database backend in use.
func New(cfg *config.Config) (VectorStore, error) {
	dbType, _ := cfg.DatabaseConnection()
	if dbType == config.Psql {
		return newPostgres(cfg)
	}
	return newSQLite(cfg)
}
