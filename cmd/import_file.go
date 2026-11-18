package cmd

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/asciimoo/nogon/client"
	"github.com/asciimoo/nogon/config"
	"github.com/asciimoo/nogon/files"
	"github.com/asciimoo/nogon/files/extract"
	"github.com/asciimoo/nogon/server/document"
	"github.com/asciimoo/nogon/server/extractor"

	"github.com/rs/zerolog/log"
)

type importFileInput struct {
	Path  string
	Label string
}

var errImportNotRegularFile = errors.New("input is not a regular file")

func readImportFile(path string, maxFileSize int64) ([]byte, os.FileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, nil, errImportNotRegularFile
	}
	if info.Size() > maxFileSize {
		return nil, nil, fileSnapshotSizeError(info.Size(), maxFileSize)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = file.Close() }()
	info, err = file.Stat()
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, nil, errImportNotRegularFile
	}
	if info.Size() > maxFileSize {
		return nil, nil, fileSnapshotSizeError(info.Size(), maxFileSize)
	}
	content, err := files.ReadAllBounded(file, maxFileSize)
	if errors.Is(err, files.ErrFileTooLarge) {
		return nil, nil, fmt.Errorf("%w: limit from indexer.max_file_size_mb is %d bytes", err, maxFileSize)
	}
	return content, info, err
}

type fileImporter struct {
	client        *client.Client
	fileExtractor *extract.Extractor
}

func newFileImporter(c *client.Client, cfg config.Indexer) *fileImporter {
	return &fileImporter{client: c, fileExtractor: extract.New(cfg.PDFParser)}
}

type fileImportOptions struct {
	Source       string
	MaxFileSize  int64
	SkipExisting bool
	StartDate    int64
	EndDate      int64
	BatchSize    int
	Label        documentLabelOverride
}

func (i *fileImporter) importFile(ctx context.Context, input importFileInput, opts fileImportOptions) (imported, skipped, errCount int) {
	switch strings.ToLower(filepath.Ext(input.Path)) {
	case ".7z":
		return importJSONFile(i.client, input.Path, opts.SkipExisting, opts.StartDate, opts.EndDate, opts.BatchSize, opts.Label)
	case ".json":
		isExport, err := isNogonJSONExport(input.Path)
		if err != nil {
			log.Warn().Err(err).Str("file", input.Path).Msg("Failed to inspect JSON file")
			return 0, 0, 1
		}
		if isExport {
			return importJSONFile(i.client, input.Path, opts.SkipExisting, opts.StartDate, opts.EndDate, opts.BatchSize, opts.Label)
		}
	case ".html", ".htm":
		return i.importHTMLFile(ctx, input, opts.Source, opts.MaxFileSize, opts.SkipExisting, opts.Label)
	}
	return i.importRemoteFilePath(ctx, input, opts.Source, opts.MaxFileSize, opts.SkipExisting, opts.Label)
}

func defaultRemoteFileSource() string {
	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		return "client"
	}
	return hostname
}

func normalizeRemoteFileSource(source string) (string, error) {
	var normalized strings.Builder
	lastSeparator := false
	for _, r := range strings.ToLower(strings.TrimSpace(source)) {
		allowed := r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '-'
		if allowed {
			normalized.WriteRune(r)
			lastSeparator = false
			continue
		}
		if normalized.Len() > 0 && !lastSeparator {
			normalized.WriteByte('-')
			lastSeparator = true
		}
	}
	value := strings.Trim(normalized.String(), ".-")
	if value == "" {
		return "", fmt.Errorf("--source must contain at least one ASCII letter or digit")
	}
	return value, nil
}

func remoteFileURL(source, filename string) (string, error) {
	normalizedSource, err := normalizeRemoteFileSource(source)
	if err != nil {
		return "", err
	}
	absPath, err := filepath.Abs(filename)
	if err != nil {
		return "", fmt.Errorf("resolve file path: %w", err)
	}
	urlPath := filepath.ToSlash(absPath)
	if !strings.HasPrefix(urlPath, "/") {
		urlPath = "/" + urlPath
	}
	return (&url.URL{Scheme: "remote-file", Host: normalizedSource, Path: urlPath}).String(), nil
}

func expandImportInputs(args []string, directories []*config.Directory) ([]importFileInput, error) {
	inputs := make([]importFileInput, 0)
	seen := make(map[string]bool)
	appendFile := func(path, label string) error {
		absPath, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		if !seen[absPath] {
			seen[absPath] = true
			inputs = append(inputs, importFileInput{Path: absPath, Label: label})
		}
		return nil
	}
	walkDirectory := func(root string, directory *config.Directory) error {
		return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				log.Warn().Err(walkErr).Str("path", path).Msg("Error accessing path")
				return nil
			}
			if entry.IsDir() {
				if path != root && files.ShouldSkipDir(entry.Name(), directory.Excludes, directory.IncludeHidden) {
					return filepath.SkipDir
				}
				return nil
			}
			info, err := os.Stat(path)
			if err != nil {
				log.Warn().Err(err).Str("path", path).Msg("Failed to inspect path")
				return nil
			}
			if !info.Mode().IsRegular() || !directory.IsMatching(entry.Name()) {
				return nil
			}
			return appendFile(path, directory.Label)
		})
	}

	if len(args) == 0 {
		if len(directories) == 0 {
			return nil, fmt.Errorf("no watched directories are configured")
		}
		for _, directory := range directories {
			if directory == nil {
				continue
			}
			root, err := filepath.Abs(files.ExpandHome(directory.Path))
			if err != nil {
				return nil, fmt.Errorf("resolve watched directory %s: %w", directory.Path, err)
			}
			if err := walkDirectory(root, directory); err != nil {
				return nil, fmt.Errorf("walk watched directory %s: %w", root, err)
			}
		}
	} else {
		for _, input := range args {
			absPath, err := filepath.Abs(files.ExpandHome(input))
			if err != nil {
				return nil, fmt.Errorf("resolve input %s: %w", input, err)
			}
			info, err := os.Stat(absPath)
			if err != nil {
				return nil, fmt.Errorf("inspect input %s: %w", input, err)
			}
			if !info.IsDir() {
				if !info.Mode().IsRegular() {
					return nil, fmt.Errorf("input is not a regular file: %s", input)
				}
				label := ""
				if directory := files.FindMatchingDir(directories, absPath); files.DirectoryMatchesPath(directory, absPath) {
					label = directory.Label
				}
				if err := appendFile(absPath, label); err != nil {
					return nil, err
				}
				continue
			}

			directory := files.FindMatchingDir(directories, absPath)
			if directory == nil {
				directory = &config.Directory{Path: absPath}
			}
			if err := walkDirectory(absPath, directory); err != nil {
				return nil, fmt.Errorf("walk input directory %s: %w", absPath, err)
			}
		}
	}

	if len(args) == 0 {
		sort.Slice(inputs, func(i, j int) bool { return inputs[i].Path < inputs[j].Path })
	}
	return inputs, nil
}

