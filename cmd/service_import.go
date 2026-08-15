package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/asciimoo/hister/client"
	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/crawler"
	"github.com/asciimoo/hister/server/document"
	"github.com/asciimoo/hister/server/extractor"
	"github.com/asciimoo/hister/server/indexer"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

const (
	serviceImportRequestTimeout   = 30 * time.Second
	maxServiceImportResponseSize  = 64 << 20
	maxServiceImportErrorBodySize = 64 << 10
)

type serviceAPIClient struct {
	name                string
	baseURL             string
	authorizationScheme string
	tokenProvider       func() string
	tokenHint           string
	httpClient          *http.Client
}

func newServiceAPIClient(
	name string,
	instanceURL string,
	token string,
	tokenHint string,
	httpClient *http.Client,
) (*serviceAPIClient, error) {
	return newServiceAPIClientWithAuthorization(
		name,
		instanceURL,
		"Bearer",
		func() string { return token },
		tokenHint,
		httpClient,
	)
}

func newServiceAPIClientWithBearerToken(
	name string,
	instanceURL string,
	bearerTokenProvider func() string,
	tokenHint string,
	httpClient *http.Client,
) (*serviceAPIClient, error) {
	return newServiceAPIClientWithAuthorization(
		name,
		instanceURL,
		"Bearer",
		bearerTokenProvider,
		tokenHint,
		httpClient,
	)
}

func newServiceAPIClientWithAuthorization(
	name string,
	instanceURL string,
	authorizationScheme string,
	tokenProvider func() string,
	tokenHint string,
	httpClient *http.Client,
) (*serviceAPIClient, error) {
	instanceURL = strings.TrimSpace(instanceURL)
	parsed, err := url.Parse(instanceURL)
	if err != nil {
		return nil, fmt.Errorf("invalid %s instance URL: %w", name, err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("invalid %s instance URL: an http or https URL with a host is required", name)
	}
	if parsed.User != nil {
		return nil, fmt.Errorf("invalid %s instance URL: embedded credentials are not supported", name)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("invalid %s instance URL: query parameters and fragments are not supported", name)
	}
	authorizationScheme = strings.TrimSpace(authorizationScheme)
	if authorizationScheme == "" || strings.ContainsAny(authorizationScheme, " \t\r\n") {
		return nil, fmt.Errorf("invalid %s authorization scheme", name)
	}
	if tokenProvider == nil {
		return nil, fmt.Errorf("missing %s token provider", name)
	}
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: serviceImportRequestTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return errors.New("stopped after 10 redirects")
				}
				origin := via[0].URL
				if !strings.EqualFold(req.URL.Scheme, origin.Scheme) || !strings.EqualFold(req.URL.Host, origin.Host) {
					return fmt.Errorf("refusing to send the %s token to a different origin", name)
				}
				return nil
			},
		}
	}

	return &serviceAPIClient{
		name:                name,
		baseURL:             strings.TrimRight(parsed.String(), "/"),
		authorizationScheme: authorizationScheme,
		tokenProvider:       tokenProvider,
		tokenHint:           tokenHint,
		httpClient:          httpClient,
	}, nil
}

func (c *serviceAPIClient) getJSON(ctx context.Context, endpoint string, query url.Values, target any) error {
	requestURL, err := url.Parse(c.baseURL + endpoint)
	if err != nil {
		return fmt.Errorf("build %s request URL: %w", c.name, err)
	}
	requestURL.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return fmt.Errorf("create %s request: %w", c.name, err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", c.authorizationScheme+" "+c.tokenProvider())
	req.Header.Set("User-Agent", UserAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request %s data: %w", c.name, err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Debug().Err(closeErr).Msg("Failed to close service import response body")
		}
	}()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxServiceImportErrorBodySize))
		detail := strings.TrimSpace(string(body))
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return fmt.Errorf("%s authentication failed with status %d; check %s", c.name, resp.StatusCode, c.tokenHint)
		}
		if detail != "" {
			return fmt.Errorf("%s returned status %d: %s", c.name, resp.StatusCode, detail)
		}
		return fmt.Errorf("%s returned status %d", c.name, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxServiceImportResponseSize+1))
	if err != nil {
		return fmt.Errorf("read %s response: %w", c.name, err)
	}
	if len(body) > maxServiceImportResponseSize {
		return fmt.Errorf("%s response exceeds the %d MiB limit", c.name, maxServiceImportResponseSize>>20)
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("decode %s response: %w", c.name, err)
	}
	return nil
}

type serviceContentFetcher interface {
	Fetch(context.Context, string) (*document.Document, error)
}

type crawlerServiceContentFetcher struct {
	cfg     *config.CrawlerConfig
	crawler crawler.Crawler
}

func newCrawlerServiceContentFetcher(cfg *config.CrawlerConfig) *crawlerServiceContentFetcher {
	return &crawlerServiceContentFetcher{cfg: cfg}
}

