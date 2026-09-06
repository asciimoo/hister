package cmd

import (
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/asciimoo/hister/client"
	"github.com/asciimoo/hister/server/document"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

const (
	shaarliSecretEnv           = "HISTER_IMPORT_SHAARLI_SECRET"
	shaarliSourceMetadataValue = "shaarli"
	shaarliPageSize            = 100
)

var errShaarliMissingURL = errors.New("shaarli record has no URL or short URL")

type shaarliLink struct {
	ID          int64    `json:"id"`
	URL         string   `json:"url"`
	ShortURL    string   `json:"shorturl"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Private     bool     `json:"private"`
	Created     string   `json:"created"`
	Updated     string   `json:"updated"`
}

type shaarliHistoryEvent struct {
	Event    string `json:"event"`
	Datetime string `json:"datetime"`
	ID       int64  `json:"id"`
}

type shaarliClient struct {
	*serviceAPIClient
	updatedAfter int64
	pageSize     int
	now          func() time.Time
}

var importShaarliCmd = &cobra.Command{
	Use:   "shaarli INSTANCE_URL",
	Short: "Import bookmarks from Shaarli",
	Long: `Import bookmarks, notes, and searchable page content from a Shaarli instance.

Set the API secret from the Shaarli administration page with
HISTER_IMPORT_SHAARLI_SECRET or --api-token. Hister generates a short lived
HS512 JWT for every API request. The API secret itself is never sent.

Shaarli stores bookmark descriptions rather than complete page content. Hister
downloads linked pages using the configured crawler backend. Override the
backend with --backend and --backend-option.

The global --token flag remains the access token for the destination Hister server.`,
	Args: cobra.ExactArgs(1),
	PreRun: func(_ *cobra.Command, _ []string) {
		initExtractor()
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true
		secret := serviceAPIToken(cmd, shaarliSecretEnv)
		if secret == "" {
			return fmt.Errorf("Shaarli API secret is required; set %s or use --api-token", shaarliSecretEnv)
		}

		runtime, err := newServiceImportRuntime(cmd)
		if err != nil {
			return err
		}
		defer func() {
			if err := runtime.Close(); err != nil {
				log.Warn().Err(err).Msg("Shaarli content crawler close error")
			}
		}()
		source, err := newShaarliClient(args[0], secret, nil)
		if err != nil {
			return err
		}
		updatedAfter, err := latestServiceUpdated(runtime.target, shaarliSourceMetadataValue)
		if err != nil {
			return fmt.Errorf("Failed to find the latest Shaarli import: %w", err)
		}
		source.updatedAfter = updatedAfter

		stats, err := importShaarli(
			cmd.Context(),
			source,
			runtime.target,
			runtime.languageDetector,
			runtime.contentFetcher,
			runtime.options,
		)
		if err != nil {
			err = fmt.Errorf("Shaarli import failed: %w", err)
		}
		return finishImport(cmd, stats, err)
	},
}

func newShaarliClient(instanceURL, secret string, httpClient *http.Client) (*shaarliClient, error) {
	source := &shaarliClient{
		pageSize: shaarliPageSize,
		now:      time.Now,
	}
	apiClient, err := newServiceAPIClientWithBearerToken(
		shaarliSourceMetadataValue,
		instanceURL,
		func() string { return generateShaarliJWT(secret, source.now()) },
		shaarliSecretEnv+" or --api-token",
		httpClient,
	)
	if err != nil {
		return nil, err
	}
	source.serviceAPIClient = apiClient
	return source, nil
}

func generateShaarliJWT(secret string, issuedAt time.Time) string {
	encode := base64.RawURLEncoding.EncodeToString
	header := encode([]byte(`{"typ":"JWT","alg":"HS512"}`))
	payload := encode([]byte(`{"iat":` + strconv.FormatInt(issuedAt.Unix(), 10) + `}`))
	content := header + "." + payload
	signature := hmac.New(sha512.New, []byte(secret))
	_, _ = signature.Write([]byte(content))
	return content + "." + encode(signature.Sum(nil))
}

func (c *shaarliClient) links(ctx context.Context, offset int) ([]shaarliLink, error) {
	query := make(url.Values)
	query.Set("offset", strconv.Itoa(offset))
	query.Set("limit", strconv.Itoa(c.pageSize))
	query.Set("visibility", "all")
	var links []shaarliLink
	if err := c.getJSON(ctx, "/api/v1/links", query, &links); err != nil {
		return nil, err
	}
	return links, nil
}

func (c *shaarliClient) history(ctx context.Context, offset int) ([]shaarliHistoryEvent, error) {
	query := make(url.Values)
	query.Set("since", time.Unix(c.updatedAfter, 0).UTC().Format(time.RFC3339))
	query.Set("offset", strconv.Itoa(offset))
	query.Set("limit", strconv.Itoa(c.pageSize))
	var events []shaarliHistoryEvent
	if err := c.getJSON(ctx, "/api/v1/history", query, &events); err != nil {
		return nil, err
	}
	return events, nil
}

func (c *shaarliClient) link(ctx context.Context, id int64) (*shaarliLink, error) {
	var link shaarliLink
	if err := c.getJSON(ctx, "/api/v1/links/"+strconv.FormatInt(id, 10), make(url.Values), &link); err != nil {
		return nil, err
	}
	return &link, nil
}

func (c *shaarliClient) walkLinks(ctx context.Context, visit func(shaarliLink)) error {
	if c.pageSize < 1 {
		return errors.New("shaarli page size must be positive")
	}
	if c.updatedAfter != 0 {
		return c.walkChangedLinks(ctx, visit)
	}
	for offset := 0; ; {
		links, err := c.links(ctx, offset)
		if err != nil {
			return err
		}
		for _, link := range links {
			visit(link)
		}
		if len(links) < c.pageSize {
			return nil
		}
		nextOffset := offset + len(links)
		if nextOffset <= offset {
			return errors.New("shaarli pagination offset did not advance")
		}
		offset = nextOffset
	}
}

func (c *shaarliClient) walkChangedLinks(ctx context.Context, visit func(shaarliLink)) error {
	ids := make([]int64, 0)
	seen := make(map[int64]struct{})
	deleted := make(map[int64]struct{})
	for offset := 0; ; {
		events, err := c.history(ctx, offset)
		if err != nil {
			return err
		}
		for _, event := range events {
			switch strings.ToUpper(strings.TrimSpace(event.Event)) {
			case "CREATED", "UPDATED":
				if event.ID == 0 {
					continue
				}
				if _, exists := seen[event.ID]; !exists {
					seen[event.ID] = struct{}{}
					ids = append(ids, event.ID)
				}
			case "DELETED":
				if event.ID != 0 {
					deleted[event.ID] = struct{}{}
				}
			}
		}
		if len(events) < c.pageSize {
			break
		}
		nextOffset := offset + len(events)
		if nextOffset <= offset {
			return errors.New("shaarli history pagination offset did not advance")
		}
		offset = nextOffset
	}

	for _, id := range ids {
		if _, wasDeleted := deleted[id]; wasDeleted {
			continue
		}
		link, err := c.link(ctx, id)
		if err != nil {
			return fmt.Errorf("fetch changed shaarli link %d: %w", id, err)
		}
		visit(*link)
	}
	return nil
}

func importShaarli(
	ctx context.Context,
	source *shaarliClient,
	target *client.Client,
	languageDetector document.LanguageDetector,
	contentFetcher serviceContentFetcher,
	options serviceImportOptions,
) (serviceImportStats, error) {
	buffer, err := newServiceImportBuffer(
		shaarliSourceMetadataValue,
		target,
		languageDetector,
		contentFetcher,
		options,
	)
	if err != nil {
		return serviceImportStats{}, err
	}

	err = source.walkLinks(ctx, func(link shaarliLink) {
		d, contentRequest, err := source.document(link, languageDetector)
		if errors.Is(err, errShaarliMissingURL) {
			log.Debug().Int64("shaarli_id", link.ID).Msg("Skipping Shaarli record without a URL or short URL")
			buffer.stats.Skipped++
			return
		}
		if err != nil {
			log.Warn().Err(err).Int64("shaarli_id", link.ID).Msg("Failed to convert Shaarli record, skipping")
			buffer.stats.Errors++
			return
		}
		buffer.Add(ctx, d, contentRequest)
	})
	buffer.Flush()
	return buffer.stats, err
}

func (c *shaarliClient) document(
	link shaarliLink,
	languageDetector document.LanguageDetector,
) (*document.Document, *serviceContentRequest, error) {
	rawURL, note, err := c.documentURL(link)
	if err != nil {
		return nil, nil, err
	}
	added := parseServiceTime(link.Created)
	updated := parseServiceTime(link.Updated)
	if updated == 0 {
		updated = added
	}
	title := strings.TrimSpace(link.Title)
	d := &document.Document{
		URL:      rawURL,
		Title:    title,
		Text:     strings.TrimSpace(link.Description),
		Added:    added,
		Updated:  updated,
		Metadata: shaarliMetadata(link, note),
	}
	if err := d.Process(languageDetector, nil); err != nil {
		return nil, nil, err
	}
	if title == "" {
		d.Title = d.URL
	}
	if updated != 0 {
		d.Updated = updated
	}

	if note {
		return d, nil, nil
	}
	return d, &serviceContentRequest{
		URL:         rawURL,
		PrefixText:  link.Description,
		SourceTitle: title,
	}, nil
}

func (c *shaarliClient) documentURL(link shaarliLink) (string, bool, error) {
	rawURL := strings.TrimSpace(link.URL)
	note := rawURL == "" || strings.HasPrefix(rawURL, "?")
	if rawURL == "" {
		shortURL := strings.TrimSpace(link.ShortURL)
		if shortURL == "" {
			return "", false, errShaarliMissingURL
		}
		rawURL = "?" + url.QueryEscape(shortURL)
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", false, fmt.Errorf("parse shaarli URL: %w", err)
	}
	if !parsed.IsAbs() {
		base, err := url.Parse(strings.TrimRight(c.baseURL, "/") + "/")
		if err != nil {
			return "", false, fmt.Errorf("parse shaarli instance URL: %w", err)
		}
		parsed = base.ResolveReference(parsed)
	}
	if !note && shaarliURLIsPermalink(c.baseURL, parsed, link.ShortURL) {
		note = true
	}
	return parsed.String(), note, nil
}

func shaarliURLIsPermalink(instanceURL string, linkURL *url.URL, shortURL string) bool {
	shortURL = strings.TrimSpace(shortURL)
	if shortURL == "" || linkURL.RawQuery != shortURL {
		return false
	}
	base, err := url.Parse(strings.TrimRight(instanceURL, "/") + "/")
	if err != nil {
		return false
	}
	return strings.EqualFold(base.Scheme, linkURL.Scheme) &&
		strings.EqualFold(base.Host, linkURL.Host) &&
		strings.TrimRight(base.Path, "/") == strings.TrimRight(linkURL.Path, "/")
}

func shaarliMetadata(link shaarliLink, note bool) map[string]any {
	metadata := map[string]any{
		"source":          shaarliSourceMetadataValue,
		"shaarli_id":      link.ID,
		"shaarli_private": link.Private,
		"shaarli_note":    note,
	}
	if shortURL := strings.TrimSpace(link.ShortURL); shortURL != "" {
		metadata["shaarli_shorturl"] = shortURL
	}
	if description := strings.TrimSpace(link.Description); description != "" {
		metadata["description"] = description
	}
	if len(link.Tags) > 0 {
		tags := make([]string, 0, len(link.Tags))
		for _, tag := range link.Tags {
			if tag = strings.TrimSpace(tag); tag != "" {
				tags = append(tags, tag)
			}
		}
		if len(tags) > 0 {
			metadata["shaarli_tags"] = tags
		}
	}
	return metadata
}
