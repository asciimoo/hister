---
date: '2026-04-07T11:00:00+00:00'
draft: false
title: 'Extractors'
description: 'Understand and configure the built in content handlers used for indexing and previews.'
---

<script>
  import ConfigReference from '#lib/ConfigReference.svelte';

  const ytdlpOptions = [
    {
      name: 'binary',
      type: 'string',
      defaultValue: 'yt-dlp',
      description: 'Path to the yt-dlp executable. Use this when the binary is not on PATH or to select a specific version.',
    },
    {
      name: 'timeout',
      type: 'int',
      defaultValue: '15',
      description: 'Seconds to wait for yt-dlp to finish before aborting the request.',
    },
    {
      name: 'max_concurrent_jobs',
      type: 'int',
      defaultValue: '2',
      description: 'Maximum number of yt-dlp processes running at once. Additional jobs wait for a slot. Set it to zero for no limit.',
    },
    {
      name: 'fetch_subtitles',
      type: 'bool',
      defaultValue: 'false',
      description: 'Downloads subtitles or an automatically generated transcript and appends them to the indexed text and preview.',
    },
    {
      name: 'sub_language',
      type: 'string',
      defaultValue: 'auto',
      description: 'Subtitle language code to request. Auto selects automatically generated captions. Used only when fetch_subtitles is true.',
    },
    {
      name: 'cookies_file',
      type: 'string',
      defaultValue: '(none)',
      description: 'Path to a Netscape format cookies file passed to yt-dlp. Useful for restricted content.',
    },
    {
      name: 'cookies_from_browser',
      type: 'string',
      defaultValue: '(none)',
      description: 'Browser from which yt-dlp extracts cookies. This takes precedence over cookies_file.',
    },
    {
      name: 'extra_args',
      type: 'string[]',
      defaultValue: '(none)',
      description: 'Additional yt-dlp command line flags appended verbatim to every invocation.',
    },
    {
      name: 'extra_domains',
      type: 'string[]',
      defaultValue: '(none)',
      description: 'Additional domains to match this extractor. They should take the form "domain.tld", e.g. "vimeo.com". Check the yt-dlp repository for a full list of supported sites.'
    }
  ];
</script>

Extractors are the components responsible for turning raw HTML or file content
into rich, searchable data. Specialist extractors understand particular sites
and file formats. General extractors handle everything else.

## Purpose

Generic HTML-to-text conversion loses a lot of signal. A Stack Overflow answer,
a Go package reference, a local Markdown note, and a news article all have
different structure and a one-size-fits-all parser cannot take advantage of
that structure.

Extractors exist so that each kind of source can be handled in the most
**domain-specific** way possible. A specialist extractor for a particular
website or file format can:

- pull out the parts of the page that actually matter and discard noise (ads,
  navigation, boilerplate)
- produce richer plain text that makes search results more relevant
- surface structured details answers, code snippets, documentation sections
  that a generic parser would flatten or miss entirely
- give each content type a suitable preview layout and presentation

The goal is always to capture **more specialised, higher-quality information**
about the content being processed, so that search results and the document
preview are as useful as possible for the source in question.

When a page is fetched by the browser extension, the CLI, or the crawler,
Hister processes its raw HTML or file contents into searchable text and a
preview.

## Configuration

Each extractor can be enabled or disabled, and may expose custom options,
through the `extractors` section of the config file.

```yaml
extractors:
  <extractor-name>:
    enable: true | false
    options:
      key: value
```

Use the lower case extractor names listed below as configuration keys.

Only entries you want to change from the default need to be specified. If an
extractor is omitted from the config, its built-in defaults apply.

### `enable`

Controls whether the extractor participates in the chain.

| Value   | Effect                                                            |
| ------- | ----------------------------------------------------------------- |
| `true`  | Extractor is active. This is the default except for `ytdlp`.      |
| `false` | Extractor is skipped for automatic indexing and preview selection |

### `options`

A map of settings supported by that extractor. An unrecognised option causes a
configuration error.

## Built-in extractors

The extractors below are listed in processing order. Specialist handlers run
before general fallbacks. Metadata enrichers can add details without replacing
the searchable body.

### `markdown`

