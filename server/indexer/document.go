package indexer

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/asciimoo/hister/server/indexer/types"

	"github.com/rs/zerolog/log"
)

type Document struct {
	URL                string         `json:"url"`
	Domain             string         `json:"domain"`
	HTML               string         `json:"html"`
	Title              string         `json:"title"`
	Text               string         `json:"text"`
	Favicon            string         `json:"favicon"`
	Score              float64        `json:"score"`
	Added              int64          `json:"added"`
	Type               types.DocType  `json:"type"`
	Language           string         `json:"language"`
	UserID             uint           `json:"user_id"`
	Properties         map[string]any `json:"properties,omitempty"`
	faviconURL         string
	processed          bool
	skipSensitiveCheck bool
}

func (d *Document) extractHTML() error {
	return Extract(d)
}

func (d *Document) DownloadFavicon(userAgent string) error {
	if d.faviconURL == "" {
		d.faviconURL = fullURL(d.URL, "/favicon.ico")
	}
	if strings.HasPrefix(d.faviconURL, "data:") {
		d.Favicon = d.faviconURL
		return nil
	}
	cli := &http.Client{
		Timeout: 10 * time.Second,
	}
	req, err := http.NewRequest("GET", d.faviconURL, nil)
	req.Header.Set("User-Agent", userAgent)
	if err != nil {
		return err
	}
	resp, err := cli.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			log.Warn().Err(cerr).Msg("failed to close favicon response body")
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("invalid status code (%d)", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	d.Favicon = fmt.Sprintf("data:%s;base64,%s", resp.Header.Get("Content-Type"), base64.StdEncoding.EncodeToString(data))
	return nil
}

func (d *Document) Process(ld LanguageDetector) error {
	if d.processed {
		return nil
	}
	if ld == nil {
		ld = NewNullLanguageDetector()
	}
	if !d.skipSensitiveCheck && sensitiveContentRe != nil && sensitiveContentRe.MatchString(d.HTML) {
		log.Debug().Msg("Matching sensitive content: " + strings.Join(sensitiveContentRe.FindAllString(d.HTML, -1), ","))
		return ErrSensitiveContent
	}
	if d.URL == "" {
		return errors.New("missing URL")
	}
	pu, err := url.Parse(d.URL)
	if err != nil {
		return err
	}
	if pu.Scheme == "file" {
		return d.processFile(ld, pu)
	}
	if pu.Scheme == "" || pu.Host == "" {
		return errors.New("invalid URL: missing scheme/host")
	}
	if pu.Fragment != "" {
		pu.Fragment = ""
		d.URL = pu.String()
	}
	d.Added = time.Now().Unix()
	q := pu.Query()
	qChange := false
	for k := range q {
		if k == "utm" || strings.HasPrefix(k, "utm_") {
			qChange = true
			q.Del(k)
		}
	}
	if qChange {
		pu.RawQuery = q.Encode()
		d.URL = pu.String()
	}
	d.Type = types.Web
	d.Domain = pu.Host
	if err := d.extractHTML(); err != nil {
		return err
	}

	d.Language = ld.DetectLanguage(d.Text)

	d.processed = true
	return nil
}

func (d *Document) processFile(ld LanguageDetector, pu *url.URL) error {
	if ld == nil {
		ld = NewNullLanguageDetector()
	}
	if d.Text == "" {
		content, err := os.ReadFile(pu.Path)
		if err != nil {
			return &ReadFileError{
				Msg: err.Error(),
			}
		}
		if !utf8.Valid(content) {
			return errors.New("binary file")
		}
		d.Text = string(content)
	}
	if !d.skipSensitiveCheck && sensitiveContentRe != nil && sensitiveContentRe.MatchString(d.Text) {
		return ErrSensitiveContent
	}
	d.Type = types.Local
	d.Domain = "local"
	base := filepath.Base(pu.Path)
	parent := filepath.Base(filepath.Dir(pu.Path))
	if parent == "." || parent == "/" {
		d.Title = base
	} else {
		d.Title = parent + "/" + base
	}
	if d.Added == 0 {
		d.Added = time.Now().Unix()
	}
	d.Language = ld.DetectLanguage(d.Text)
	d.processed = true
	return nil
}

func (d *Document) ID() string {
	return GetDocID(d.UserID, d.URL)
}

func GetDocID(uid uint, url string) string {
	if uid != 0 {
		return fmt.Sprintf("%d:%s", uid, url)
	}
	return url
}
