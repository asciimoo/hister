package indexer

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"unicode/utf8"

	"github.com/rs/zerolog/log"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/files"
)

var (
	ErrEmptyFile      = errors.New("empty file")
	ErrBinaryFile     = errors.New("binary file")
	ErrFileTooLarge   = errors.New("file too large")
	ErrReadFile       = errors.New("cannot read file")
	ErrAlreadyIndexed = errors.New("already indexed")

	maxFileSize int64 = 1024 * 1024 // 1MB default
)

const indexBatchSize = 50

type ReadFileError struct {
	Msg string
}

func (e *ReadFileError) Unwrap() error {
	return ErrReadFile
}

func (e *ReadFileError) Error() string {
	return fmt.Sprintf("%s: %s", ErrReadFile.Error(), e.Msg)
}

func IndexAll(ctx context.Context, dirs []*config.Directory, workers int) error {
	if workers < 1 {
		workers = 1
	}
	for _, dir := range dirs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		expanded := files.ExpandHome(dir.Path)
		if err := indexDirectory(ctx, expanded, dir, workers); err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			log.Error().Err(err).Str("directory", expanded).Msg("Failed to index directory")
		}
	}
	return nil
}

func indexDirectory(ctx context.Context, dir string, cfg *config.Directory, workers int) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("cannot access directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("not a directory: %s", dir)
	}

	log.Debug().Str("directory", dir).Msg("Indexing directory")

	sem := make(chan struct{}, workers)

	var (
		mu           sync.Mutex
		batch        = NewMultiBatch()
		indexed      int
		skipped      int
		pendingFlush bool
		wg           sync.WaitGroup
		walkErr      error
	)

	// Must be called with mu held.
	flushBatch := func() error {
		err := batch.Save()
		batch = NewMultiBatch()
		pendingFlush = false
		return err
	}

	walkErr = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err != nil {
			log.Warn().Err(err).Str("path", path).Msg("Error accessing path")
			return nil
		}
		if d.IsDir() {
			if path != dir && files.ShouldSkipDir(d.Name(), cfg.Excludes, cfg.IncludeHidden) {
				return filepath.SkipDir
			}
			return nil
		}
		if !cfg.IsMatching(d.Name()) {
			return nil
		}

		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}

		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			defer func() { <-sem }()

			doc, err := readFileDoc(p)
			if err != nil {
				log.Debug().Err(err).Str("path", p).Msg("Skipping file")
				mu.Lock()
				skipped++
				mu.Unlock()
				return
			}

			mu.Lock()
			defer mu.Unlock()

			if err := batch.Add(doc); err != nil {
				log.Warn().Err(err).Str("path", p).Msg("Failed to add file to batch")
				skipped++
				return
			}
			indexed++
			pendingFlush = true
			if indexed%indexBatchSize == 0 {
				if err := flushBatch(); err != nil {
					log.Warn().Err(err).Msg("Failed to flush index batch")
				}
			}
		}(path)

		return nil
	})

	wg.Wait()

	mu.Lock()
	defer mu.Unlock()

	if pendingFlush {
		if err := flushBatch(); err != nil {
			log.Warn().Err(err).Msg("Failed to flush final index batch")
		}
	}

	log.Debug().Str("directory", dir).Int("indexed", indexed).Int("skipped", skipped).Msg("Directory indexing complete")
	return walkErr
}

func readFileDoc(path string) (*Document, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() == 0 {
		return nil, ErrEmptyFile
	}
	if info.Size() > maxFileSize {
		return nil, fmt.Errorf("%w: %d bytes", ErrFileTooLarge, info.Size())
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	fileURL := "file://" + absPath

	existing := GetByURL(fileURL)
	if existing != nil && existing.Added == info.ModTime().Unix() {
		return nil, ErrAlreadyIndexed
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return nil, &ReadFileError{Msg: err.Error()}
	}
	if !utf8.Valid(content) {
		return nil, ErrBinaryFile
	}

	return &Document{
		URL:   fileURL,
		Text:  string(content),
		Added: info.ModTime().Unix(),
	}, nil
}

// IndexFile indexes a single file. Used by the file watcher.
func IndexFile(path string) error {
	doc, err := readFileDoc(path)
	if err != nil {
		if errors.Is(err, ErrAlreadyIndexed) {
			return nil
		}
		return err
	}
	return i.AddDocument(doc)
}