func (i *fileImporter) importRemoteFile(
	ctx context.Context,
	input importFileInput,
	content []byte,
	info os.FileInfo,
	source string,
	maxFileSize int64,
	skip bool,
	labelOverride documentLabelOverride,
) (imported, skipped, errCount int) {
	remoteURL, err := remoteFileURL(source, input.Path)
	if err != nil {
		log.Warn().Err(err).Str("file", input.Path).Msg("Failed to create remote file URL")
		return 0, 0, 1
	}
	if skip {
		exists, err := i.client.DocumentExists(remoteURL)
		if err != nil {
			log.Warn().Err(err).Str("url", remoteURL).Msg("Failed to check if remote file exists")
			return 0, 0, 1
		}
		if exists {
			log.Debug().Str("url", remoteURL).Msg("Remote file already exists, skipping")
			return 0, 1, 0
		}
	}

	d, err := i.prepareRemoteFile(ctx, input, content, info, source, maxFileSize, labelOverride)
	if err != nil {
		log.Warn().Err(fileSnapshotImportError(input.Path, err)).Str("file", input.Path).Msg("Failed to extract file content")
		return 0, 0, 1
	}
	if err := i.client.AddDocumentJSON(clientDocument(d)); err != nil {
		log.Warn().Err(fileSnapshotImportError(input.Path, err)).Str("file", input.Path).Str("url", remoteURL).Msg("Failed to import file snapshot")
		return 0, 0, 1
	}
	return 1, 0, 0
}

func fileSnapshotImportError(path string, err error) error {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".json" || errors.Is(err, extract.ErrBinaryFile) {
		return err
	}
	reason := "This file has no extension"
	if ext != "" {
		reason = fmt.Sprintf("This file has a %q extension", ext)
	}
	return fmt.Errorf("%w. %s, so Nogon treated it as a file snapshot. "+
		"If this is a Nogon JSON export, rename it to %q and retry the import",
		err, reason, strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))+".json")
}

func fileSnapshotSizeError(size, limit int64) error {
	return fmt.Errorf("%w: file is %d bytes, limit from indexer.max_file_size_mb is %d bytes",
		files.ErrFileTooLarge, size, limit)
}

func fileContentImportError(path string, err error) error {
	if !errors.Is(err, extract.ErrBinaryFile) {
		return err
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".zip", ".gz", ".bz2", ".xz", ".zst", ".tgz", ".tar", ".rar":
		return fmt.Errorf("%w: this archive format cannot be imported directly. Extract the archive and import the extracted files", err)
	default:
		return fmt.Errorf("%w: plain text extraction requires valid UTF 8. If this is a text file, convert it to UTF 8 and retry; otherwise use a supported file format", err)
	}
}

func (i *fileImporter) prepareRemoteFile(ctx context.Context, input importFileInput, content []byte, info os.FileInfo, source string, maxFileSize int64, labelOverride documentLabelOverride) (*document.Document, error) {
	if info.Size() == 0 {
		return nil, files.ErrEmptyFile
	}
	if maxFileSize > 0 && (info.Size() > maxFileSize || int64(len(content)) > maxFileSize) {
		return nil, fileSnapshotSizeError(max(info.Size(), int64(len(content))), maxFileSize)
	}
	remoteURL, err := remoteFileURL(source, input.Path)
	if err != nil {
		return nil, err
	}
	fallbackLabel := input.Label
	if fallbackLabel == "" {
		fallbackLabel = "import"
	}
	d := &document.Document{
		URL:     remoteURL,
		Updated: info.ModTime().Unix(),
		Type:    document.RemoteFile,
		Label:   labelOverride.resolve("", fallbackLabel),
	}
	if err := extractor.ExtractFile(ctx, i.fileExtractor, input.Path, d, content); err != nil {
		return nil, fileContentImportError(input.Path, err)
	}
	if d.Text == "" && d.HTML == "" {
		return nil, fmt.Errorf("file contains no indexable content")
	}
	return d, nil
}

func (i *fileImporter) importRemoteFilePath(
	ctx context.Context,
	input importFileInput,
	source string,
	maxFileSize int64,
	skip bool,
	labelOverride documentLabelOverride,
) (imported, skipped, errCount int) {
	content, info, err := readImportFile(input.Path, maxFileSize)
	if err != nil {
		log.Warn().Err(err).Str("file", input.Path).Msg("Failed to read file")
		return 0, 0, 1
	}
	return i.importRemoteFile(ctx, input, content, info, source, maxFileSize, skip, labelOverride)
}
