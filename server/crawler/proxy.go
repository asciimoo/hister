// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// parseProxyURL validates the proxy formats supported by Nogon.
func parseProxyURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	proxyURL, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy URL: %w", err)
	}
	proxyURL.Scheme = strings.ToLower(proxyURL.Scheme)
	if proxyURL.Scheme != "http" && proxyURL.Scheme != "socks5" {
		return nil, fmt.Errorf("proxy URL must use http:// or socks5://, got %q", proxyURL.Scheme)
	}
	if proxyURL.Hostname() == "" {
		return nil, fmt.Errorf("proxy URL must include a host")
	}
	if proxyURL.Path != "" && proxyURL.Path != "/" {
		return nil, fmt.Errorf("proxy URL must not include a path")
	}
	if proxyURL.RawQuery != "" || proxyURL.Fragment != "" {
		return nil, fmt.Errorf("proxy URL must not include a query or fragment")
	}
	if proxyURL.User != nil {
		return nil, fmt.Errorf("proxy URL credentials are not supported")
	}

	return proxyURL, nil
}

// transportWithProxy clones the default transport, overriding the proxy when set.
func transportWithProxy(proxyURL *url.URL) *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if proxyURL != nil {
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	return transport
}

func bidiProxyCapability(proxyURL *url.URL) map[string]any {
	proxy := map[string]any{
		"proxyType": "manual",
	}
	address := proxyURL.Host
	if proxyURL.Scheme == "socks5" {
		proxy["socksProxy"] = address
		proxy["socksVersion"] = 5
	} else {
		proxy["httpProxy"] = address
		proxy["sslProxy"] = address
	}
	return proxy
}