Provides sanitized HTML previews for locally indexed Markdown files. The file
indexer renders `.md` and `.markdown` source into HTML before the extractor
chain runs, so this extractor leaves indexed text unchanged and handles the
preview.

**Matches:** `file://` URLs ending in `.md` or `.markdown`.

### `orgmode`

Provides sanitized HTML previews for locally indexed Org mode files. The file
indexer renders `.org` source into HTML before the extractor chain runs, so this
extractor leaves indexed text unchanged and handles the preview.

**Matches:** `file://` URLs ending in `.org`.

### `embeddedvideo`

Scans `iframe`, `video`, `embed`, and `object` elements for embedded video URLs.
Discovered entries are stored in the document's `videos` metadata without
replacing its searchable body or preview.

**Matches:** pages whose raw HTML contains a supported embedding element.

### `jsonld`

Parses every `<script type="application/ld+json">` block in the page and writes
normalised [schema.org](https://schema.org) metadata to the document. It
captures the `@type` content classification and `headline` fields. General HTML
extraction still supplies the searchable body text.

**Matches:** any page that contains the `application/ld+json` substring.

### `reddit`

Extracts one Reddit post together with every comment already present in the
page. Indexed text and the preview preserve reply depth. Current Reddit pages
are read through semantic custom elements and content slots. Legacy pages are
read through Reddit thing identifiers, with schema.org discussion data as a
fallback.

The extractor does not request or expand hidden comments. Subreddit listings,
profiles, search results, and every other Reddit page are excluded.

**Matches:** Reddit post paths containing `/comments/{post_id}` and short post
links on `redd.it`.

### `discourse`

Extracts one Discourse topic together with every regular post already present
in the page. It combines rendered `article[data-post-id]` content with the
embedded topic stream, which preserves post numbers, authors, timestamps,
reply targets, likes, reactions, and accepted solutions. Schema.org `QAPage`
and crawler markup provide stable fallbacks.

The extractor performs no additional requests. Hidden posts and system action
entries are excluded. Category lists, tag lists, profiles, search pages, and
topic API formats are rejected.

**Matches:** topic paths such as `/t/{slug}/{topic_id}` on pages carrying a
Discourse generator, setup, or environment marker.

### `stackexchange`

Extracts Stack Exchange network question pages, including Stack Overflow,
Server Fault, Super User, Ask Ubuntu, MathOverflow, Stack Apps, and
`*.stackexchange.com` communities. Indexed text includes the question body and
all answers, with accepted answers marked.

The preview pane shows the full question body followed by each answer separated
by a horizontal rule, with accepted answers marked.

**Matches:**

- [stackoverflow.com](https://stackoverflow.com)
- [serverfault.com](https://serverfault.com)
- [superuser.com](https://superuser.com)
- [askubuntu.com](https://askubuntu.com)
- [mathoverflow.net](https://mathoverflow.net)
- [stackapps.com](https://stackapps.com)
- [stackexchange.com](https://stackexchange.com)

### `godoc`

Provides a rich preview for Go package documentation. The preview pane renders
the `Documentation-content` section of the page with relative links rewritten to
absolute `pkg.go.dev` URLs. General HTML extraction supplies the searchable
text.

**Matches:** `https://pkg.go.dev/…`

### `github`

Extracts searchable content and previews from GitHub repository roots, issue
pages, issue lists, and pull request pages. Repository results include the
description, star count, topics, programming languages, and README. Issue and
pull request results include their page specific metadata and discussion
content.

Issue previews show the status, labels, description, and comments with their
authors and dates when available. Formatting, code blocks, links, and task
completion are preserved. The preview uses the saved page, so comments that
were not loaded when the page was indexed are not included. Open the original
issue to see the latest discussion.

**Matches:** `https://github.com/{owner}/{repo}`, its `/issues` list, individual
`/issues/{number}` pages, and individual `/pull/{number}` pages. GitHub system
paths such as `/settings`, `/topics`, and `/explore` are excluded.

### `lobsters`

Extracts the full content of a lobste.rs submission, including the story
metadata (title, author, tags, submission date), the optional story body, and
the complete nested comment tree. Both indexed text and preview preserve the
parent–child comment hierarchy.

**Matches:** `https://lobste.rs/s/…`

### `hackernews`

Extracts the full content of a Hacker News item page, including the submission
metadata (title, target site, score, author, submission date), the optional self
text of an Ask HN or text post, and the complete comment tree. Hacker News
renders comments as one flat table and records depth in the `indent` attribute
of each row, so both the indexed text and the preview rebuild the parent–child
hierarchy from it.

**Matches:** `https://news.ycombinator.com/item?id=…`

### `wikipedia`

Extracts article content from Wikipedia. Indexed text includes the article
title, infobox key–value pairs, and the body text with navigation boxes,
references, and other noise removed. The preview renders the article HTML with
inline styles applied, videos replaced by their poster frames, and relative URLs
rewritten to absolute Wikipedia URLs.

**Matches:** `https://*.wikipedia.org/wiki/…` (article pages only; non-content
namespaces such as `Special:`, `Talk:`, `User:`, `File:`, and `Category:` are
excluded).

### `mastodon`

Handles Mastodon instance pages by splitting them into individual toot documents.
Each toot found on the page is indexed as a separate document with its own URL
and author, allowing individual posts to appear in search results. The original
aggregator page is not indexed. Links for remote toots are rewritten to point
directly to the account's original server instead of the instance displaying
the federated copy.

Detection is heuristic: the extractor checks for a `"repository":"mastodon/mastodon"`
marker in the page HTML, or for a `type: toot` metadata flag set by a previous
pass.

**Matches:** any Mastodon instance page containing the Mastodon source marker.

### `bluesky`

Handles Bluesky profiles, custom feeds, and post threads by splitting every
available post into a separate document. Each document receives its canonical
`bsky.app` URL, author, handle, publication time, and a
`type: bluesky` metadata marker. The source page itself is not indexed.

The extractor prefers schema.org `DiscussionForumPosting` and `Comment` data
published by Bluesky. This supports profile and individual post pages even
without browser rendering. It also reads rendered feed and thread cards through
semantic test identifiers, roles, post links, and content attributes. Generated
CSS class names are not used. Rich rendered content is preferred when both
representations describe the same post, while canonical URL deduplication keeps
one document per post.

Custom feeds require the `chromedp` or `bidi` crawler backend because their post
lists are rendered in the browser. Profile and individual post pages also work
with the `http` backend through their JSON LD data.

**Matches:** profile, feed, and post pages on `bsky.app`, `www.bsky.app`, and
`embed.bsky.app`.

### `twitter`

Handles Twitter and X feeds, profiles, and tweet pages by splitting every
visible tweet into a separate document. Each tweet receives its canonical X
URL, author, handle, publication time, and a `type: tweet` metadata marker. The
feed or conversation page itself is not indexed.

The extractor first reads schema.org `SocialMediaPosting` metadata. It also
supports rendered tweet elements and Open Graph metadata from individual tweet
pages. This allows it to handle both server rendered pages and pages captured
after the application has loaded in a browser.

When a rendered tweet body is available, it is preferred over semantic metadata
so original link destinations produced by the application are preserved.
Expanded link attributes on anchors inside the tweet body replace `t.co`
aliases in indexed text. When an expanded attribute is absent, a complete URL
rendered as the anchor text is used instead. Truncated visible URLs are not
guessed. Preview anchors point directly to the original destination. A `t.co`
URL with no available destination remains plain text rather than becoming a
clickable redirect link.

**Matches:** feeds, profiles, and tweet pages on `twitter.com` and `x.com`,
including their common web and mobile hostnames.

### `notion`

Extracts the title and rendered block content of Notion pages and produces a
sanitized preview. Notion renders content in the browser, so indexing requires
the `chromedp` or `bidi` crawler backend. Hister does not index the empty
application shell when the rendered block content is missing.

**Matches:** nonroot pages on `notion.so`, `www.notion.so`, and any
`*.notion.site` domain.

### `ytdlp`

Extracts video metadata from video-hosting sites (YouTube, Vimeo, Twitch, etc.)
using the [`yt-dlp`](https://github.com/yt-dlp/yt-dlp) command-line tool.
Provides a dedicated video preview template that shows the thumbnail, duration,
uploader, description, chapter list, and optional transcript.

The extractor is **disabled by default** because it requires `yt-dlp` to be
installed separately.

**Matches:** a curated list of video-hosting domains (YouTube, Vimeo, Twitch,
Dailymotion, Bilibili, and others), as well as any hostname containing common
video-platform substrings. Additional domains can be matched using the `extra_domains` option.

#### Options

<ConfigReference items={ytdlpOptions} />

#### Example configuration

```yaml
extractors:
  ytdlp:
    enable: true
    options:
      binary: /usr/local/bin/yt-dlp
      timeout: 30
      max_concurrent_jobs: 2
      fetch_subtitles: true
      sub_language: en
      cookies_from_browser: firefox
      extra_args:
        - --proxy
        - socks5://127.0.0.1:1080
      extra_domains:
        - vimeo.com
```

### `chatgpt`

Extracts a ChatGPT conversation as one document. Each
visible user and assistant turn is retained in order and labelled in both the
indexed text and the preview. Headings, lists, tables, code blocks, and ordinary
safe links are preserved in the sanitized preview. System, tool, internal, and
unsupported media content is ignored.

**Matches:** `https://chatgpt.com/c/<conversation-id>` authenticated,
`https://chatgpt.com/share/<conversation-id>` public shared, and
`https://chatgpt.com/g/<gpt-id>/c/<conversation-id>` custom GPT conversation URLs
(including `www.chatgpt.com`).

### `duckduckgo`

Indexes the organic results of a DuckDuckGo search page as one document per
result. Each result is searchable by its title, snippet, and site right after
the search, without visiting it. Promotional results and duplicate results are
ignored, and outbound redirect links (`/l/?uddg=...`) are unwrapped. The search
page itself is never indexed: a page that yields no parseable result, such as a
JavaScript shell, a consent screen, or an empty search, is dropped instead of
being stored as boilerplate. A result that points to an already indexed page
keeps the richer existing document.

The generated Search Assist answer is dropped with the page by default, because
it only exists after the page has been asked to generate one. The opt-in
`keep_answer` option stores it instead, as the search page's own document.

**Matches:** search pages of `duckduckgo.com`, `www.duckduckgo.com`,
`html.duckduckgo.com`, `lite.duckduckgo.com`, `noai.duckduckgo.com`, and
`start.duckduckgo.com` that carry a `q` parameter.

The selectors cover the HTML and Lite endpoints, which serve results without
JavaScript, as well as the JavaScript front end, which links results directly
and marks them with `data-testid` attributes. That front end renders its results
in the browser and returns a shell without results to a plain HTTP client, so
`duckduckgo.com/?q=` pages contribute results only when the rendered page is
submitted from the browser extension; a plain fetch of the shell stores nothing.
The live test covers the HTML endpoint; the JavaScript front end is covered by a
unit test built from a browser rendered page.

The robots.txt of `duckduckgo.com` disallows its `/html` and `/lite` paths while
allowing `/?q=`, whereas `html.duckduckgo.com` and `lite.duckduckgo.com` allow
crawling, which is why the live test uses the dedicated host.

**Options:**

- `max_results` (int, default `20`): maximum number of results recorded per
  search page.
- `keep_answer` (bool, default `false`): store the generated Search Assist
  answer as the search page's own document. Unlike the interface around it, the
  answer does not exist anywhere else, so it can be worth keeping. Only pages
  that carry an answer are stored this way; a page without one is still dropped.
  The answer text is stored without the source chips, which the page keeps next
  to it, and the page is marked with `type: duckduckgo_answer` and the search
  `query` metadata.

### `readability`

Generic article extractor using the
[go-readability](https://codeberg.org/readeck/go-readability) library. Strips
navigation, ads, sidebars, and other boilerplate and returns the main article
content as clean plain text and HTML. Also extracts author, publication date,
description, site name, and canonical image from JSON-LD, OpenGraph, and meta
tags.

**Matches:** every page. Acts as the primary fallback for all content that no
specialist extractor handles.

### `basic`

Ultimate fallback. Walks the raw HTML token stream and collects all visible text
inside `<body>`, discarding `<script>`, `<style>`, and `<noscript>` elements.
Produces plain text with no further processing.

**Matches:** every page. Only reached when `Readability` fails or is disabled.
