---
date: '2026-02-25T00:00:00+00:00'
draft: false
title: 'Query Language Guide'
description: 'Build precise searches with fields, phrases, dates, wildcards, negation, sorting, and alternatives.'
---

Hister provides a query language that allows you to search through your content with precision. This guide explains the different query types and search techniques available.

## Basic Search

Simply type any word to search across all fields:

```textplain
hister
```

This searches for "hister" in the title, text content, URL, and domain of all indexed pages.

## Quoted Phrases

Use double quotes to search for exact phrases:

```textplain
"privacy policy"
```

This finds pages containing the exact phrase "privacy policy" (not just pages with both words separately).

**Examples:**

```textplain
"self-hosted applications"
"data privacy regulations"
"end-to-end encryption"
```

## Field-Specific Searches

You can search within specific fields using the `field:value` syntax:

### Available Fields

- **title:** - Search in page titles only
- **text:** - Search in page content only
- **url:** - Search in URLs only (bare file paths without `://` are automatically resolved to absolute `file://` URLs)
- **url_re:** Search URLs with a Go regular expression
- **domain:** - Search in domain names only
- **label:** - Search in document labels only
- **language:** - Filter by detected language (e.g., `en`, `de`, `fr`. Use `unknown` for languages Hister doesn't support)
- **metadata.KEY:** - Match an exact metadata value, such as `metadata.source:linkding`
- **type:** - Filter by document type (`web` for websites, `file` or `local` for local files)
- **visits:** - Filter by visit count, with exact values (`visits:1`), bounded ranges (`visits:2..4`), or open ranges (`visits:10..`)
- **added:** Filter by when a document was first added, using a relative duration such as `added:>90d` or an absolute date such as `added:>=2026-04-01`
- **updated:** Filter by when a document was last updated, using a relative duration such as `updated:>90d` or an absolute date such as `updated:<2026-05-01`. Relative durations support seconds (`s`), minutes (`m`), hours (`h`), days (`d`), and weeks (`w`). Absolute dates use `YYYY-MM-DD` and midnight UTC
- **user_id:** - Filter by user ID (admin use; e.g., `user_id:3`)

**Examples:**

```textplain
title:encryption
```

Finds pages with "encryption" in the title.

```textplain
domain:github.com
```

Finds all pages from github.com.

```textplain
url:*/security/*
```

Finds pages with "security" in the URL path.

```textplain
text:"GDPR compliance"
```

Finds pages with "GDPR compliance" in the body text.

```textplain
language:en
```

Finds pages detected as English language.

```textplain
label:research
```

Finds documents with a matching label.

```textplain
metadata.source:linkding
```

Finds documents imported from Linkding.

```textplain
type:file
```

Finds both watched local files and imported remote file snapshots.

```textplain
type:local
```

Finds watched files that remain accessible to the server filesystem.

```textplain
type:remote
```

Finds remote file snapshots created with `hister import file` or submitted as prepared documents through `/api/add`.

```textplain
type:web
```

Finds web pages (indexed from URLs).

```textplain
visits:10..
```

Finds pages visited 10 or more times.

```textplain
updated:>90d
```

Finds pages that have not been updated in more than 90 days. Use `added:>90d` to compare against the time a page was first added. The comparison operators `<`, `<=`, `>`, and `>=` are supported.

```textplain
updated:>=2026-04-01 updated:<2026-05-01
```

Finds pages updated during April 2026. Absolute comparisons apply directly to the timestamp. Using the following day as an exclusive upper bound includes the entire final day. Relative comparisons apply to elapsed time, so `updated:>90d` means older than 90 days.

```textplain
user_id:3
```

Finds all documents belonging to user with ID 3 (admin only).

```textplain
url:/home/user/documents/report.pdf
```

Finds the local file at that path (resolved to an absolute `file://` URL automatically).

### Privacy & Security Examples

```textplain
title:privacy domain:mozilla.org
title:"security audit" text:vulnerability
url:*/privacy-policy
domain:privacyguides.org text:encryption
language:en type:web
visits:2..4 domain:example.com
added:<7d domain:example.com
user_id:3 domain:example.com
```

## Sorting Results

Use a `sort:` directive anywhere in the query to control result order. The directive controls
ordering and is not matched against document content.

```textplain
golang sort:date
```

`sort:relevance` uses the default relevance order. `sort:date` shows the most recently updated
documents first. `sort:visits` shows the most visited documents first. `sort:domain` groups results
by domain.

Prefix any sort value with a minus sign to reverse its direction. For example, `sort:-date` shows
the oldest documents first, `sort:-visits` shows the least visited documents first, and
`sort:-domain` orders domains from Z to A. `sort:-relevance` places the least relevant results
first.

```textplain
golang sort:-date
```

A query containing only a sort directive matches all documents:

```textplain
sort:date
```

When a query contains more than one valid sort directive, the final directive takes precedence.
Quoted text such as `"sort:date"` remains searchable text rather than changing the result order.

## Wildcard Searches

Use asterisks (`*`) for wildcard matching:

```textplain
secur*
```

Matches: security, secure, securing, etc.

```textplain
*privacy*
```

Matches: privacy, myprivacy, privacytools, etc.

**Field-specific wildcards:**

```textplain
domain:*.github.io
url:*/docs/*
title:*firewall*
```

URL wildcard matching ignores letter case, so `url:*readme*` also matches a path ending in `README.md`.

## URL Regular Expressions

Use `url_re:` to match normalized page addresses with a Go regular expression. The expression uses
the same `MatchString` behavior as indexing rules. It can therefore match any part of the URL unless
you anchor it with `^` or `$`.

```textplain
url_re:^https?://([^/]+\.)?example\.com/private/
```

Quotes are optional. Use them when the expression contains whitespace.

```textplain
url_re:"^file:///.*/My Documents/.*\.pdf$"
```

Regular expression matching is case sensitive unless the expression enables case folding, for
example with `(?i)`.

```textplain
url_re:(?i)/readme\.md$
```

## Negation

Prefix terms with a minus sign (`-`) to exclude results:

```textplain
privacy -facebook
```

Finds pages about privacy but excludes results containing "facebook".

```textplain
encryption title:-tutorial
```

Finds pages about encryption but not those with "tutorial" in the title.

**Field-specific negation:**

```textplain
security -domain:facebook.com
title:hister -url:*/issues/*
privacy -"social media"
-language:en
-type:file
```

## Alternation Expressions

Use parentheses with pipe (`|`) to create OR conditions:

```textplain
(security|privacy|encryption)
```

Finds pages containing any of these terms.

```textplain
title:(firewall|vpn|proxy)
```

Finds pages with firewall, VPN, or proxy in the title.

### Combining with Other Queries

```textplain
"data protection" (GDPR|CCPA|HIPAA)
```

Finds pages about data protection mentioning any of these regulations.

```textplain
domain:(github.com|gitlab.com) title:security
```

Finds security-related pages from GitHub or GitLab.

## Combining Query Types

You can combine all query types for powerful searches:

```textplain
title:encryption "end-to-end" domain:(signal.org|whatsapp.com) -deprecated
```

This finds pages where:

- "encryption" appears in the title
- Contains the phrase "end-to-end"
- From signal.org OR whatsapp.com domains
- Does NOT contain "deprecated"

### Real-World Examples

**Finding privacy tools:**

```textplain
(privacy|security) tools "open source" -commercial type:web
```

**Research on specific topics:**

```textplain
"threat model" (encryption|authentication|authorization) -tutorial language:en
```

**Documentation searches:**

```textplain
title:(setup|installation|configuration) domain:(*.io|*.dev) hister
```

**Local code documentation:**

```textplain
type:file (README|documentation) language:en
```

**Security vulnerabilities:**

```textplain
(CVE|vulnerability|exploit) (2024|2025|2026) -"not affected" type:web
```

**Self-hosting resources:**

```textplain
"self-hosted" (docker|kubernetes|compose) title:(guide|tutorial)
```

## Tips

### 1. Case Insensitivity

All searches are case-insensitive:

```textplain
Privacy = privacy = PRIVACY
```

### 2. Wildcards and Performance

- Leading wildcards (`*term`) are slower than trailing wildcards (`term*`)
- Starting query with `*` immediately tries to find every document, that can lead to performance issues
- Use field-specific wildcards when possible for better performance

### 3. Empty Alternations

Alternations must contain at least one option:

```textplain
()           # Invalid
(a)          # Valid - single option
(a|b)        # Valid - multiple options
```

## Query Best Practices

### Start Broad, Then Narrow

```textplain
# Start with:
encryption

# Refine to:
encryption privacy

# Further refine:
"end-to-end encryption" (signal|matrix) -deprecated
```

### Use Field Searches for Precision

Instead of:

```textplain
github security issue
```

Try:

```textplain
domain:github.com title:(security|vulnerability) -closed
```

### Combine Phrases with Alternations

```textplain
"privacy policy" (updated|changed|revised) (2025|2026)
```

## Common Use Cases

### Pruning Unwanted Documents

Use the search web UI to build a query that selects unwanted documents. For example, find pages from a domain that have not been updated for more than 90 days:

```textplain
domain:example.com updated:>90d
```

Review the results and refine the query as needed, then open **Actions**, click **Delete all matching results**, and confirm. This deletes every document matching the query, like `hister delete`, so you can quickly clean up unwanted results as you search. See [Deleting Documents](data-lifecycle#deleting-documents) for details about what deletion removes.

### Finding Documentation

```textplain
title:(docs|documentation|guide) domain:*.io hister
```

### Research Topic

```textplain
"zero-knowledge" (encryption|proof|architecture) -marketing
```

### Tracking Updates

```textplain
domain:mozilla.org (firefox|thunderbird) "release notes"
```

### Security News

```textplain
(vulnerability|CVE|security) "disclosure" -duplicate
```

### Privacy Tools Comparison

```textplain
"privacy" (comparison|vs|versus) (browser|vpn|email)
```

## Troubleshooting Queries

### No Results Found

- Remove field restrictions and try a broader search
- Check spelling and try wildcards
- Remove negations to see what's being excluded
- Simplify alternations

### Too Many Results

- Add field-specific searches
- Use quoted phrases for exact matches
- Add negations to filter out unwanted content
- Specify domains to narrow scope

### Unexpected Results

- Ensure quotes are properly closed
- Check that parentheses are balanced
- Verify field names are spelled correctly (`title`, `text`, `url`, `domain`, `language`, `type`, `user_id`)
- Remember searches are case-insensitive
- For type filter, use "web" or "file" (also accepts "local" for files)
