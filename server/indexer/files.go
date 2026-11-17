package indexer

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/blevesearch/bleve/v2"
	"github.com/rs/zerolog/log"

	"github.com/asciimoo/nogon/config"
	"github.com/asciimoo/nogon/files"
	"github.com/asciimoo/nogon/files/extract"
	"github.com/asciimoo/nogon/server/document"
	"github.com/asciimoo/nogon/server/extractor"
	"github.com/asciimoo/nogon/server/model"
)

var (
	ErrEmptyFile    = files.ErrEmptyFile
	ErrBinaryFile   = extract.ErrBinaryFile
	ErrFileTooLarge = files.ErrFileTooLarge
)

const defaultMaxFileSize int64 = 1024 * 1024 // 1MB default

type fileIndexOp int

const (
	fileIndexAdd fileIndexOp = iota
	fileIndexDelete
)

type fileIndexQueueItem struct {
	op     fileIndexOp
	path   string
	userID uint
}

// LocalDocumentCleanupResult describes the local document reconciliation
// performed by cleanup.
type LocalDocumentCleanupResult struct {
	Checked int
	Removed int
	Skipped int
}

type directoryOwner struct {
	userID uint
	err    error
}

type FileIndexQueue struct {
	mu      sync.Mutex
	pending map[string]fileIndexQueueItem
	notify  chan struct{}
	indexer *Indexer
}

func (i *Indexer) NewFileIndexQueue() *FileIndexQueue {
	return newFileIndexQueue(i)
}

func newFileIndexQueue(i *Indexer) *FileIndexQueue {
	return &FileIndexQueue{
		pending: make(map[string]fileIndexQueueItem),
		notify:  make(chan struct{}, 1),
		indexer: i,
	}
}

func (q *FileIndexQueue) EnqueueIndex(path string, userID uint) {
	q.enqueue(fileIndexQueueItem{
		op:     fileIndexAdd,
		path:   path,
		userID: userID,
	})
}

func (q *FileIndexQueue) EnqueueDelete(path string) {
	q.enqueue(fileIndexQueueItem{
		op:   fileIndexDelete,
		path: path,
	})
}

func (q *FileIndexQueue) enqueue(item fileIndexQueueItem) {
	q.mu.Lock()
	q.pending[item.path] = item
	q.mu.Unlock()

	select {
	case q.notify <- struct{}{}:
	default:
	}
}

func (q *FileIndexQueue) Run(ctx context.Context) error {
	for {
		if item, ok := q.pop(); ok {
			q.process(item)
			continue
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-q.notify:
		}
	}
}

func (q *FileIndexQueue) pop() (fileIndexQueueItem, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	for path, item := range q.pending {
		delete(q.pending, path)
		return item, true
	}
	return fileIndexQueueItem{}, false
}

func (q *FileIndexQueue) process(item fileIndexQueueItem) {
	if q.indexer == nil {
		log.Error().Msg("Cannot process file queue item before indexer initialization")
		return
	}
	switch item.op {
	case fileIndexAdd:
		if err := q.indexer.IndexFile(item.path, item.userID); err != nil {
			logFileIndexError(item.path, err)
		}
	case fileIndexDelete:
		if err := q.indexer.DeleteFile(item.path); err != nil {
			log.Debug().Err(err).Str("path", item.path).Msg("Failed to delete file from index")
		}
	}
}

func logFileIndexError(path string, err error) {
	if errors.Is(err, document.ErrSensitiveContent) {
		log.Warn().Str("path", path).Msg("File indexing skipped: sensitive content detected")
		return
	}
	log.Debug().Err(err).Str("path", path).Msg("Failed to index file")
}

func (i *Indexer) IndexAll(dirs []*config.Directory) {
	for _, dir := range dirs {
		expanded := files.ExpandHome(dir.Path)
		if err := i.indexDirectory(expanded, dir); err != nil {
			log.Error().Err(err).Str("directory", expanded).Msg("Failed to index directory")
		}
	}
}

func (q *FileIndexQueue) EnqueueAll(dirs []*config.Directory) {
	for _, dir := range dirs {
		expanded := files.ExpandHome(dir.Path)
		if err := q.enqueueDirectory(expanded, dir); err != nil {
			log.Error().Err(err).Str("directory", expanded).Msg("Failed to queue directory indexing")
		}
	}
}

