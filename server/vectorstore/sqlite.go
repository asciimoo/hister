// SPDX-License-Identifier: AGPL-3.0-or-later

package vectorstore

import (
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/vectorstore/sqlitevec"

	"github.com/rs/zerolog/log"
)

const sqliteVectorSchemaVersion = 1

var sqliteEmbeddingColumn = regexp.MustCompile(`(?i)\bembedding\s+float\s*\[\s*(\d+)\s*\]`)

type sqliteVectorStore struct {
	db         *sql.DB
	dimensions int
}

func newSQLite(cfg *config.Config) (VectorStore, error) {
	sqlitevec.Auto()
	dbPath := cfg.FullPath(cfg.Server.Database)
	dir := filepath.Dir(dbPath)
	vecDBPath := filepath.Join(dir, "vectors.sqlite3")

	db, err := sql.Open("sqlite3", vecDBPath)
	if err != nil {
		return nil, fmt.Errorf("open vector database: %w", err)
	}
	// Single connection to avoid locking issues with SQLite.
	db.SetMaxOpenConns(1)

	return &sqliteVectorStore{
		db:         db,
		dimensions: cfg.SemanticSearch.Dimensions,
	}, nil
}

func (s *sqliteVectorStore) Init() error {
	// Verify sqlite-vec is available by querying its version.
	var version string
	if err := s.db.QueryRow("SELECT vec_version()").Scan(&version); err != nil {
		return fmt.Errorf("sqlite-vec extension not available (is the vec0 shared library installed?): %w", err)
	}
	log.Info().Str("version", version).Msg("sqlite-vec loaded")

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin vector database initialization: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var schemaVersion int
	if err := tx.QueryRow(`PRAGMA user_version`).Scan(&schemaVersion); err != nil {
		return fmt.Errorf("read vector database schema version: %w", err)
	}
	if schemaVersion > sqliteVectorSchemaVersion {
		return fmt.Errorf("vector database schema version %d is newer than supported version %d", schemaVersion, sqliteVectorSchemaVersion)
	}

	// Regular table for chunk metadata (text content, doc association).
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS chunk_meta (
		chunk_key TEXT PRIMARY KEY,
		doc_id TEXT NOT NULL,
		chunk_idx INTEGER NOT NULL,
		user_id INTEGER NOT NULL DEFAULT 0,
		chunk_text TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("create chunk_meta table: %w", err)
	}
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_chunk_meta_doc ON chunk_meta(doc_id)`); err != nil {
		return fmt.Errorf("create chunk_meta doc_id index: %w", err)
	}

	migratedVectors, err := s.initEmbeddingsTable(tx)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, sqliteVectorSchemaVersion)); err != nil {
		return fmt.Errorf("write vector database schema version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit vector database initialization: %w", err)
	}
	if migratedVectors >= 0 {
		log.Info().Int64("vectors", migratedVectors).Msg("migrated SQLite vector store to cosine distance; review semantic_search.similarity_threshold if it was customized")
	}
	return nil
}

func (s *sqliteVectorStore) createEmbeddingsTable(tx *sql.Tx, dimensions int) error {
	stmt := fmt.Sprintf(`CREATE VIRTUAL TABLE embeddings USING vec0(
		user_id INTEGER PARTITION KEY,
		chunk_key TEXT PRIMARY KEY,
		embedding FLOAT[%d] distance_metric=cosine
	)`, dimensions)
	if _, err := tx.Exec(stmt); err != nil {
		return fmt.Errorf("create embeddings table: %w", err)
	}
	return nil
}

func (s *sqliteVectorStore) initEmbeddingsTable(tx *sql.Tx) (int64, error) {
	var schema string
	err := tx.QueryRow(`SELECT sql FROM sqlite_schema WHERE type = 'table' AND name = 'embeddings'`).Scan(&schema)
	if errors.Is(err, sql.ErrNoRows) {
		return -1, s.createEmbeddingsTable(tx, s.dimensions)
	}
	if err != nil {
		return -1, fmt.Errorf("read embeddings table schema: %w", err)
	}

	normalizedSchema := strings.Join(strings.Fields(strings.ToLower(schema)), "")
	if !strings.Contains(normalizedSchema, "usingvec0(") {
		return -1, errors.New("existing embeddings table is not a vec0 virtual table")
	}
	column := sqliteEmbeddingColumn.FindStringSubmatch(schema)
	if len(column) != 2 {
		return -1, errors.New("cannot read dimensions from existing embeddings table")
	}
	storedDimensions, err := strconv.Atoi(column[1])
	if err != nil {
		return -1, fmt.Errorf("read stored embedding dimensions: %w", err)
	}
	if storedDimensions != s.dimensions {
		// Keep existing vectors until the user requests a reindex. Initialization
		// must still succeed so the live indexer can rebuild this store.
		log.Warn().Int("stored_dimensions", storedDimensions).Int("configured_dimensions", s.dimensions).
			Msg("vector store dimensions differ from semantic_search.dimensions. Run `hister reindex` to rebuild embeddings")
	}
	if strings.Contains(normalizedSchema, "distance_metric=cosine") {
		return -1, nil
	}

	if _, err := tx.Exec(`CREATE TEMP TABLE embeddings_cosine_migration (
		user_id INTEGER NOT NULL,
		chunk_key TEXT PRIMARY KEY,
		embedding BLOB NOT NULL
	)`); err != nil {
		return -1, fmt.Errorf("create embeddings migration table: %w", err)
	}
	copyResult, err := tx.Exec(`INSERT INTO embeddings_cosine_migration(user_id, chunk_key, embedding)
		SELECT user_id, chunk_key, embedding FROM embeddings`)
	if err != nil {
		return -1, fmt.Errorf("copy embeddings for cosine migration: %w", err)
	}
	migratedVectors, err := copyResult.RowsAffected()
	if err != nil {
		return -1, fmt.Errorf("count embeddings for cosine migration: %w", err)
	}
	if _, err := tx.Exec(`DROP TABLE embeddings`); err != nil {
		return -1, fmt.Errorf("drop L2 embeddings table: %w", err)
	}
	// A distance metric migration preserves vectors and their original size.
	// Only a reindex can replace them with vectors of a different size.
	if err := s.createEmbeddingsTable(tx, storedDimensions); err != nil {
		return -1, err
	}
	restoreResult, err := tx.Exec(`INSERT INTO embeddings(user_id, chunk_key, embedding)
		SELECT user_id, chunk_key, embedding FROM embeddings_cosine_migration`)
	if err != nil {
		return -1, fmt.Errorf("restore embeddings after cosine migration: %w", err)
	}
	restoredVectors, err := restoreResult.RowsAffected()
	if err != nil {
		return -1, fmt.Errorf("count restored embeddings after cosine migration: %w", err)
	}
	if restoredVectors != migratedVectors {
		return -1, fmt.Errorf("restore embeddings after cosine migration: copied %d vectors but restored %d", migratedVectors, restoredVectors)
	}
	if _, err := tx.Exec(`DROP TABLE embeddings_cosine_migration`); err != nil {
		return -1, fmt.Errorf("drop embeddings migration table: %w", err)
	}
	return migratedVectors, nil
}

func chunkKey(docID string, chunkIdx int) string {
	return fmt.Sprintf("%s#%d", docID, chunkIdx)
}

func (s *sqliteVectorStore) PutChunks(docID string, userID uint, chunks []Chunk) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	// Delete all existing chunks for this document.
	if _, err = tx.Exec(`DELETE FROM embeddings WHERE chunk_key IN (SELECT chunk_key FROM chunk_meta WHERE doc_id = ?)`, docID); err != nil {
		return fmt.Errorf("delete old embeddings: %w", err)
	}
	if _, err = tx.Exec(`DELETE FROM chunk_meta WHERE doc_id = ?`, docID); err != nil {
		return fmt.Errorf("delete old chunk_meta: %w", err)
	}

	metaStmt, err := tx.Prepare(`INSERT INTO chunk_meta(chunk_key, doc_id, chunk_idx, user_id, chunk_text) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare chunk_meta insert: %w", err)
	}
	defer metaStmt.Close() //nolint:errcheck

	embStmt, err := tx.Prepare(`INSERT INTO embeddings(user_id, chunk_key, embedding) VALUES (?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare embeddings insert: %w", err)
	}
	defer embStmt.Close() //nolint:errcheck

	for _, c := range chunks {
		key := chunkKey(docID, c.Index)
		if _, err = metaStmt.Exec(key, docID, c.Index, userID, c.Text); err != nil {
			return fmt.Errorf("insert chunk_meta: %w", err)
		}
		blob := float32ToBlob(c.Embedding)
		if _, err = embStmt.Exec(userID, key, blob); err != nil {
			return fmt.Errorf("insert embedding: %w", err)
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit chunks: %w", err)
	}
	return nil
}

func (s *sqliteVectorStore) Delete(docID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if _, err = tx.Exec(`DELETE FROM embeddings WHERE chunk_key IN (SELECT chunk_key FROM chunk_meta WHERE doc_id = ?)`, docID); err != nil {
		return fmt.Errorf("delete embeddings: %w", err)
	}
	if _, err = tx.Exec(`DELETE FROM chunk_meta WHERE doc_id = ?`, docID); err != nil {
		return fmt.Errorf("delete chunk_meta: %w", err)
	}
	return tx.Commit()
}

func (s *sqliteVectorStore) Search(vector []float32, topK int, threshold float64, userID uint) (_ []Result, err error) {
	candidateLimit := searchCandidateLimit(topK)
	userResults, err := s.searchUser(vector, candidateLimit, threshold, userID)
	if err != nil || userID == 0 {
		return diversifySearchResults(userResults, topK, maxChunksPerDocument), err
	}
	globalResults, err := s.searchUser(vector, candidateLimit, threshold, 0)
	if err != nil {
		return nil, err
	}
	merged := mergeSearchResults(0, userResults, globalResults)
	return diversifySearchResults(merged, topK, maxChunksPerDocument), nil
}

func (s *sqliteVectorStore) searchUser(vector []float32, topK int, threshold float64, userID uint) (_ []Result, err error) {
	blob := float32ToBlob(vector)
	rows, err := s.db.Query(
		`SELECT e.chunk_key, e.distance, COALESCE(m.doc_id, ''), COALESCE(m.chunk_idx, 0), COALESCE(m.chunk_text, '')
		 FROM embeddings e
		 LEFT JOIN chunk_meta m ON e.chunk_key = m.chunk_key
		 WHERE e.embedding MATCH ?
		   AND e.k = ?
		   AND e.user_id = ?
		 ORDER BY e.distance`,
		blob, topK, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("vector search for user %d: %w", userID, err)
	}
	defer func() {
		if cerr := rows.Close(); err == nil {
			err = cerr
		}
	}()

	var results []Result
	for rows.Next() {
		var chunkKey, docID, chunkText string
		var chunkIdx int
		var distance float64
		if err := rows.Scan(&chunkKey, &distance, &docID, &chunkIdx, &chunkText); err != nil {
			return nil, fmt.Errorf("scan vector result: %w", err)
		}
		similarity := 1.0 - distance
		if similarity >= threshold {
			results = append(results, Result{
				DocID:      docID,
				ChunkIdx:   chunkIdx,
				ChunkText:  chunkText,
				Similarity: similarity,
			})
		}
	}
	return results, rows.Err()
}

func (s *sqliteVectorStore) Clear() error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin vector store rebuild: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()
	// vec0 column dimensions cannot be altered. Recreate the table so reindex
	// also applies dimension changes from the semantic search configuration.
	if _, err := tx.Exec(`DROP TABLE embeddings`); err != nil {
		return fmt.Errorf("drop embeddings table: %w", err)
	}
	if err := s.createEmbeddingsTable(tx, s.dimensions); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM chunk_meta`); err != nil {
		return fmt.Errorf("clear chunk_meta: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit vector store rebuild: %w", err)
	}
	return nil
}

func (s *sqliteVectorStore) Close() error {
	return s.db.Close()
}

// float32ToBlob converts a []float32 to a little-endian byte slice suitable
// for sqlite-vec's vec_f32 format.
func float32ToBlob(v []float32) []byte {
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}
