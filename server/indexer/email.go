// SPDX-License-Identifier: AGPL-3.0-or-later

package indexer

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"strings"
	"time"

	"github.com/emersion/go-message"

	"github.com/asciimoo/hister/server/document"
	"github.com/asciimoo/hister/server/sanitizer"
)

type emailFileType struct{}

func (emailFileType) Match(path string) bool {
	return hasExtension(path, ".eml")
}

func (emailFileType) Prepare(d *document.Document, emailData []byte) error {
	// TODO support mbox files
	if isMboxMessage(emailData) {
		return errors.New("email parse: file looks like an mbox message, cannot parse")
	}
	ent, err := message.Read(strings.NewReader(string(emailData)))
	if err != nil {
		return fmt.Errorf("email parse: %w", err)
	}

	var plainText, htmlText string
	if err := ent.Walk(func(_ []int, e *message.Entity, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		mediaType, _, _ := e.Header.ContentType()
		if !strings.HasPrefix(mediaType, "text/") {
			// Go to next mime part if it doesn't contain text
			return nil
		}
		data, err := io.ReadAll(e.Body)
		if err != nil {
			return err
		}
		switch mediaType {
		case "text/plain":
			if plainText == "" {
				plainText = string(data)
			}
		case "text/html":
			if htmlText == "" {
				htmlText = string(data)
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("email walk: %w", err)
	}

	h := ent.Header
	subject, _ := h.Text("Subject")

	if strings.TrimSpace(plainText) != "" {
		d.Text = sanitizer.SanitizeText(plainText)
	} else if strings.TrimSpace(htmlText) != "" {
		d.Text = sanitizer.SanitizeHTML(htmlText)
	}

	if d.Text == "" {
		return errors.New("eml file contains no extractable text")
	}

	// Store the original message HTML unchanged so the extractor can render a
	// preview from it without discarding any source data.
	if strings.TrimSpace(htmlText) != "" {
		d.HTML = htmlText
	}
	if subject != "" {
		d.Title = subject
	}

	d.AddMetadata("type", "email")
	d.AddMetadata("subject", subject)
	d.AddMetadata("author", headerText(h, "From"))
	d.AddMetadata("to", headerText(h, "To"))
	d.AddMetadata("cc", headerText(h, "Cc"))
	d.AddMetadata("reply_to", headerText(h, "Reply-To"))
	return nil
}

// AddEmail renders email files to HTML, stores it in d.HTML, and stores rendered
// plain text in d.Text for full-text indexing.
func (i *Indexer) AddEmail(d *document.Document, emailData []byte) error {
	if err := (emailFileType{}).Prepare(d, emailData); err != nil {
		return err
	}
	return i.Add(d)
}

func headerText(h message.Header, key string) string {
	v, _ := h.Text(key)
	return strings.TrimSpace(v)
}

func headerDate(h message.Header) time.Time {
	raw, _ := h.Text("Date")
	if raw == "" {
		return time.Time{}
	}
	t, err := mail.ParseDate(raw)
	if err != nil {
		return time.Time{}
	}
	return t
}

// isMboxMessage reports whether the content begins with an mbox "From"
// separator line such as "From abc@abc.com Thu Jan  1 00:00:00 2026".
func isMboxMessage(data []byte) bool {
	return bytes.HasPrefix(data, []byte("From "))
}