func (i *Indexer) indexDirectory(dir string, cfg *config.Directory) error {
	log.Debug().Str("directory", dir).Msg("Indexing directory")

	indexed, skipped, err := walkDirectoryFiles(dir, cfg, func(path string, userID uint) bool {
		if err := i.IndexFile(path, userID); err != nil {
			logFileIndexError(path, err)
			return false
		}
		return true
	})

	log.Debug().Str("directory", dir).Int("indexed", indexed).Int("skipped", skipped).Msg("Directory indexing complete")
	return err
}

func (q *FileIndexQueue) enqueueDirectory(dir string, cfg *config.Directory) error {
	log.Debug().Str("directory", dir).Msg("Queueing directory indexing")

	queued, _, err := walkDirectoryFiles(dir, cfg, func(path string, userID uint) bool {
		q.EnqueueIndex(path, userID)
		return true
	})

	log.Debug().Str("directory", dir).Int("queued", queued).Msg("Directory indexing queued")
	return err
}

func walkDirectoryFiles(dir string, cfg *config.Directory, callback func(path string, userID uint) bool) (int, int, error) {
	userID, err := directoryUserID(cfg)
	if err != nil {
		return 0, 0, fmt.Errorf("user %q not found for directory %s: %w", cfg.User, dir, err)
	}
	return walkDirectoryFilesForUser(dir, cfg, userID, callback)
}

func directoryUserID(cfg *config.Directory) (uint, error) {
	if cfg.User != "" {
		u, err := model.GetUser(cfg.User)
		if err != nil {
			return 0, err
		}
		return u.ID, nil
	}
	return 0, nil
}

func walkDirectoryFilesForUser(dir string, cfg *config.Directory, userID uint, callback func(path string, userID uint) bool) (int, int, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return 0, 0, fmt.Errorf("cannot access directory: %w", err)
	}
	if !info.IsDir() {
		return 0, 0, fmt.Errorf("not a directory: %s", dir)
	}

	processed := 0
	skipped := 0

	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
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
		if callback(path, userID) {
			processed++
		} else {
			skipped++
		}
		return nil
	})

	return processed, skipped, err
}

// CleanupLocalDocuments removes indexed local documents that no longer match
// the current directory filters or ownership. It only inspects indexed fields
// and does not walk, stat, or read the filesystem.
func (i *Indexer) CleanupLocalDocuments(dirs []*config.Directory) (LocalDocumentCleanupResult, error) {
	result := LocalDocumentCleanupResult{}

	owners := make(map[*config.Directory]directoryOwner, len(dirs))
	for _, dir := range dirs {
		if dir == nil {
			continue
		}
		ownerID, err := directoryUserID(dir)
		owners[dir] = directoryOwner{userID: ownerID, err: err}
		if err != nil {
			log.Warn().Err(err).Str("directory", dir.Path).Msg("Failed to resolve user while cleaning local documents")
		}
	}

	checked, removed, skipped, err := i.pruneStaleLocalDocuments(dirs, owners)
	result.Checked = checked
	result.Removed = removed
	result.Skipped = skipped
	return result, err
}

func (i *Indexer) pruneStaleLocalDocuments(dirs []*config.Directory, owners map[*config.Directory]directoryOwner) (int, int, int, error) {
	docType := float64(document.Local)
	localQuery := bleve.NewNumericRangeInclusiveQuery(&docType, &docType, new(true), new(true))
	localQuery.SetField("type")
	req := bleve.NewSearchRequest(localQuery)
	req.Fields = []string{"url", "user_id"}
	req.Size = 200
	req.SortBy([]string{"_id"})

	checked, removed, skipped := 0, 0, 0
	var sortKey []string
	for {
		if len(sortKey) > 0 {
			req.SetSearchAfter(sortKey)
		}
		res, err := i.searchIndexes(req)
		if err != nil {
			return checked, removed, skipped, fmt.Errorf("find indexed local documents: %w", err)
		}
		if len(res.Hits) == 0 {
			return checked, removed, skipped, nil
		}

		batch := newMultiBatch(i)
		pageRemoved := 0
		for _, hit := range res.Hits {
			checked++
			remove, couldNotCheck := staleLocalDocument(hit.Fields, dirs, owners)
			if couldNotCheck {
				skipped++
			}
			if remove {
				batch.Delete(hit.ID)
				pageRemoved++
			}
		}
		sortKey = res.Hits[len(res.Hits)-1].Sort
		if pageRemoved == 0 {
			continue
		}
		if err := batch.Save(); err != nil {
			return checked, removed, skipped, fmt.Errorf("remove stale local documents: %w", err)
		}
		removed += pageRemoved
	}
}

