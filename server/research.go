// SPDX-FileContributor: Theophysics research fork
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/files/extract"
	"github.com/asciimoo/hister/server/document"
	"github.com/asciimoo/hister/server/indexer"
	"github.com/asciimoo/hister/server/model"

	"github.com/rs/zerolog/log"
)

// Research jobs: named containers for uploaded source files. Each upload is
// hashed (SHA-256), receives a stored receipt, is checked for exact
// duplicates, and is indexed through the existing Hister indexer under a
// stable per-artifact URL. Search can then be scoped to a single job.

const (
	researchMaxUploadBytes = 256 << 20 // whole multipart request
	researchMaxFileBytes   = 64 << 20  // single file
	researchSearchLimit    = 50
	researchIndexHost      = "research"
)

//go:embed research_page.html
var researchPageHTML []byte

func researchUploadLimit(*config.Config) int64 { return researchMaxUploadBytes }

func init() {
	Endpoints = append(Endpoints,
		&Endpoint{
			Name:        "Research page",
			Path:        "/research",
			Method:      GET,
			NoStore:     true,
			Handler:     serveResearchPage,
			Description: "Research job control page",
		},
		&Endpoint{
			Name:        "List research jobs",
			Path:        "/api/research/jobs",
			Method:      GET,
			Handler:     serveResearchJobs,
			Description: "List the caller's research jobs with receipt counts",
		},
		&Endpoint{
			Name:         "Create research job",
			Path:         "/api/research/jobs",
			Method:       POST,
			CSRFRequired: true,
			Handler:      serveCreateResearchJob,
			Description:  "Create a named research job",
			JSONSchema: []*JSONSchemaField{
				{Name: "name", Type: "string", Required: true, Description: "Job name, 1-200 characters"},
				{Name: "description", Type: "string", Description: "Optional description"},
			},
		},
		&Endpoint{
			Name:        "Get research job",
			Path:        "/api/research/jobs/{id}",
			Method:      GET,
			Handler:     serveResearchJob,
			Description: "Get a research job and its source receipts",
		},
		&Endpoint{
			Name:         "Upload research files",
			Path:         "/api/research/jobs/{id}/files",
			Method:       POST,
			CSRFRequired: true,
			BodyLimit:    researchUploadLimit,
			Handler:      serveResearchUpload,
			Description:  "Upload files (multipart field 'files', optional parallel 'paths' for relative folder paths). Each file is hashed, receipted, duplicate-checked and indexed.",
		},
		&Endpoint{
			Name:        "Search research job",
			Path:        "/api/research/jobs/{id}/search",
			Method:      GET,
			Handler:     serveResearchSearch,
			Description: "Full-text search limited to documents accepted into one research job",
			Args: []*EndpointArg{
				{Name: "q", Type: "string", Required: true, Description: "Search query"},
			},
		},
	)
}

func serveResearchPage(c *webContext) {
	c.Response.Header().Set("Content-Type", "text/html; charset=utf-8")
	c.Response.Header().Set("Content-Security-Policy", fmt.Sprintf("default-src 'self'; script-src 'nonce-%s'; style-src 'unsafe-inline'", c.nonce))
	page := strings.ReplaceAll(string(researchPageHTML), "<script>", fmt.Sprintf(`<script nonce="%s">`, c.nonce))
	if _, err := io.WriteString(c.Response, page); err != nil {
		log.Warn().Err(err).Msg("failed to write research page")
	}
}

func researchJobFromPath(c *webContext) (*model.ResearchJob, bool) {
	id, err := strconv.ParseUint(c.Request.PathValue("id"), 10, 64)
	if err != nil || id == 0 {
		http.Error(c.Response, "invalid job id", http.StatusBadRequest)
		return nil, false
	}
	job, err := model.GetResearchJob(targetUserID(c), uint(id))
	if errors.Is(err, model.ErrResearchJobNotFound) {
		http.Error(c.Response, err.Error(), http.StatusNotFound)
		return nil, false
	}
	if err != nil {
		log.Error().Err(err).Msg("failed to load research job")
		serve500(c)
		return nil, false
	}
	return job, true
}

func serveResearchJobs(c *webContext) {
	jobs, err := model.ListResearchJobs(targetUserID(c))
	if err != nil {
		log.Error().Err(err).Msg("failed to list research jobs")
		serve500(c)
		return
	}
	c.JSON(map[string]any{"jobs": jobs})
}

