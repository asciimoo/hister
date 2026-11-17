---
date: '2026-02-25T00:00:00+00:00'
draft: false
title: 'Query Language Guide'
description: 'Build precise searches with fields, phrases, Boolean groups, dates, wildcards, negation, and sorting.'
---

Nogon provides a query language that allows you to search through your content with precision. This guide explains the different query types and search techniques available.

## Basic Search

Simply type any word to search across all fields:

```textplain
nogon
```

This searches for "nogon" in the title, text content, URL, and domain of all indexed pages.

## Quoted Phrases

Use double quotes to search for exact phrases:

```textplain
"privacy policy"
```

This finds pages containing the exact phrase "privacy policy" (not just pages with both words separately).

Quotes also keep query operators literal. For example, `"domain:example.com"` searches for that
phrase in document content, and `"-archived"` does not exclude documents. To exclude a phrase,
put the minus sign before the opening quote: `-"privacy policy"`.

**Examples:**

```textplain
"self-hosted applications"
"data privacy regulations"
"end-to-end encryption"
```

## Query Errors

An incomplete query shows an error explaining what needs fixing and where. Positions start at
character 1. Non-English letters count as characters, not multiple bytes. If an alias expands
your query, the position refers to the expanded query.

For example, `privacy OR` reports `expected a term after OR` at character 9. Complete it as
`privacy OR security`, or use `privacy "OR"` to search for the word OR.

Close every quote and group, supply a value after a field name, and put a term on each side of
`AND`, `OR`, or `|`. `NOT` must be followed by a term or group. Empty quotes and groups are
rejected. Quote literal operators or escape punctuation, for example `"OR"` or `\(example\)`.
Invalid URL regular expressions also report an error.

Search, update, and delete operations reject these invalid queries. In the web and terminal
clients, correct the query to resume searching. Text containing an unknown field name, such
as `unknown:value`, remains ordinary search text.

## Field-Specific Searches

You can search within specific fields using the `field:value` syntax:

### Available Fields

- **title:** - Search in page titles only
- **text:** - Search in page content only
- **url:** - Search in URLs only (bare file paths without `://` are automatically resolved to absolute `file://` URLs)
- **url_re:** Search URLs with a Go regular expression
- **domain:** - Search in domain names only
- **site:** Match a domain and its subdomains, ignoring letter case
- **has:** Find documents with a nonempty field or metadata value, such as `has:label` or `has:metadata.author`
- **label:** - Search in document labels only
- **language:** Filter by stored language tag, ignoring case. `en` includes regional tags such as `en-US`; `en-US` selects that region. Use `unknown` for documents without an identified language.
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
site:example.com
```

Finds documents from example.com and any of its subdomains, such as docs.example.com.
It does not match notexample.com or example.com.evil.test. Use a hostname without a scheme,
port, path, or wildcard. Letter case and a final dot are ignored. `domain:example.com`
continues to match only that exact domain.

```textplain
has:label
-has:metadata.author
```

The first query finds documents with an assigned label. The second finds documents without
an author metadata value. `has:` accepts `title`, `text`, `url`, `domain`, `label`, `language`,
`type`, `visits`, `added`, `updated`, `user_id`, and any `metadata.KEY` path.

Missing values, null, empty strings, whitespace only strings, and empty collections count as
absent. Numeric zero and boolean false count as present. Arrays and nested metadata objects
count as present when they contain at least one nonempty stored value. For example,
`has:metadata.author.name` checks a nested author name. Field names and metadata keys are
case sensitive. Unsupported field names match no documents.

Both filters support negation and alternatives:

```textplain
-site:example.com
site:(example.com|example.org) has:label
has:(label|metadata.author)
```

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

Finds documents labeled as English, including regional tags such as `en-US` and `en-GB`, whether detected or manually assigned. Use `language:en-US` to narrow the filter to that tag and any more specific variants. Filters normalize aliases: `language:no` and `language:nb` both match Norwegian Bokmal, including historical `no` labels. Patterns such as `language:en-*`, negation, and grouped alternatives also work.

See [Language Settings and Detection](/docs/language-detection) for supported codes, accuracy options, stop words, and how language analysis affects matching.

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

Finds remote file snapshots created with `nogon import file` or submitted as prepared documents through `/api/add`.

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
Put sort directives outside Boolean groups. Inside a group, `sort:date` is searchable text.

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

For faster searches on large collections, use a specific starting prefix when possible, such as
`url_re:^https://example\.com/docs/`. Patterns that match many URLs can still take longer.

When combining a regular expression with other alternatives, quote the expression so that its
pipes and parentheses stay part of the pattern:

```textplain
(url_re:"/docs/|/manual/" OR site:example.com) security
```

Pipes in an unquoted `url_re:` value belong to the regular expression. Use a quoted value before
a Boolean pipe, or separate alternatives with a space and `OR`.

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

Use `NOT` as an alternative to a minus sign. Either form can exclude a whole group:

```textplain
privacy NOT (facebook OR twitter)
privacy -(facebook|twitter)
```

Both examples exclude pages mentioning either Facebook or Twitter.

**Field-specific negation:**

```textplain
security -domain:facebook.com
title:nogon -url:*/issues/*
privacy -"social media"
-language:en
-type:file
```

## Alternation Expressions

Use pipe (`|`) or uppercase `OR` to match alternatives. Parentheses keep an expression together:

```textplain
(security|privacy|encryption)
```

Finds pages containing any of these terms.

```textplain
title:(firewall|vpn|proxy)
```

Finds pages with firewall, VPN, or proxy in the title.

Groups can contain phrases, negation, and other groups. Spaces around pipes and parentheses
are optional:

```textplain
title:("privacy policy" OR security)
title:(privacy AND (policy OR protection))
(domain:github.com OR domain:gitlab.com) NOT title:(archived OR deprecated)
```

A field before a group applies to every term in that group, including nested groups. An explicit
field inside the group overrides it. For example, `title:(privacy OR text:encryption)` matches
privacy in the title or encryption in the body.

### Boolean Operators and Precedence

Spaces between terms mean `AND`. You can also write uppercase `AND` explicitly:

```textplain
privacy security
privacy AND security
```

Negation is evaluated first, then `AND`, then `OR`. For example, `privacy OR security guide`
means `privacy OR (security AND guide)`. Use `(privacy OR security) guide` to require guide
for both alternatives. These rules also apply to `|` and `-`.

Only uppercase `AND`, `OR`, and `NOT` are operators. To search for an operator as text, quote it
or escape it with a backslash, such as `"OR"` or `\OR`. Quotes or backslashes also protect
parentheses and pipes in values: `label:"a|b"` and `label:a\|b` match the label `a|b`.

The web UI applies a newly selected facet or date filter to the whole query. Filters inside an
OR branch or a group stay in the query text rather than appearing as removable filter chips.

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
title:(setup|installation|configuration) domain:(*.io|*.dev) nogon
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

Bulk deletion in the web interface removes every document matching the query, including
matches beyond the displayed results. See [Deleting Documents](data-lifecycle#deleting-documents)
for deletion scope and command line examples.

### Finding Documentation

```textplain
title:(docs|documentation|guide) domain:*.io nogon
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