func staleLocalDocument(fields map[string]any, dirs []*config.Directory, owners map[*config.Directory]directoryOwner) (remove bool, couldNotCheck bool) {
	rawURL, _ := fields["url"].(string)
	parsed, err := url.Parse(rawURL)
	if err != nil || !strings.EqualFold(parsed.Scheme, "file") {
		return true, false
	}
	filePath := filepath.Clean(files.FileURLToPath(rawURL))
	if !filepath.IsAbs(filePath) {
		return true, false
	}
	dir := files.FindMatchingDir(dirs, filePath)
	if !files.DirectoryMatchesPath(dir, filePath) {
		return true, false
	}
	owner, ok := owners[dir]
	if !ok || owner.err != nil {
		return false, true
	}
	indexedUserID := uint(0)
	if value, ok := fields["user_id"].(float64); ok {
		indexedUserID = uint(value)
	}
	return indexedUserID != owner.userID, false
}

func configuredFileLabel(dirs []*config.Directory, path string) string {
	dir := files.FindMatchingDir(dirs, path)
	if !files.DirectoryMatchesPath(dir, path) {
		return ""
	}
	return dir.Label
}

func (i *Indexer) openLocalFile(path string) (*os.File, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	dir := files.FindMatchingDir(i.directories, absPath)
	if !files.DirectoryMatchesPath(dir, absPath) {
		return nil, ErrFileURLNotAllowed
	}
	rootPath, err := filepath.Abs(files.ExpandHome(dir.Path))
	if err != nil {
		return nil, err
	}
	rootPath, err = filepath.EvalSymlinks(rootPath)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()

	// Resolve first to retain support for absolute links within the configured
	// tree. Open through Root so a subsequent symlink replacement cannot escape.
	resolvedPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(rootPath, resolvedPath)
	if err != nil || !filepath.IsLocal(rel) {
		return nil, ErrFileURLNotAllowed
	}
	return root.Open(rel)
}

func (i *Indexer) prepareLocalFile(ctx context.Context, path string, file *os.File, d *document.Document) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		return ErrEmptyFile
	}
	if info.Size() > i.maxFileSize {
		return fmt.Errorf("%w: %d bytes", ErrFileTooLarge, info.Size())
	}

	content, err := files.ReadAllBounded(file, i.maxFileSize)
	if err != nil {
		if errors.Is(err, ErrFileTooLarge) {
			return err
		}
		return &document.ReadFileError{Msg: err.Error()}
	}

	d.Updated = info.ModTime().Unix()
	d.SetTrustedLocalFile()
	return extractor.ExtractFile(ctx, i.fileExtractor, path, d, content)
}

func (i *Indexer) reloadLocalFile(ctx context.Context, path string, source *document.Document) (*document.Document, error) {
	file, err := i.openLocalFile(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	d := &document.Document{
		URL:              source.URL,
		LanguageOverride: source.LanguageOverride,
		Added:            source.Added,
		UserID:           source.UserID,
		Label:            source.Label,
		AddCount:         source.AddCount,
	}
	if source.IgnoreSkipRules() {
		d.SetIgnoreSkipRules(true)
	}
	if err := i.prepareLocalFile(ctx, path, file, d); err != nil {
		return nil, err
	}
	return d, nil
}

func (i *Indexer) IndexFile(path string, userID uint) error {
	file, err := i.openLocalFile(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		return ErrEmptyFile
	}

	if info.Size() > i.maxFileSize {
		return fmt.Errorf("%w: %d bytes", ErrFileTooLarge, info.Size())
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	fileURL := files.PathToFileURL(absPath)
	label := configuredFileLabel(i.directories, absPath)

	// Skip if already indexed with the same modification time
	existing := i.GetByURLAndUser(fileURL, userID)
	if existing != nil && existing.Updated == info.ModTime().Unix() && existing.Type == document.Local {
		if label == "" || existing.Label == label {
			return nil
		}
		existing.Label = label
		return i.Save(existing)
	}

	doc := &document.Document{
		URL:    fileURL,
		UserID: userID,
		Label:  label,
	}
	if err := i.prepareLocalFile(context.Background(), path, file, doc); err != nil {
		return err
	}
	return i.AddDocument(doc)
}

// DeleteFile removes the document for the given filesystem path from the index.
// It uses a url: field query so it removes the file across all users and
// language-specific sub-indexes. Returns nil if the document is not found.
func (i *Indexer) DeleteFile(path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}
	fileURL := files.PathToFileURL(absPath)
	_, err = i.DeleteByQuery("url:"+fileURL, nil, nil)
	return err
}
