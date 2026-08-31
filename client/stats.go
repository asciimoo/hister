package client

import (
	"encoding/json"
)

// DomainStat is the storage a single domain accounts for.
//
// Mirrors indexer.DomainStat rather than importing it, so a client does not pull in the whole
// indexer package — the same reason the other response types in this package are declared here.
type DomainStat struct {
	Domain string `json:"domain"`
	Pages  uint64 `json:"pages"`
	// TextBytes is an estimate of the domain's share of the search index, not a measurement.
	TextBytes uint64 `json:"text_bytes"`
	// HTMLBytes and FaviconBytes are exact sizes of files on disk, counting only blobs this
	// domain alone references — what deleting it would actually release.
	HTMLBytes    uint64 `json:"html_bytes"`
	FaviconBytes uint64 `json:"favicon_bytes"`
	// SharedBytes is data this domain references but shares with others, so deleting it alone
	// releases none of it. Reported so a domain with nothing but shared content does not appear
	// to hold no data at all.
	SharedBytes uint64 `json:"shared_bytes"`
	TotalBytes  uint64 `json:"total_bytes"`
}

type domainStatsResponse struct {
	Domains []DomainStat `json:"domains"`
}

// DomainStats fetches per-domain storage from the server, largest first.
func (c *Client) DomainStats() (_ []DomainStat, err error) {
	req, err := c.newRequest("GET", "/api/stats/domains", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer closeBody(resp, &err)
	if err := checkStatus(resp); err != nil {
		return nil, err
	}
	var data domainStatsResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	return data.Domains, nil
}
