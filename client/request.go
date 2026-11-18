package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func (c *Client) request(ctx context.Context, method, path string, body io.Reader, contentType string, handle func(*http.Response) error) error {
	return c.requestWithClient(ctx, c.httpClient, method, path, body, contentType, handle)
}

// requestWithClient owns the response body. The handler runs before it is closed so a
// response validation error takes precedence over a body close error.
func (c *Client) requestWithClient(ctx context.Context, httpClient *http.Client, method, path string, body io.Reader, contentType string, handle func(*http.Response) error) (err error) {
	req, err := c.newRequest(method, path, body)
	if err != nil {
		return err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := httpClient.Do(req.WithContext(ctx))
	if err != nil {
		return err
	}
	defer closeBody(resp, &err)
	return handle(resp)
}

func (c *Client) requestJSON(ctx context.Context, method, path string, payload, target any) error {
	var body io.Reader
	var contentType string
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
		contentType = "application/json"
	}
	return c.request(ctx, method, path, body, contentType, func(resp *http.Response) error {
		return decodeResponse(resp, target)
	})
}

func (c *Client) postForm(path string, form url.Values) error {
	return c.request(context.Background(), http.MethodPost, path, strings.NewReader(form.Encode()), "application/x-www-form-urlencoded", checkStatus)
}

func decodeResponse(resp *http.Response, target any) error {
	if err := checkStatus(resp); err != nil {
		return err
	}
	if target == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func checkStatus(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	detail := strings.TrimSpace(string(body))
	errWithStatus := func(msg string) error {
		return &HTTPError{
			StatusCode: resp.StatusCode,
			Detail:     detail,
			Message:    msg,
		}
	}

	switch resp.StatusCode {
	case http.StatusUnauthorized:
		msg := "authentication required: the server requires an access token"
		if detail != "" {
			msg += " (" + detail + ")"
		}
		return errWithStatus(fmt.Sprintf("%s\nProvide one with --token / -t or set access_token in your config file", msg))
	case http.StatusForbidden:
		msg := "access denied: the token is invalid or does not have permission for this operation"
		if detail != "" {
			msg += " (" + detail + ")"
		}
		return errWithStatus(fmt.Sprintf("%s\nCheck the token with --token / -t or verify the user's permissions on the server", msg))
	case http.StatusNotFound:
		msg := "resource not found (404)"
		if detail != "" {
			msg += ": " + detail
		}
		return errWithStatus(msg)
	case http.StatusNotAcceptable:
		msg := "page skipped: this URL was rejected by the server (usually due to allow or skip rules)"
		if detail != "" {
			msg += " (" + detail + ")"
		}
		return errWithStatus(msg)
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		msg := fmt.Sprintf("server error (%d)", resp.StatusCode)
		if detail != "" {
			msg += ": " + detail
		}
		return errWithStatus(fmt.Sprintf("%s\nCheck the server logs for details", msg))
	default:
		if detail == "" {
			detail = resp.Status
		}
		return errWithStatus(fmt.Sprintf("unexpected response (%d): %s", resp.StatusCode, detail))
	}
}

func closeBody(resp *http.Response, errp *error) {
	if cerr := resp.Body.Close(); cerr != nil && *errp == nil {
		*errp = fmt.Errorf("closing response body: %w", cerr)
	}
}

// newRequest retains the legacy CLI origin so requests also work with older servers.
func (c *Client) newRequest(method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Origin", "hister://")
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	if c.accessToken != "" {
		req.Header.Set("X-Access-Token", c.accessToken)
	}
	if c.targetUserID != nil {
		req.Header.Set(targetUserIDHeader, strconv.FormatUint(uint64(*c.targetUserID), 10))
		// Older servers only recognize the compatibility header.
		req.Header.Set("X-Hister-Target-User-ID", req.Header.Get(targetUserIDHeader))
	}
	return req, nil
}
