---
date: '2026-03-19T00:00:00+00:00'
draft: false
title: 'Browser Extension'
description: 'Install and configure browser capture, authentication, search integration, and indexing controls.'
---

<script>
  import ConfigReference from '#lib/ConfigReference.svelte';

  const connectionOptions = [
    {
      name: 'Server URL',
      type: 'string',
      defaultValue: 'http://127.0.0.1:4433/',
      description: 'Full URL of the Hister server, including its scheme and port.',
    },
    {
      name: 'Custom Headers',
      type: 'map[string]string',
      defaultValue: '(none)',
      description: 'Additional HTTP headers included with every request. Use them for reverse proxy authentication or other server requirements.',
    },
    {
      name: 'Access Token',
      type: 'string',
      defaultValue: '(none)',
      description: 'Optional global or personal access token used to authenticate extension requests.',
    },
    {
      name: 'Submit as public documents',
      type: 'bool',
      defaultValue: 'Off',
      description: 'Stores newly indexed pages as public documents with user ID zero. This setting appears after authentication.',
    },
  ];
</script>

The Hister browser extension is the primary way to automatically index your browsing history. It runs silently in the background, sending page content to your Hister server as you browse.

## Installation

- **Chrome / Chromium / Edge**: [Install from Chrome Web Store](https://chromewebstore.google.com/detail/hister/cciilamhchpmbdnniabclekddabkifhb)
- **Firefox**: [Install from Firefox Add-ons](https://addons.mozilla.org/en-US/firefox/addon/hister/) (also works on Firefox for Android)
- **qutebrowser**: Use the built in `hister companion qutebrowser` command as described below.
- **Safari**: Use the built in `hister companion safari` command as described below.

> The extension only communicates with your Hister server, it never contacts any third-party services or the websites you visit (except for downloading the page's favicon while collecting page data).

### qutebrowser

The qutebrowser companion submits the rendered page title, text, HTML, and
favicon to Hister, then monitors changes made by single page applications.
Pages served from the configured Hister base URL are ignored.

The built in `hister companion qutebrowser` command receives tab, navigation,
and DOM update events through the local Qt WebEngine DevTools endpoint.
Extraction runs in an isolated JavaScript world, then the companion submits the
rendered content directly to Hister. The access token is never injected into
the inspected page.

Close all qutebrowser processes, then start qutebrowser with a loopback only
debugging endpoint:

```bash
QTWEBENGINE_REMOTE_DEBUGGING=127.0.0.1:9222 qutebrowser
```

Run the companion in a separate terminal:

```bash
hister companion qutebrowser
```

The command uses `server.base_url` and `app.access_token` from the normal
Hister configuration. Global `--server-url`, `--token`, and `--client-timeout`
options can override the destination settings:

```bash
hister --server-url http://127.0.0.1:4433 \
  --token 'replace-with-app-access-token' \
  companion qutebrowser
```

The DevTools endpoint defaults to `http://127.0.0.1:9222`. Use
`--devtools-url`, `--label`, `--initial-delay`, `--debounce`, or `--max-wait`
after the `qutebrowser` subcommand to change its behavior. Run the command with
`--help` for every option. Newly loaded pages are submitted after one second.
Later content changes use a ten second quiet period, with a maximum wait of
thirty seconds for continuously changing pages.

Remote debugging provides complete access to open browser pages. The companion
accepts only a localhost or loopback IP DevTools endpoint. Never expose the Qt
WebEngine debugging port to a network.

### Safari

Safari has no Hister extension. The `hister companion safari` command keeps the
index current instead, by following the history database Safari maintains
anyway and indexing pages as they are visited:

```bash
hister companion safari
```

**It is not equivalent to the extension.** Safari's history records addresses,
not page content, so the companion asks Hister's crawler to fetch each page. A
page behind a login is indexed as an anonymous visitor sees it, and a page built
by JavaScript may index as an empty shell. Where the extension captures what you
saw, this records where you went and fetches what is there now.

Reading the history requires Full Disk Access for the terminal or application
running Hister, granted under System Settings > Privacy & Security > Full Disk
Access. The requirement is checked at startup.

The first run indexes visits from the moment it starts. Existing history belongs
to [`hister import browser`](import#importing-browser-history); use `--catch-up` to index it
here instead. Use `--poll-interval`, `--batch-size`, `--state-path`, or
`--label` to change its behaviour, and `--help` for every option.

Pages already in the index are not fetched again, so revisiting a site costs
nothing.

## Features

### Automatic Page Indexing

The extension captures visible pages, including the page title, full text, HTML, and favicon, then sends them to your Hister server via its API. Pages opened in a background tab are captured when you view them.

Turn off **Automatic indexing** in the extension popup to pause automatic capture. A thick red border around the toolbar icon and the tooltip "Hister: automatic indexing is off" indicate this setting. You can still index pages manually.

When a page provides a `<link rel="canonical">` URL, the extension uses it for indexing if it is an HTTP or HTTPS URL with exactly the same hostname as the visited page. Relative canonical URLs are resolved against the document base URL. Other domains and subdomains, invalid URLs, and URLs containing credentials are ignored. URL fragments are removed. Automatic indexing checks allow and skip rules against both the visited URL and the canonical URL.

While a page remains visible, the extension checks for changes at intervals starting at 30 seconds. The interval doubles when nothing changes, up to five minutes. Navigation within a page waits for one second of quiet before a check, and automatic checks and submissions remain at least 30 seconds apart within that page. Rapid navigation can therefore capture only the latest page state.

Changes to text, title, favicon, or page metadata trigger an update. Cosmetic markup changes alone wait for a preview check, at most once every five minutes, so animations and changing HTML attributes do not continuously trigger indexing. Each submission includes the current full HTML for extraction and previews.

When you hide a tab, the extension captures one final snapshot and stops polling. If that snapshot has changed, it is submitted after any remaining submission delay. Showing the tab again resumes checks using fresh content. Closing the tab or navigating away flushes changed content immediately to the extension's background process, bypassing the remaining delay. Unchanged content is not submitted again. Manual indexing also submits immediately and bypasses these delays.

To capture only selected websites, add allow patterns on the Hister Rules page. A URL
must match at least one allow pattern when the list is nonempty, and skip rules take
precedence. The extension checks both lists before uploading pages or PDFs. Patterns
that JavaScript cannot evaluate are checked by the server when the page is submitted.

Pages served with an unsuccessful HTTP status (`4xx` or `5xx`) are not indexed
automatically, so error pages such as "404 Not Found" stay out of the index. The
extension reads the status of every main frame response with the `webRequest`
API, which works in both Chromium based browsers and Firefox. Manual reindex
overrides this check.

### Manual Reindex

Manual indexing immediately submits the current
page or PDF, even when automatic indexing is disabled or allow or skip rules exclude it.
The indexing keyboard shortcut does the same. This is useful when you exclude a
domain from automatic indexing but want to save an individual page from it.

Manual indexing saves an allow and skip rule override with the document, so it survives an
index rebuild. It leaves your indexing rules and automatic indexing setting intact;
later automatic submissions still respect them. Sensitive content checks and
normal access restrictions still apply.

### Keyboard Shortcuts

The extension defines browser level shortcuts for common indexing actions.

| Action                              | Windows and Linux | macOS       | Notes                                                                 |
| ----------------------------------- | ----------------- | ----------- | --------------------------------------------------------------------- |
| Index current page                  | `Ctrl+I`          | `Command+I` | Runs even when automatic indexing is disabled.                        |
| Disable indexing for current page   | `Ctrl+B`          | `Command+B` | Adds a skip rule for the exact current URL.                           |
| Disable indexing for current domain | `Ctrl+Y`          | `Command+Y` | Adds a skip rule for the current origin, including scheme and domain. |

Browser shortcuts can be changed in the browser extension shortcut settings. In Chrome, Chromium, and Edge, open `chrome://extensions/shortcuts`. In Firefox, open `about:addons`, choose the extension shortcut settings, then edit the Hister shortcuts.

### Search Engine Result Tracking

The extension detects when you click on a search result in **Google** or **DuckDuckGo** and records the query alongside the result's title and URL to provide that result for the same query in the future.

### Indexed Page Indicator

The optional indexed page indicator checks the active page against your Hister server.
It is disabled by default.

## Options Page

### Connection Settings

<ConfigReference items={connectionOptions} />

The extension checks the server connection before saving settings.

### Authentication

The extension supports global or personal access tokens and browser session authentication.

Browser session authentication requires an existing login to the Hister web interface in
the same browser when [user handling](/docs/user-handling) is enabled.

The extension shares the active web session. Logging out revokes that session;
a personal access token authenticates independently.

**Submit as public documents** makes newly indexed pages visible as public documents instead
of storing them only under your user account.

## Troubleshooting

**The extension icon shows a `!` badge**

The last attempt to send page data to the server failed. Open the popup to see the error. Common causes:

- The Hister server is not running: start it with `hister listen`.
- The **Server URL** is wrong: confirm it matches the address printed when the server starts (default `http://127.0.0.1:4433/`).
- User handling is enabled but the extension is not authenticated: configure an access token or use an active browser session.

**Pages are not being indexed**

- Make sure **Automatic indexing** is enabled in the popup.
- Check that the server is reachable and the URL is correct (see above).
- If user handling is enabled, make sure you have authenticated the extension (see above).
- Some pages (browser-internal pages like `chrome://…`, `about:…`) cannot be accessed by extensions and are silently skipped.
