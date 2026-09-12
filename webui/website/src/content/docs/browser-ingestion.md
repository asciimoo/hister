---
date: '2026-08-01T00:00:00+02:00'
draft: false
title: 'Browser Ingestion'
description: 'Choose between live browser capture and history imports, and understand how repeated visits are stored.'
---

Hister can collect browsing data in two different ways. The browser extension captures pages as you visit them. The browser history import reads old URLs from a browser database and fetches their current contents. Many people use both.

## Choose the Right Method

| Goal                                              | Use                                                         | Why                                                                         |
| ------------------------------------------------- | ----------------------------------------------------------- | --------------------------------------------------------------------------- |
| Index pages from now on                           | [Browser extension](browser-extension)                      | It captures rendered page content automatically while you browse            |
| Capture pages behind a login                      | [Browser extension](browser-extension)                      | It sees the page already rendered in your signed in browser tab             |
| Bring in existing browser history                 | [`hister import browser`](import#importing-browser-history) | It reads qualifying URLs from the browser history database                  |
| Index an entire public site                       | [Website crawler](crawler)                                  | It follows permitted links instead of relying on your visit history         |
| Keep a browser database continuously synchronized | No direct option, except Safari                             | Browser history import is an explicit import, not a continuous sync service. `hister companion safari` follows Safari's history database, but indexes fetched pages rather than captured ones |

## Browser Extension Capture

The extension reads the current tab after the page loads. It submits the URL, title, extracted text, rendered HTML, and favicon to your configured Hister server. It also watches for content changes made by applications that update a page without a full navigation.

This means the extension can index content that a normal crawler cannot see, including pages rendered with JavaScript and pages available through your signed in browser session. It also means private account pages can be submitted. Configure skip rules for sites or paths that should never be stored, and review the [sensitive content patterns](configuration#sensitive_content_patterns-section).

The extension only handles pages visited after it is installed and enabled. It does not scan old browser history, bookmarks, downloads, cookies, or browser caches. It does capture the rendered page, which can contain private or sensitive information. Browser internal pages such as `about:` and `chrome:` pages cannot be captured.

For installation, authentication, controls, and shortcuts, see [Browser Extension](browser-extension).

## Browser History Import

Browser history databases contain URLs, titles, visit counts, and timestamps. They normally do not contain complete historical page contents. When you run:

```bash
hister import browser
```

Hister detects a supported browser database, reads qualifying URLs, and asks its crawler to fetch the pages that exist now. The resulting indexed content can differ from what you originally saw. A page may have changed, disappeared, moved, or started requiring authentication.

The `--min-visit` option can use the browser's visit count to choose URLs. The
`--start-date` option selects URLs whose most recent recorded browser visit is
on or after the given date. These filters do not recreate the browser's
original visit timeline or preserve every historical visit as a Hister record.
The indexed document timestamps describe the imported page fetch.

The import does not inherit the browser session. Sites that need login cookies may return a login page or reject the request. You can select a browser based crawler backend and provide cookies explicitly when appropriate. Treat exported cookies as secrets.

Browser imports use persistent crawl jobs so an interrupted import can resume. The job records remain in the Hister database until you remove the job with `hister crawl delete JOB_ID`.

See [Importing Browser History](import#importing-browser-history) for supported browsers, filtering, backends, cookies, and resume commands.

## How Repeated Visits Behave

Hister stores one current document for each normalized URL and owner. If the extension submits that URL again, the current document is updated and its Hister submission count increases. Hister does not keep a complete snapshot for every visit.

If the URL matches a versioning rule and its content changes, Hister stores a difference record that can reconstruct earlier content. Version tracking is optional and has no automatic expiry. See [Data Storage and Lifecycle](data-lifecycle#updates-and-versioning).

## What Deleting at the Source Does

Browser ingestion is not bidirectional sync.

- Clearing browser history does not delete Hister documents.
- Removing a bookmark does not delete an imported Hister document.
- Disabling or uninstalling the extension stops future capture but does not delete data already on the server.
- Deleting a Hister document does not change browser history, bookmarks, or source websites.

Use Hister deletion controls separately. See [Deleting Documents](data-lifecycle#deleting-documents).
