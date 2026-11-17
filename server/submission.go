package server

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/asciimoo/nogon/server/document"

	"github.com/rs/zerolog/log"
)

// prepareDocumentSubmission applies the request policy shared by single and
// batch adds. Ownership is assigned after normalization so submitted fields
// cannot override the caller's authorized scope. On rejection it returns the
// status for the HTTP response or the individual batch operation.
func prepareDocumentSubmission(c *webContext, d *document.Document) (int, error) {
	clearSubmittedStorageReferences(d)
	if err := validateAddDocument(d); err != nil {
		return http.StatusBadRequest, err
	}
	if shouldSkipSubmission(c, d) || c.Config.IsSameHost(d.URL) {
		log.Debug().Str("url", d.URL).Msg("skip indexing")
		return http.StatusNotAcceptable, errors.New("url skipped by rules")
	}
	d.UserID = submittedDocumentUserID(c)
	return 0, nil
}

// Storage keys identify server managed blobs, not content supplied by a client.
// Derive them from submitted HTML and favicon data for every document type.
func clearSubmittedStorageReferences(d *document.Document) {
	d.HTMLKey = ""
	d.FaviconKey = ""
}

// shouldSkipSubmission checks URL allow and skip rules unless the document carries an
// explicit override. Other document validation and authorization still apply.
func shouldSkipSubmission(c *webContext, d *document.Document) bool {
	return !d.IgnoreSkipRules() && c.effectiveRules().IsSkip(d.URL)
}

func validateAddDocument(d *document.Document) error {
	if d.URL == "" {
		return errors.New("missing url")
	}
	parsedURL, err := url.Parse(d.URL)
	if err != nil {
		return fmt.Errorf("invalid document URL: %w", err)
	}
	if d.Type != document.RemoteFile {
		if strings.EqualFold(parsedURL.Scheme, "remote-file") {
			return errors.New("remote-file URLs require the remote document type")
		}
		return nil
	}
	if parsedURL.Scheme != "remote-file" || parsedURL.Hostname() == "" {
		return errors.New("remote file URL must use the remote-file scheme and include a source host")
	}
	if parsedURL.User != nil || parsedURL.RawQuery != "" || parsedURL.Fragment != "" {
		return errors.New("remote file URL must not contain user information, a query, or a fragment")
	}
	if parsedURL.Path == "" || parsedURL.Path == "/" || !strings.HasPrefix(parsedURL.Path, "/") {
		return errors.New("remote file URL must contain an absolute file path")
	}
	if d.Text == "" && d.HTML == "" {
		return errors.New("remote file document must contain extracted text or HTML")
	}

	// All remote snapshot fields that depend on processing or server storage are
	// derived again. This also prevents a submitted Processed value from
	// bypassing URL and sensitive content checks.
	d.DocumentID = ""
	d.Domain = ""
	d.Favicon = ""
	d.Score = 0
	d.Language = ""
	d.UserID = 0
	d.AddCount = 0
	d.Processed = false
	d.ExtraDocuments = nil
	d.SkipIndexing = false
	return nil
}
