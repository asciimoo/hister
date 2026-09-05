---
date: '2026-03-19T00:00:00+00:00'
draft: false
title: 'Browser Extension'
description: 'Install and configure browser capture, authentication, search integration, and indexing controls.'
---

<script>
  import ConfigReference from '$lib/ConfigReference.svelte';

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

After installing, click the extension icon in your browser toolbar to open the popup and verify the server URL is correct.

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

The extension automatically captures page content every time you visit a URL. It extracts the page title, full text, HTML, and favicon, then sends them to your Hister server via its API.

After a page is successfully indexed, the extension continues monitoring it in the background and re-submits if the content changes (for example on single-page applications). The re-check interval starts at 10 seconds and doubles each time the page content is unchanged, reducing resource usage over time.

Automatic indexing can be paused at any time using the toggle in the popup.

### Manual Reindex

The **Reindex Page** button in the popup forces an immediate re-submission of the current page, regardless of whether it has changed. This is useful after clearing your server's index or when a page failed to index automatically (indicated by a `!` badge on the extension icon).

### Keyboard Shortcuts

The extension defines browser level shortcuts for common indexing actions.

| Action                              | Windows and Linux | macOS       | Notes                                                                 |
| ----------------------------------- | ----------------- | ----------- | --------------------------------------------------------------------- |
| Index current page                  | `Ctrl+I`          | `Command+I` | Runs even when automatic indexing is disabled.                        |
| Disable indexing for current page   | `Ctrl+B`          | `Command+B` | Adds a skip rule for the exact current URL.                           |
| Disable indexing for current domain | `Ctrl+Y`          | `Command+Y` | Adds a skip rule for the current origin, including scheme and domain. |

Successful shortcut actions show a `✓` badge on the extension icon for a few seconds. Failed actions show a `!` badge.

Browser shortcuts can be changed in the browser extension shortcut settings. In Chrome, Chromium, and Edge, open `chrome://extensions/shortcuts`. In Firefox, open `about:addons`, choose the extension shortcut settings, then edit the Hister shortcuts.

### Search Engine Result Tracking

The extension detects when you click on a search result in **Google** or **DuckDuckGo** and records the query alongside the result's title and URL to provide that result for the same query in the future.

## Popup

Clicking the extension icon opens the popup, which provides quick access to the most common controls.

| Control                                      | Description                                                                                                           |
| -------------------------------------------- | --------------------------------------------------------------------------------------------------------------------- |
| **Automatic indexing** toggle                | Enable or disable automatic page indexing. The setting is saved immediately.                                          |
| **Reindex Page** button                      | Force a re-submission of the current page to the server.                                                              |
| **Authenticate with Browser Session** button | Copy session cookies from the logged in Hister web UI to authenticate the extension when no access token is used.     |
| **Settings icon** (⚙)                        | Expand an inline form to view and update the Server URL and Access Token without opening the full options page.       |
| **Show indicator for indexed pages** setting | Show a `✓` badge on pages that are already indexed. This is available after opening the settings view from the popup. |
| **Submit as public documents** setting       | Store newly indexed pages as public documents with user id `0`. This appears after the extension is authenticated.    |

A status banner appears at the bottom of the popup after any action, showing success or error feedback. If the server rejected the last submission, a `!` badge is shown on the extension icon; saving valid settings clears it.

The indexed page indicator is off by default. To enable it, click the extension icon, open the settings view with the settings icon, then turn on **Show indicator for indexed pages**. When enabled, the extension checks the active page against your Hister server and keeps a `✓` badge visible on pages already present in the index.

## Options Page

The full options page is accessible via your browser's extension manager (right-click the icon → _Options_, or navigate to the extensions settings page). It provides all configuration in one place.

To open it directly, right click on the extension icon and select "Options", or with:

- **Chrome**: `chrome://extensions` → find Hister → click **Details** → **Extension options**
- **Firefox**: `about:addons` → find Hister → click the **…** menu → **Preferences**

### Connection Settings

<ConfigReference items={connectionOptions} />

Click **Save Settings** to apply. The extension validates the connection by calling `GET /api/config` before saving; an invalid URL will show an error instead.

### Authentication

The extension supports access token and browser session authentication. To use a token, enter a global access token or a personal access token in the popup settings or options page, then save the settings.

When [user handling](/docs/user-handling) is enabled, you can instead share the session cookies from the Hister web interface:

1. Log in to the Hister web interface in the same browser.
2. Click the **Authenticate with Browser Session** button in the popup or options page.

The extension copies the active session cookie, so it submits pages under your user account. You don't need to repeat this step if you log out in the web interface.

After authentication, the popup settings and options page show **Submit as public documents**. Turn this on when pages indexed from the extension should be visible as public documents instead of being stored only under your user account. The extension sends this preference with document submissions, and the server stores those documents with user id `0`.

## Troubleshooting

**The extension icon shows a `!` badge**

The last attempt to send page data to the server failed. Open the popup to see the error. Common causes:

- The Hister server is not running: start it with `hister listen`.
- The **Server URL** is wrong: confirm it matches the address printed when the server starts (default `http://127.0.0.1:4433/`).
- User handling is enabled but the extension is not authenticated: configure an access token, or click **Authenticate with Browser Session** after logging in to the web interface.

**Pages are not being indexed**

- Make sure **Automatic indexing** is enabled in the popup.
- Check that the server is reachable and the URL is correct (see above).
- If user handling is enabled, make sure you have authenticated the extension (see above).
- Some pages (browser-internal pages like `chrome://…`, `about:…`) cannot be accessed by extensions and are silently skipped.