func (f *crawlerServiceContentFetcher) Fetch(ctx context.Context, rawURL string) (*document.Document, error) {
	if f.crawler == nil {
		cr, err := crawler.New(f.cfg, nil)
		if err != nil {
			return nil, fmt.Errorf("initialize content crawler: %w", err)
		}
		f.crawler = cr
	}
	validator, err := crawler.NewValidator(&crawler.ValidatorRules{MaxLinks: 1})
	if err != nil {
		return nil, fmt.Errorf("initialize content validator: %w", err)
	}
	documents, err := f.crawler.Crawl(ctx, rawURL, validator)
	if err != nil {
		return nil, fmt.Errorf("download content: %w", err)
	}
	select {
	case d, ok := <-documents:
		if !ok {
			return nil, errors.New("download content: no response")
		}
		return d, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("download content: %w", ctx.Err())
	}
}

func (f *crawlerServiceContentFetcher) Close() error {
	if f.crawler == nil {
		return nil
	}
	return f.crawler.Close()
}

type serviceImportOptions struct {
	BatchSize         int
	SkipExisting      bool
	StartDate         int64
	EndDate           int64
	Label             documentLabelOverride
	FaviconDownloader func(*document.Document) error
}

type serviceImportStats struct {
	Imported int
	Skipped  int
	Errors   int
}

type serviceImportRuntime struct {
	target           *client.Client
	languageDetector document.LanguageDetector
	contentFetcher   *crawlerServiceContentFetcher
	options          serviceImportOptions
}

func newServiceImportRuntime(cmd *cobra.Command) (*serviceImportRuntime, error) {
	batchSize, _ := cmd.Flags().GetInt("batch-size")
	if batchSize < 1 || batchSize > maxImportBatchSize {
		return nil, fmt.Errorf("--batch-size must be between 1 and %d", maxImportBatchSize)
	}
	dateRange, err := parseDateRangeFlags(cmd)
	if err != nil {
		return nil, err
	}
	global, _ := cmd.Flags().GetBool("global")
	clientOptions := append([]client.Option{client.WithTimeout(0)}, targetUserIDClientOptions(cmd, global)...)
	languageDetector := document.LanguageDetector(document.NewNullLanguageDetector())
	if cfg.Indexer.DetectLanguages {
		languageDetector = document.NewLanguageDetectorFor(
			cfg.Indexer.Languages,
			cfg.Indexer.LowAccuracyLanguageDetection(),
		)
	}
	cfg.Crawler.UserAgent = UserAgent
	applyCrawlerBackendFlags(cmd)
	skipExisting, _ := cmd.Flags().GetBool("skip-existing")

	return &serviceImportRuntime{
		target:           newClient(clientOptions...),
		languageDetector: languageDetector,
		contentFetcher:   newCrawlerServiceContentFetcher(&cfg.Crawler),
		options: serviceImportOptions{
			BatchSize:    batchSize,
			SkipExisting: skipExisting,
			StartDate:    dateRange.From,
			EndDate:      dateRange.To,
			Label:        newDocumentLabelOverride(cmd),
			FaviconDownloader: func(d *document.Document) error {
				return d.DownloadFavicon(UserAgent)
			},
		},
	}, nil
}

func (r *serviceImportRuntime) Close() error {
	return r.contentFetcher.Close()
}

type serviceContentRequest struct {
	URL         string
	HTML        string
	PrefixText  string
	SourceTitle string
}

type serviceImportBuffer struct {
	source           string
	target           *client.Client
	languageDetector document.LanguageDetector
	contentFetcher   serviceContentFetcher
	options          serviceImportOptions
	stats            serviceImportStats
	documents        []*document.Document
}

func newServiceImportBuffer(
	source string,
	target *client.Client,
	languageDetector document.LanguageDetector,
	contentFetcher serviceContentFetcher,
	options serviceImportOptions,
) (*serviceImportBuffer, error) {
	if options.BatchSize < 1 || options.BatchSize > maxImportBatchSize {
		return nil, fmt.Errorf("batch size must be between 1 and %d", maxImportBatchSize)
	}
	return &serviceImportBuffer{
		source:           source,
		target:           target,
		languageDetector: languageDetector,
		contentFetcher:   contentFetcher,
		options:          options,
		documents:        make([]*document.Document, 0, options.BatchSize),
	}, nil
}

func (b *serviceImportBuffer) Add(ctx context.Context, d *document.Document, contentRequest *serviceContentRequest) {
	if (b.options.StartDate != 0 && d.Added < b.options.StartDate) || (b.options.EndDate != 0 && d.Added > b.options.EndDate) {
		log.Debug().Str("source", b.source).Str("url", d.URL).Int64("added", d.Added).Msg("Skipping service record outside of date range")
		b.stats.Skipped++
		return
	}
	if b.options.SkipExisting {
		exists, err := b.target.DocumentExists(d.URL)
		if err != nil {
			log.Warn().Err(err).Str("source", b.source).Str("url", d.URL).Msg("Failed to check if document exists, skipping")
			b.stats.Errors++
			return
		}
		if exists {
			log.Debug().Str("source", b.source).Str("url", d.URL).Msg("Document already exists, skipping")
			b.stats.Skipped++
			return
		}
	}
	if contentRequest != nil {
		if err := loadServiceContent(ctx, b.contentFetcher, d, *contentRequest, b.languageDetector); err != nil {
			log.Warn().Err(err).Str("source", b.source).Str("url", d.URL).Msg("Failed to load service content, importing bookmark metadata only")
			b.stats.Errors++
		}
	}
	if d.Favicon == "" && b.options.FaviconDownloader != nil {
		if err := b.options.FaviconDownloader(d); err != nil {
			log.Debug().Err(err).Str("source", b.source).Str("url", d.URL).Msg("Failed to download service import favicon")
		}
	}
	b.options.Label.apply(d, b.source)

	b.documents = append(b.documents, d)
	if len(b.documents) == b.options.BatchSize {
		b.Flush()
	}
}

