package qutebrowser

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/asciimoo/nogon/client"
)

const (
	defaultDevToolsURL = "http://127.0.0.1:9222"
	defaultNogonURL    = "http://127.0.0.1:4433/"
)

// DocumentSubmitter sends an extracted page to Nogon.
type DocumentSubmitter interface {
	AddDocumentJSON(*client.Document) error
}

// Options configures qutebrowser monitoring and page submission.
type Options struct {
	DevToolsURL     string
	NogonURL        string
	Label           string
	InitialDelay    time.Duration
	Debounce        time.Duration
	MaxWait         time.Duration
	RetryDelay      time.Duration
	ReconnectDelay  time.Duration
	CommandTimeout  time.Duration
	RequestTimeout  time.Duration
	MaxFaviconBytes int64
	UserAgent       string
}

type normalizedOptions struct {
	devToolsURL     *url.URL
	nogonURL        *url.URL
	label           string
	initialDelay    time.Duration
	debounce        time.Duration
	maxWait         time.Duration
	retryDelay      time.Duration
	reconnectDelay  time.Duration
	commandTimeout  time.Duration
	requestTimeout  time.Duration
	maxFaviconBytes int64
	userAgent       string
}

// DefaultOptions returns the default qutebrowser companion settings.
func DefaultOptions() Options {
	return Options{
		DevToolsURL:     defaultDevToolsURL,
		NogonURL:        defaultNogonURL,
		InitialDelay:    time.Second,
		Debounce:        10 * time.Second,
		MaxWait:         30 * time.Second,
		RetryDelay:      30 * time.Second,
		ReconnectDelay:  3 * time.Second,
		CommandTimeout:  10 * time.Second,
		RequestTimeout:  30 * time.Second,
		MaxFaviconBytes: 1024 * 1024,
		UserAgent:       "Nogon qutebrowser companion",
	}
}

func normalizeOptions(input Options) (normalizedOptions, error) {
	var normalized normalizedOptions
	var err error

	normalized.devToolsURL, err = parseHTTPURL("DevTools", input.DevToolsURL)
	if err != nil {
		return normalizedOptions{}, err
	}
	if !isLoopbackHost(normalized.devToolsURL.Hostname()) {
		return normalizedOptions{}, errors.New(
			"the DevTools endpoint must use localhost or a loopback IP address",
		)
	}
	normalized.nogonURL, err = parseHTTPURL("Nogon", input.NogonURL)
	if err != nil {
		return normalizedOptions{}, err
	}
	if !strings.HasSuffix(normalized.nogonURL.Path, "/") {
		normalized.nogonURL.Path += "/"
	}
	normalized.nogonURL.RawQuery = ""
	normalized.nogonURL.Fragment = ""

	switch {
	case input.InitialDelay < 0:
		return normalizedOptions{}, errors.New("initial delay cannot be negative")
	case input.Debounce < 0:
		return normalizedOptions{}, errors.New("debounce cannot be negative")
	case input.MaxWait < 0:
		return normalizedOptions{}, errors.New("maximum wait cannot be negative")
	case input.RetryDelay < 0:
		return normalizedOptions{}, errors.New("retry delay cannot be negative")
	case input.ReconnectDelay <= 0:
		return normalizedOptions{}, errors.New("reconnect delay must be positive")
	case input.CommandTimeout <= 0:
		return normalizedOptions{}, errors.New("command timeout must be positive")
	case input.RequestTimeout <= 0:
		return normalizedOptions{}, errors.New("request timeout must be positive")
	case input.MaxFaviconBytes <= 0:
		return normalizedOptions{}, errors.New("maximum favicon size must be positive")
	}

	normalized.label = input.Label
	normalized.initialDelay = input.InitialDelay
	normalized.debounce = input.Debounce
	normalized.maxWait = input.MaxWait
	normalized.retryDelay = input.RetryDelay
	normalized.reconnectDelay = input.ReconnectDelay
	normalized.commandTimeout = input.CommandTimeout
	normalized.requestTimeout = input.RequestTimeout
	normalized.maxFaviconBytes = input.MaxFaviconBytes
	normalized.userAgent = input.UserAgent
	return normalized, nil
}

func parseHTTPURL(name, rawURL string) (*url.URL, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid %s URL: %w", name, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("invalid %s URL: must use HTTP or HTTPS", name)
	}
	if u.Hostname() == "" || u.User != nil {
		return nil, fmt.Errorf("invalid %s URL: must have a host and no user information", name)
	}
	return u, nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func newHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout}
}

func closeResponseBody(response *http.Response, errp *error) {
	if err := response.Body.Close(); err != nil && *errp == nil {
		*errp = fmt.Errorf("close HTTP response body: %w", err)
	}
}
