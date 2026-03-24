package indexer

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"unicode/utf8"

	"github.com/rs/zerolog/log"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/files"
)

var (
	ErrEmptyFile    = errors.New("empty file")
	ErrBinaryFile   = errors.New("binary file")
	ErrFileTooLarge = errors.New("file too large")
	ErrReadFile     = errors.New("cannot read file")

	maxFileSize int64 = 1024 * 1024 // 1MB default
)

type ReadFileError struct {
	Msg string
}

func (e *ReadFileError) Unwrap() error {
	return ErrReadFile
}

func (e *ReadFileError) Error() string {
	return fmt.Sprintf("%s: %s", ErrReadFile.Error(), e.Msg)
}

func IndexAll(ctx context.Context, dirs []*config.Directory) error {
	for _, dir := range dirs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		expanded := files.ExpandHome(dir.Path)
		if err := indexDirectory(ctx, expanded, dir); err != nil {
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

func indexDirectory(ctx context.Context, dir string, cfg *config.Directory) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("cannot access directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("not a directory: %s", dir)
	}

	indexed := 0
	skipped := 0

	log.Debug().Str("directory", dir).Msg("Indexing directory")

	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
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
		if err := IndexFile(path); err != nil {
			log.Debug().Err(err).Str("path", path).Msg("Skipping file")
			skipped++
		} else {
			indexed++
		}
		return nil
	})

	log.Debug().Str("directory", dir).Int("indexed", indexed).Int("skipped", skipped).Msg("Directory indexing complete")
	return err
}

func IndexFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		return ErrEmptyFile
	}
	if info.Size() > maxFileSize {
		return fmt.Errorf("%w: %d bytes", ErrFileTooLarge, info.Size())
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	fileURL := "file://" + absPath

	// Skip if already indexed with the same modification time
	existing := GetByURL(fileURL)
	if existing != nil && existing.Added == info.ModTime().Unix() {
		return nil
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return &ReadFileError{
			Msg: err.Error(),
		}
	}
	if !utf8.Valid(content) {
		return ErrBinaryFile
	}

	doc := &Document{
		URL:   fileURL,
		Text:  string(content),
		Added: info.ModTime().Unix(),
	}

	return i.AddDocument(doc)
}