func serveCreateResearchJob(c *webContext) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if !decodeRequestJSON(c, &req) {
		return
	}
	job, err := model.CreateResearchJob(targetUserID(c), req.Name, req.Description)
	if errors.Is(err, model.ErrResearchJobName) {
		http.Error(c.Response, err.Error(), http.StatusBadRequest)
		return
	}
	if err != nil {
		log.Error().Err(err).Msg("failed to create research job")
		serve500(c)
		return
	}
	c.JSONStatus(http.StatusCreated, job)
}

func serveResearchJob(c *webContext) {
	job, ok := researchJobFromPath(c)
	if !ok {
		return
	}
	sources, err := model.ListResearchSources(job.ID)
	if err != nil {
		log.Error().Err(err).Msg("failed to list research sources")
		serve500(c)
		return
	}
	c.JSON(map[string]any{"job": job, "sources": sources})
}

// researchIndexURL is the stable index identity of an artifact. It depends
// only on the content hash, so re-uploads and duplicates map to one index
// record and replays are idempotent.
func researchIndexURL(sha string, name string) string {
	base := path.Base(strings.ReplaceAll(name, "\\", "/"))
	if base == "." || base == "/" || base == "" {
		base = "file"
	}
	return (&url.URL{Scheme: "remote-file", Host: researchIndexHost, Path: "/" + sha + "/" + base}).String()
}

func cleanRelativePath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	p = path.Clean("/" + p)
	p = strings.TrimPrefix(p, "/")
	if p == "." {
		return ""
	}
	if len(p) > 2000 {
		p = p[:2000]
	}
	return p
}

type researchUploadResult struct {
	Source *model.ResearchSource `json:"source"`
}

func serveResearchUpload(c *webContext) {
	job, ok := researchJobFromPath(c)
	if !ok {
		return
	}
	if job.Status != model.ResearchJobOpen {
		http.Error(c.Response, "job is not open", http.StatusConflict)
		return
	}
	reader, err := c.Request.MultipartReader()
	if err != nil {
		http.Error(c.Response, "expected multipart/form-data upload", http.StatusBadRequest)
		return
	}
	uid := targetUserID(c)
	var paths []string
	var results []*model.ResearchSource
	fileIndex := 0
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			if !requestBodyTooLarge(c, err) {
				http.Error(c.Response, "invalid upload: "+err.Error(), http.StatusBadRequest)
			}
			return
		}
		switch part.FormName() {
		case "paths":
			b, _ := io.ReadAll(io.LimitReader(part, 4096))
			paths = append(paths, string(b))
		case "files":
			rel := ""
			if fileIndex < len(paths) {
				rel = paths[fileIndex]
			}
			fileIndex++
			src, err := acceptResearchFile(c, job, uid, part, rel)
			if err != nil {
				if requestBodyTooLarge(c, err) {
					return
				}
				log.Error().Err(err).Msg("failed to accept research file")
				serve500(c)
				return
			}
			results = append(results, src)
		}
		_ = part.Close()
	}
	if len(results) == 0 {
		http.Error(c.Response, "no files in upload (use multipart field 'files')", http.StatusBadRequest)
		return
	}
	c.JSONStatus(http.StatusCreated, map[string]any{"job_id": job.ID, "sources": results})
}

