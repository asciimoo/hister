---
date: '2026-05-15T00:00:00+00:00'
draft: false
title: 'Rules'
description: 'Control indexing and ranking with allow, skip, priority, versioning, and query alias rules.'
---

Rules let you control how Hister indexes and surfaces documents. They live in
`{data_directory}/rules.json` in single-user mode, or per-user in the database
when [user handling](user-handling) is enabled. The **Rules** tab in the web
interface (and the TUI) is the easiest way to manage them.

## Rule Types

### Allow rules

Allow rules limit indexing to URLs matching at least one allow pattern. Patterns
are combined with OR: a match against any entry is enough. Skip rules still take
precedence, so you can allow a whole site while excluding particular paths.

An empty allow list permits any URL that does not match a skip rule. Removing the
last allow rule restores that behavior. Existing rules files without an `allow`
field continue to work as before.

For example, allow these sites:

```text
^https://example\.com/
^https://docs\.example\.org/
```

Then add `^https://docs\.example\.org/private/` as a skip rule to exclude private
paths on the documentation site.

Allow rules apply to API submissions and browser imports as well as automatic
browser capture. Editing rules does not immediately remove saved documents.
Running `hister reindex` removes documents excluded by the current allow or skip
rules, except documents carrying an explicit manual override. In multiple user
mode, reindexing uses each document owner's rules; public documents use the
instance rules.

### Skip rules

Skip rules prevent matching URLs from being added to the index. When a URL
matches any pattern in the skip list, Hister silently discards the document
during indexing and during `hister reindex`.

To save an individual page excluded by allow or skip rules, click **Index this page now**
in the browser extension or use its indexing shortcut. This explicitly overrides
both rule groups for that submission and saves the choice with the document, so the
page survives `hister reindex`. Automatic submissions still respect the rules.

From the command line, use `--ignore-rules` with `hister index` or any `hister import`
subcommand to save the same explicit override with each submitted document. For an
already indexed URL, combine `hister index --ignore-rules` with `--force` so that the
document is submitted again. The override does not bypass sensitive content checks,
robots rules, or crawl and file selection filters.

API clients can set `metadata.ignore_skip_rules` to the boolean `true` in JSON
documents submitted to `/api/add` or `/add`. For `/api/add_pdf`, it belongs inside
`document.metadata`; for `/api/batch`, each add operation has its own metadata.
The override is stored with the document and preserved in exported documents.

The override applies only to URL allow and skip rules. The metadata field keeps its
existing name for compatibility. Authentication, ownership, sensitive
content checks, and the exclusion of Hister's own URLs still apply. Accepted
updates preserve an existing override unless they explicitly submit
`metadata.ignore_skip_rules: false`. Explicit deletion still removes the document.

**Use cases**: block ad networks, login pages, cookie-consent walls, or any
site you never want in your search results.

```
^https://ads\.example\.com
^https?://(login|mail)\.example\.com/
.*\?utm_source=
```

### Priority rules

Priority rules push matching documents to the top of search results regardless
of their relevance score. Hister applies a large score boost to any document
whose URL matches a priority pattern.

**Use cases**: always surface your personal wiki, your company's internal
documentation, or a trusted source before other results.

```
^https://wiki\.example\.com/
^https://docs\.example\.com/
```

### Versioning rules

Versioning rules tell Hister to **track changes** to a document every time it
is re-indexed. When a URL matches a versioning pattern and the document content
differs from the previously indexed version, Hister stores diff match patch
records for the HTML and text changes in the database.

**Use cases**: monitor pages for edits (privacy policies, documentation, news
articles), build a personal changelog of sites you follow, or audit when a
trusted resource last changed.

```
^https://example\.com/privacy-policy$
^https://docs\.example\.com/
```

#### Viewing stored versions

Once Hister has recorded at least one version diff for a document, the preview
panel shows a previous version count. Clicking it opens a changelog with each
recorded diff and timestamp. From there you can inspect a diff or reconstruct
and display an archived version.

### Aliases

Aliases are query-time shortcuts: before Hister executes a search, it replaces
any alias keyword with its expanded form.

```json
{
  "gh": "domain:github.com",
  "local": "type:file",
  "work": "domain:(internal.example.com|jira.example.com)"
}
```

With the `work` alias above, searching for `work deployment` is equivalent to
`domain:(internal.example.com|jira.example.com) deployment`.

## Pattern syntax

Allow, skip, priority, and versioning rules are matched against the **full URL**
(including scheme, host, path, and query string). A few important details:

- Patterns follow [Go regular expression syntax](https://pkg.go.dev/regexp/syntax).
  Look-ahead and look-behind are **not** supported.
- Anchoring must include the scheme: `^https://foo.com` or
  `^https?://(login|mail)\.` are valid; `^foo.com` is not.
- The URL hash is stripped before matching
  (`https://foo.com/#section` becomes `https://foo.com/`).
- Query-string parameters are **not** reordered; only `utm_*` parameters are
  stripped.
- A trailing `$` anchor will **not** match URLs that have a query string:
  `/login$` does not match `https://foo.com/login?auth=1`.

When adding or editing a skip rule on the Rules page, or adding one from a result action menu,
select **Delete matching documents already in the index** to apply the saved rule to existing
documents. The option is available only when the resulting rule type is skip and is disabled by
default. When documents match, the page shows their count and asks for confirmation before deleting
them. Cancelling keeps the saved rule and leaves the existing documents in the index.

## Storage

In **single-user mode** (user handling disabled), rules are saved to and read
from `rules.json` in the configured data directory. Changes made through the
web interface or API update this file directly.

In **multi-user mode** (user handling enabled), each user has a private copy of
their rules stored in the database. Changes affect only the authenticated user
and do not touch `rules.json`. See [User Handling](user-handling) for details.