func (b *serviceImportBuffer) Flush() {
	if len(b.documents) == 0 {
		return
	}
	imported, errCount := addDocumentBatch(b.target, b.documents)
	b.stats.Imported += imported
	b.stats.Errors += errCount
	b.documents = b.documents[:0]
}

func combineImportText(values ...string) string {
	parts := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		parts = append(parts, value)
	}
	return strings.Join(parts, "\n\n")
}

func downloadServiceContent(
	ctx context.Context,
	contentFetcher serviceContentFetcher,
	d *document.Document,
	request serviceContentRequest,
	languageDetector document.LanguageDetector,
) error {
	fetched, err := contentFetcher.Fetch(ctx, request.URL)
	if err != nil {
		return err
	}
	return applyServiceContent(d, fetched, request.PrefixText, request.SourceTitle, languageDetector)
}

func loadServiceContent(
	ctx context.Context,
	contentFetcher serviceContentFetcher,
	d *document.Document,
	request serviceContentRequest,
	languageDetector document.LanguageDetector,
) error {
	var embeddedErr error
	if strings.TrimSpace(request.HTML) != "" {
		embeddedErr = applyServiceHTML(
			d,
			request.URL,
			request.HTML,
			request.PrefixText,
			request.SourceTitle,
			languageDetector,
		)
		if embeddedErr == nil {
			return nil
		}
	}
	if contentFetcher != nil {
		return downloadServiceContent(ctx, contentFetcher, d, request, languageDetector)
	}
	return embeddedErr
}

func applyServiceHTML(
	d *document.Document,
	rawURL string,
	htmlContent string,
	prefixText string,
	sourceTitle string,
	languageDetector document.LanguageDetector,
) error {
	fetched := &document.Document{URL: rawURL, HTML: htmlContent}
	return applyServiceContent(d, fetched, prefixText, sourceTitle, languageDetector)
}

func applyServiceContent(
	d *document.Document,
	fetched *document.Document,
	prefixText string,
	sourceTitle string,
	languageDetector document.LanguageDetector,
) error {
	if fetched == nil {
		return errors.New("downloaded page is missing")
	}
	if err := fetched.Process(languageDetector, extractor.Extract); err != nil {
		return fmt.Errorf("process downloaded content: %w", err)
	}
	if strings.TrimSpace(fetched.Text) == "" {
		return errors.New("downloaded page has no extractable content")
	}

	d.CopyFaviconFrom(fetched)
	d.HTML = fetched.HTML
	d.Text = combineImportText(prefixText, fetched.Text)
	if sourceTitle != "" {
		d.Title = sourceTitle
	} else if fetched.Title != "" {
		d.Title = fetched.Title
	}
	if d.Metadata == nil {
		d.Metadata = make(map[string]any)
	}
	for key, value := range fetched.Metadata {
		if _, exists := d.Metadata[key]; !exists {
			d.Metadata[key] = value
		}
	}
	d.Language = languageDetector.DetectLanguage(d.Text)
	return nil
}

func setServiceFavicon(d *document.Document, favicon string) {
	favicon = strings.TrimSpace(favicon)
	if favicon == "" {
		return
	}
	if strings.HasPrefix(favicon, "data:") {
		d.Favicon = favicon
		return
	}
	if faviconURL, err := url.Parse(favicon); err == nil && !faviconURL.IsAbs() {
		if documentURL, baseErr := url.Parse(d.URL); baseErr == nil {
			favicon = documentURL.ResolveReference(faviconURL).String()
		}
	}
	d.SetFaviconURL(favicon)
}

func latestServiceUpdated(target *client.Client, source string) (int64, error) {
	result, err := target.Search(&indexer.Query{
		Text:  "metadata.source:" + source,
		Limit: 1,
		Sort:  "date",
	})
	if err != nil {
		return 0, err
	}
	if len(result.Documents) == 0 {
		return 0, nil
	}
	return result.Documents[0].Updated, nil
}

func parseServiceTime(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05-0700", "2006-01-02"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.Unix()
		}
	}
	return 0
}

func serviceAPIToken(cmd *cobra.Command, envName string) string {
	if cmd.Flags().Changed("api-token") {
		token, _ := cmd.Flags().GetString("api-token")
		return strings.TrimSpace(token)
	}
	return strings.TrimSpace(os.Getenv(envName))
}