// acceptResearchFile hashes one uploaded file, stores its receipt, checks
// for exact duplicates and indexes it. A returned error means the receipt
// could not be stored; extraction/index failures are recorded on the
// receipt instead.
func acceptResearchFile(c *webContext, job *model.ResearchJob, uid uint, part *multipart.Part, rel string) (*model.ResearchSource, error) {
	name := part.FileName()
	if name == "" {
		name = "file"
	}
	data, err := io.ReadAll(io.LimitReader(part, researchMaxFileBytes+1))
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	src := &model.ResearchSource{
		JobID:        job.ID,
		UserID:       uid,
		OriginalName: truncate(path.Base(strings.ReplaceAll(name, "\\", "/")), 1000),
		RelativePath: cleanRelativePath(rel),
		SHA256:       sha,
		Size:         int64(len(data)),
		MediaType:    truncate(part.Header.Get("Content-Type"), 200),
		Status:       model.ResearchSourceAccepted,
	}
	if src.RelativePath == "" {
		src.RelativePath = src.OriginalName
	}
	tooLarge := len(data) > researchMaxFileBytes
	if tooLarge {
		// Receipt only records that the file was refused; the hash covers
		// the truncated prefix and is therefore not an identity.
		src.Size = -1
		src.SHA256 = ""
		src.Status = model.ResearchSourceFailed
		src.Error = fmt.Sprintf("file exceeds %d MiB limit", researchMaxFileBytes>>20)
	}
	artifact := &model.ResearchArtifact{UserID: uid, SHA256: sha, Size: int64(len(data)), IndexURL: researchIndexURL(sha, src.OriginalName)}
	if !tooLarge {
		var created bool
		artifact, created, err = model.FindOrCreateResearchArtifact(artifact)
		if err != nil {
			return nil, err
		}
		if !created {
			jobID := job.ID
			if prior, err := model.FirstResearchSourceForArtifact(artifact.ID, &jobID); err == nil && prior != nil {
				src.DuplicateKind = model.DuplicateInJob
				src.DuplicateOfID = &prior.ID
			} else if prior, err := model.FirstResearchSourceForArtifact(artifact.ID, nil); err == nil && prior != nil {
				src.DuplicateKind = model.DuplicateOtherJob
				src.DuplicateOfID = &prior.ID
			}
		}
		src.ArtifactID = artifact.ID
	}
	// Persist the receipt before extraction/indexing so provenance survives
	// any later failure.
	if err := model.CreateResearchSource(src); err != nil {
		return nil, err
	}
	if tooLarge {
		return src, nil
	}
	if src.DuplicateKind != "" && model.ArtifactIndexed(artifact.ID) {
		// Same bytes already searchable; nothing new to index.
		if err := model.MarkResearchSource(src.ID, model.ResearchSourceIndexed, ""); err != nil {
			return nil, err
		}
		src.Status = model.ResearchSourceIndexed
		return src, nil
	}
	status, msg := model.ResearchSourceIndexed, ""
	if err := indexResearchArtifact(c, artifact, src.OriginalName, data); err != nil {
		status, msg = model.ResearchSourceFailed, err.Error()
		log.Warn().Err(err).Str("file", src.OriginalName).Msg("research file not indexed")
	}
	if err := model.MarkResearchSource(src.ID, status, msg); err != nil {
		return nil, err
	}
	src.Status, src.Error = status, msg
	return src, nil
}

func indexResearchArtifact(c *webContext, artifact *model.ResearchArtifact, name string, data []byte) error {
	res, err := extract.New(c.Config.Indexer.PDFParser).Extract(c.Request.Context(), name, data)
	if err != nil {
		return fmt.Errorf("text extraction failed: %w", err)
	}
	if strings.TrimSpace(res.Text) == "" {
		return errors.New("no text could be extracted")
	}
	title := strings.TrimSpace(res.Title)
	if title == "" {
		title = name
	}
	_ = model.UpdateResearchArtifactMetadata(artifact.ID, string(res.Format), title)
	d := &document.Document{
		URL:    artifact.IndexURL,
		Title:  title,
		Text:   res.Text,
		Type:   document.RemoteFile,
		UserID: artifact.UserID,
	}
	if err := c.Indexer.AddContext(c.Request.Context(), d); err != nil {
		if errors.Is(err, document.ErrSensitiveContent) {
			return errors.New("rejected by sensitive-content filter")
		}
		return err
	}
	if c.Indexer.GetByURLAndUser(artifact.IndexURL, artifact.UserID) == nil {
		return errors.New("index did not acknowledge the document")
	}
	return nil
}

type researchSearchHit struct {
	ArtifactID uint   `json:"artifact_id"`
	URL        string `json:"url"`
	Title      string `json:"title"`
	Snippet    string `json:"snippet"`
}

func serveResearchSearch(c *webContext) {
	job, ok := researchJobFromPath(c)
	if !ok {
		return
	}
	q := strings.TrimSpace(c.Request.URL.Query().Get("q"))
	if q == "" {
		http.Error(c.Response, "missing q", http.StatusBadRequest)
		return
	}
	allowed, err := model.ResearchJobIndexURLs(job.ID)
	if err != nil {
		log.Error().Err(err).Msg("failed to load research job documents")
		serve500(c)
		return
	}
	hits := []researchSearchHit{}
	if len(allowed) > 0 {
		// Restrict the full-text query to research artifacts, then keep
		// only those accepted into this job.
		query := &indexer.Query{Text: "(" + q + ") domain:" + researchIndexHost, Limit: 500, IncludeText: true}
		res, err := searchIndex(c.Indexer, query, c.effectiveRules(), targetUserID(c))
		if err != nil {
			http.Error(c.Response, "search failed: "+err.Error(), http.StatusBadRequest)
			return
		}
		for _, d := range res.Documents {
			aid, ok := allowed[d.URL]
			if !ok {
				continue
			}
			hits = append(hits, researchSearchHit{ArtifactID: aid, URL: d.URL, Title: d.Title, Snippet: truncate(d.Text, 400)})
			if len(hits) >= researchSearchLimit {
				break
			}
		}
	}
	c.JSON(map[string]any{"job_id": job.ID, "query": q, "results": hits})
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
