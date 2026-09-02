---
date: '2026-02-21T16:18:00+01:00'
draft: false
title: 'Quickstart'
description: 'Start a local Hister server, connect a browser extension, and search your first indexed pages.'
---

<script>
import screenshot from "$lib/assets/landing_screenshot.png";
</script>

The simplest way to use Hister requires no configuration. First [install the Hister binary](installing), then follow the steps below.
When you are ready to customize it, start with [Creating a Config File](configuration#creating-a-config-file).

## Running the Server

Open a terminal, and start the server:

```bash
./hister listen
```

On Windows, run `.\hister.exe listen` in PowerShell instead.

The server will start on http://127.0.0.1:4433 and thus be accessible only from the same machine.
This is perfect for personal use on a single computer.

Just make sure not to close the terminal that the above command was run on, as doing so will shut down the server.
(The server can also be closed normally by pressing <kbd>Ctrl</kbd>+<kbd>C</kbd>.)
It is fine to close and/or reopen the server at any time, but it **must** be running for the clients to be able to index any pages; you will get errors otherwise.

On macOS and systemd Linux, install a user-level background service instead of leaving the terminal open:

```bash
./hister service install
```

This uses the binary in the current directory. If you have moved Hister to a directory on `PATH`, use `hister service install` instead. See [Running in the background](installing#running-in-the-background). Windows still uses `hister.exe listen` in a terminal.

More advanced setups are described in the "Advanced Server Setup" documentation.
To change storage, logging, interface, database, or authentication settings, review the [`app`](configuration#app-section) and [`server`](configuration#server-section) configuration sections.

## Installing a Browser Extension

Install one or more of the following extensions to index any pages you _newly_ visit:

- **Chrome**: [Install from Chrome Web Store](https://chromewebstore.google.com/detail/hister/cciilamhchpmbdnniabclekddabkifhb)
- **Firefox**: [Install from Firefox Add-ons](https://addons.mozilla.org/en-US/firefox/addon/hister/) (also available on Firefox Mobile!)

Click the extension's icon to access its settings; the server URL should already be the default of `http://127.0.0.1:4433`.

To search Hister from your browser's address bar, add your Hister instance as a custom search engine in the browser settings.
Feel free to make it the default. Hister's search interface has a button to forward your query to an external search engine like DuckDuckGo or Google.

Note that the browser extensions _do not_ access the network except for downloading the page's favicon while collecting page data.
In particular, they will never make requests to any site you are visiting (they only look at what the browser itself has loaded), and thus they are completely transparent to the indexed websites.

## Accessing the Web Interface

Type <http://localhost:4433> (or, equivalently, `http://127.0.0.1:4433`) into your browser's address bar, and you will access Hister's interface.
There, you can perform searches, add indexing rules... feel free to look around!

<img src={screenshot} alt="Screenshot of Hister's landing page" />

As you visit more pages in your browser (with the extension enabled), the number of indexed pages should increase!
(Just refresh the page to update it.)
If the number fails to increase, check that your browser extension is installed, enabled, and configured to the right URL.

If you can't access the Web interface at all, check that [the server is running](#running-the-server).

## Next Steps

- [Import files, browser history, or Linkwarden bookmarks](import)
- [Index local directories](configuration#local-directory-indexing)
- [Enable semantic search](configuration#semantic-search)
- In the Web interface, set up ignoring and priority rules, as well as keyword aliases for efficient searching
- Explore the [Hister query language](query-language)
- Configure [web hotkeys](configuration#hotkeysweb-section) and review the [sensitive content patterns](configuration#sensitive_content_patterns-section)
