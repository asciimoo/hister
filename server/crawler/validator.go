// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync"
)

// URLStatus indicates what the crawler should do with a given URL.
type URLStatus int

const (
	// URLAllow means the URL should be fetched and indexed.
	URLAllow URLStatus = iota
	// URLSkip means this URL should be skipped but crawling continues.
	URLSkip
	// URLStop means crawling should stop entirely.
	URLStop
)

// ValidatorRules configures the traversal constraints for a crawl.
// Zero values mean unlimited / no restriction for numeric fields and
// empty slices mean "allow all" for list fields.
type ValidatorRules struct {
	// MaxDepth is the maximum link depth to follow from the start URL.
	// 0 means unlimited.
	MaxDepth int
	// MaxLinks is the maximum total number of pages to visit.
	// 0 means unlimited.
	MaxLinks int
	// AllowedDomains restricts crawling to these hostnames (and their
	// subdomains). Empty means allow all domains.
	AllowedDomains []string
	// ExcludeDomains lists hostnames (and their subdomains) to skip.
	ExcludeDomains []string
	// AllowedPatterns lists regexp patterns; only URLs matching at least
	// one are followed. Empty means allow all URLs.
	AllowedPatterns []string
	// ExcludePatterns lists regexp patterns; URLs matching any are skipped.
	ExcludePatterns []string
	// NoDepth disables traversal beyond URLs explicitly inserted into the crawl
	// queue. Discovered links are ignored instead of enqueued.
	NoDepth bool
}

// Validator decides whether a URL should be crawled based on ValidatorRules.
// It is safe for concurrent use.
type Validator struct {
	rules           *ValidatorRules
	mu              sync.Mutex
	visited         int
	allowedPatterns []*regexp.Regexp
	excludePatterns []*regexp.Regexp
}

// NewValidator compiles the regexp patterns in rules and returns a Validator.
func NewValidator(rules *ValidatorRules) (*Validator, error) {
	v := &Validator{rules: rules}
	for _, p := range rules.AllowedPatterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("invalid allowed pattern %q: %w", p, err)
		}
		v.allowedPatterns = append(v.allowedPatterns, re)
	}
	for _, p := range rules.ExcludePatterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("invalid exclude pattern %q: %w", p, err)
		}
		v.excludePatterns = append(v.excludePatterns, re)
	}
	return v, nil
}

// Rules returns the ValidatorRules used to create this Validator.
// The returned pointer must not be mutated.
func (v *Validator) Rules() *ValidatorRules {
	return v.rules
}

// SetVisited pre-seeds the visited counter (used when resuming a job).
func (v *Validator) SetVisited(n int) {
	v.mu.Lock()
	v.visited = n
	v.mu.Unlock()
}

// Validate checks whether u at the given crawl depth should be visited.
// When URLAllow is returned the internal visited counter is incremented,
// so the same Validator instance tracks how many pages have been allowed.
func (v *Validator) Validate(u *url.URL, depth int) URLStatus {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.rules.MaxLinks > 0 && v.visited >= v.rules.MaxLinks {
		return URLStop
	}

	if v.rules.MaxDepth > 0 && depth > v.rules.MaxDepth {
		return URLSkip
	}

	host := u.Hostname()

	for _, d := range v.rules.ExcludeDomains {
		if host == d || strings.HasSuffix(host, "."+d) {
			return URLSkip
		}
	}

	if len(v.rules.AllowedDomains) > 0 {
		allowed := false
		for _, d := range v.rules.AllowedDomains {
			if host == d || strings.HasSuffix(host, "."+d) {
				allowed = true
				break
			}
		}
		if !allowed {
			return URLSkip
		}
	}

	rawURL := u.String()

	for _, re := range v.excludePatterns {
		if re.MatchString(rawURL) {
			return URLSkip
		}
	}

	if len(v.allowedPatterns) > 0 {
		allowed := false
		for _, re := range v.allowedPatterns {
			if re.MatchString(rawURL) {
				allowed = true
				break
			}
		}
		if !allowed {
			return URLSkip
		}
	}

	v.visited++
	return URLAllow
}

// MarshalValidatorRules serializes rules to a JSON string for storage.
func MarshalValidatorRules(rules *ValidatorRules) (string, error) {
	b, err := json.Marshal(rules)
	if err != nil {
		return "", fmt.Errorf("marshal validator rules: %w", err)
	}
	return string(b), nil
}

// UnmarshalValidatorRules deserializes rules from a JSON string.
func UnmarshalValidatorRules(s string) (*ValidatorRules, error) {
	var rules ValidatorRules
	if err := json.Unmarshal([]byte(s), &rules); err != nil {
		return nil, fmt.Errorf("unmarshal validator rules: %w", err)
	}
	return &rules, nil
}
