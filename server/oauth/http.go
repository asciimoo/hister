package oauth

import (
	"fmt"
	"io"
	"net/http"
	"time"

	servererrors "github.com/asciimoo/nogon/server/errors"
)

const (
	oauthHTTPTimeout         = 10 * time.Second
	maxOAuthResponseBodySize = 1 << 20
)

var defaultHTTPClient = &http.Client{Timeout: oauthHTTPTimeout}

func oauthHTTPClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return defaultHTTPClient
}

func doRequest(client *http.Client, req *http.Request) (*http.Response, error) {
	resp, err := oauthHTTPClient(client).Do(req)
	if err != nil {
		return nil, err
	}
	if err := validateResponseStatus(resp); err != nil {
		servererrors.LogCloseBody(resp.Body)
		return nil, err
	}
	return resp, nil
}

func validateResponseStatus(resp *http.Response) error {
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected response status code: %d", resp.StatusCode)
	}
	return nil
}

func readResponseBody(resp *http.Response) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxOAuthResponseBodySize+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxOAuthResponseBodySize {
		return nil, fmt.Errorf("response body exceeds %d bytes", maxOAuthResponseBodySize)
	}
	return body, nil
}

// ReadTokenResponse reads a successful OAuth token response with a size limit.
func ReadTokenResponse(resp *http.Response) (TokenResponse, error) {
	if err := validateResponseStatus(resp); err != nil {
		return nil, err
	}
	body, err := readResponseBody(resp)
	if err != nil {
		return nil, err
	}
	return TokenResponse(body), nil
}
