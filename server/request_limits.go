package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/asciimoo/nogon/config"
)

const (
	// 1 MiB allows API parameters, rules, and MCP requests while bounding
	// parsing memory. Document uploads use the configured allowance below.
	maxRequestBodyBytes int64 = 1 << 20
	// 1 MiB gives rule previews room for large patterns and test URLs,
	// including JSON encoding overhead, within the general API allowance.
	maxRulePreviewBytes int64 = 1 << 20
	// 64 KiB accommodates search parameters without buffering large messages
	// on every open WebSocket. The limit covers the whole message across frames.
	maxSearchMessageBytes int64 = 64 << 10
)

// Document and batch uploads carry full content, so they use
// server.max_batch_body_size (40 MiB by default), including base64 PDF overhead.
func documentBodyLimit(cfg *config.Config) int64 {
	return cfg.Server.MaxBatchBodyBytes()
}

// withRequestBodyLimit runs before CSRF parsing, so forms and JSON requests
// share the same limit. MaxBytesReader also covers requests without a length.
func withRequestBodyLimit(handler endpointHandler, limit func(*config.Config) int64) endpointHandler {
	return func(c *webContext) {
		maxBytes := maxRequestBodyBytes
		if limit != nil {
			maxBytes = limit(c.Config)
		}
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Response, c.Request.Body, maxBytes)
		}
		if c.Request.ContentLength > maxBytes {
			requestBodyTooLarge(c, &http.MaxBytesError{Limit: maxBytes})
			return
		}
		handler(c)
	}
}

func requestBodyTooLarge(c *webContext, err error) bool {
	maxBytesErr, ok := errors.AsType[*http.MaxBytesError](err)
	if !ok {
		return false
	}
	limit := maxBytesErr.Limit
	message := fmt.Sprintf("request body exceeds the %d byte limit", limit)
	if limit%(1<<20) == 0 {
		message = fmt.Sprintf("request body exceeds the %d MiB limit", limit>>20)
	} else if limit%(1<<10) == 0 {
		message = fmt.Sprintf("request body exceeds the %d KiB limit", limit>>10)
	}
	c.JSONStatus(http.StatusRequestEntityTooLarge, map[string]any{
		"error": message, "code": "request_too_large", "limit_bytes": limit,
	})
	return true
}

func decodeRequestJSON(c *webContext, target any) bool {
	if err := json.NewDecoder(c.Request.Body).Decode(target); err != nil {
		if !requestBodyTooLarge(c, err) {
			http.Error(c.Response, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		}
		return false
	}
	return true
}

func parseRequestForm(c *webContext) bool {
	if err := c.Request.ParseForm(); err != nil {
		if !requestBodyTooLarge(c, err) {
			http.Error(c.Response, "invalid form: "+err.Error(), http.StatusBadRequest)
		}
		return false
	}
	return true
}
