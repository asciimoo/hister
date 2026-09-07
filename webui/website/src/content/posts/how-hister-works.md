---
date: '2026-02-10T22:57:22+01:00'
draft: false
title: 'How Hister Works'
description: 'High level explanation about how Hister works by breaking down its four main components'
---

Hister is made up of four main parts:

1. **The Browser Extension** — Captures web pages as you browse
2. **The Indexer** — Organizes and prepares content for searching
3. **The Web Application** — Your search interface where you find things
4. **The Command Line Tool** — For advanced tasks, manual indexing, search and setup

## The Browser Extension

The browser extension is available for both [Chrome](https://chromewebstore.google.com/detail/hister/cciilamhchpmbdnniabclekddabkifhb) and [Firefox](https://addons.mozilla.org/en-US/firefox/addon/hister/). Once you install it, it works in the background without requiring any action from you.

Note: Update the server address in the extension, if your Hister server is listening on a non-default address.

### What It Does

**First, it reads the page exactly as you see it.** This is important because many modern websites are built dynamically.

**Second, it packages everything up.** The extension collects:

- The full URL of the page
- The page title
- The complete text content of the page
- The favicon
- The timestamp of when you visited
- The whole HTML document (for development and future reindex purposes)

**Third, it sends this package to your local Hister server.** This all happens automatically — you don't see it or need to do anything.

**Fourth, it keeps content up-to-date.** After indexing a page, the extension periodically checks if the content has changed. When it detects that the content has been updated (perhaps the article was edited, new comments were added, or documentation was updated), it automatically sends the new version to Hister. This means when you search for something later, you're finding the most recent version of the content you saw, not just the initial snapshot from when you first loaded the page.

## The Indexer

Once the browser extension sends page data to Hister, the indexer takes over.

The indexer includes several Hister-only features that make searching more efficient:

- **Relevance ranking:** Not all matches are equal. A page where search phrase appears in the title and multiple times in the content is ranked higher than a page where it's only mentioned once in passing.
- **Keyword aliases:** You can define shortcuts. For example, if you configure `go` as an alias for both `go` and `golang`, searching for "go strings" will find pages that reference the the Go programming language as Golang.
- **Priority results:** Hister automatically learns which pages you open for specific searches and shows them first next time. You can also manually pin any result to always appear at the top for specific search queries by clicking the three dots next to the result URL.
- **Duplicate detection:** If you visit the same page multiple times, Hister automatically updates the existing entry rather than creating duplicates.

## The Web Application

The web application is what you interact with most — it's your search interface, similar to Google's homepage but for your personal browsing history.

### Opening Hister

To use Hister, open your browser and go to the address where Hister is running (`http://127.0.0.1:4433/` by default).

### Searching

**Basic Search:**

Just start typing in the search box. As soon as you type the first few characters, results start appearing in real-time.

Let's say you remember reading something about Python async networking but can't recall which site it was on or what the article was called. You type "python async network" and immediately see:

- Every page you've visited that mentions the search terms
- Snippets of text showing where those words appear on the page
- The page title and URL
- When you indexed the page (like "2 days ago" or "3 weeks ago")
- The site's icon to help you recognize it

**Advanced Search:**

The search box supports advanced queries if you want to be more specific:

- **Combine terms:** Type `go AND python` to find pages mentioning both
- **Exclude terms:** Type `go -python` to find Golang pages that don't mention Python
- **Search specific fields:** Type `domain:github.com` to only search in URLs, or `title:tutorial` to only search page titles
- **Use wildcards:** Type `auth*` to find authentication, authorize, authorization, etc.
- **Search phrases:** Type `"error handling"` with quotes to find that exact phrase

**Real-World Example:**

You remember reading a great explanation of async/await in JavaScript, but you don't remember if it was on MDN, JavaScript.info, or a blog. You search for "async await javascript" and Hister instantly shows you all pages you've visited that discuss this topic. You scroll through and recognize the blog post you were thinking of from the snippet of text shown in the results.

### Search Tips and Shortcuts

The interface includes several helpful features:

**Autocomplete:** As you type, Hister suggests completions based on similar past queries. Press `Tab` to accept the suggestion.

**Recent searches:** The system remembers what you've searched for recently. If you're doing research on a topic, you can quickly repeat searches.

**Fallback to web search:** If you search and don't find what you're looking for, you can press `Alt+o` to open the same query in your preferred web search engine (like DuckDuckGo or Google). This makes it easy to seamlessly switch from searching your personal history to searching the entire web.

### Priority Results

Every time you search for something and click on a result, Hister remembers that connection. The next time you search for the same or similar terms, pages you've previously opened for those searches appear at the top in a "Priority Results" section, before the regular search results.

**Define Priority Results:**

Sometimes you want to explicitly pin a specific page to always appear first for certain searches. This is perfect for documentation you reference constantly or pages that are particularly important to your work.

To manually set a priority result:

1. Find the page in your search results
2. Click the three dots (⋮) next to the result URL
3. Select "Set as priority result"
4. Enter the search query you want this page to appear for
5. Click save

**Managing Priority Results:**

If you want to remove a priority result, click the three dots next to it when it appears in search results, and you'll see an option to remove it from the priority list for that specific query.

## The Command Line Tool

While most of your interaction with Hister is through the browser extension and web interface, the command line tool provides additional capabilities for setup and management.

### Initial Setup

When you first install Hister, you use the command line tool to set things up:

**Starting the server:**

```bash
hister listen
```

This command starts the Hister server so it can receive data from the browser extension and respond to your searches. You typically run this once when your computer starts up (or set it up to start automatically).

**Creating a configuration file:**

```bash
hister config create ~/.config/hister/config.yml
```

This generates a configuration file with default settings that you can then customize with your preferences.

### Importing Existing History

One of the most useful features of the command line tool is the ability to import your existing browser history. When you first install Hister, your index is empty. But you've likely been browsing the web for years and have thousands of pages in your browser's history.

The import command reads your browser's history database and indexes all those pages:

**For Chrome:**

```bash
hister import browser chrome ~/.config/google-chrome/Default/History
```

**For Firefox:**

```bash
hister import browser firefox ~/.mozilla/firefox/your-profile/places.sqlite
```

Hister goes through your history and attempts to fetch and index each page. This process can take a while if you have thousands of pages, but it only needs to be done once. After this initial import, the browser extension handles everything automatically going forward.

Note: The import can only index pages that are still accessible online. If a page has been deleted or moved, Hister won't be able to fetch and index it.

### Manual Indexing

Sometimes you want to index a specific page without visiting it in your browser. Maybe someone sends you a link to an important document, and you want to make sure it's in your Hister index for future reference.

```bash
hister index https://example.com/important-page
```

This command fetches the page and adds it to your index, just as if you had visited it in your browser.

## Privacy and Data Storage

### Everything Stays Local

- The browser extension sends data only to your local Hister server (typically `localhost`)
- Your browsing history never leaves your machine
- Your search queries never leave your machine
- No company, service, or third party sees what you browse or search for

### What Gets Stored

When you visit a page, Hister stores:

- The full text content of the page
- The page title and URL
- When you visited it
- The site's icon/favicon
- Metadata about the page

Hister uses an sqlite database as well to store search history data.

Both are stored in `~/.config/hister/` by default.

### What Doesn't Get Stored

Hister does not store:

- Your passwords or login credentials
- Form data you submit
- Cookies or session tokens
- Videos or large file downloads
- Images from the page (just the favicon)

### Controlling What Gets Indexed

You have complete control over what Hister indexes through the rules page. You can:

**Exclude specific domains:**

```textplain
google.com
youtube.com
facebook.com
twitter.com
reddit.com
```

This tells Hister to ignore pages from these sites entirely. They won't be captured, indexed, or searchable.

**Exclude URL patterns:**

```textplain
/admin/              # Skip admin pages
/account/            # Skip account pages
banking.example.com  # Skip banking sites
```

**Exclude by content type:**

The indexer automatically skips certain types of content like PDFs, videos, and downloads. It focuses on text content that makes sense to search.

### Data Security

Since everything is stored locally, your data security depends on your computer's security:

- Use skip rules to forbid indexing sensitive URLs
- Immediately delete URLs with sensitive data in case it was accidentally indexed. (Run `hister delete URL`)
- Use full-disk encryption
- Set up proper file permissions on the Hister directory
- Include the Hister directory in your regular backups - Your entire Hister history is stored in a single directory (by default `~/.config/hister/`). To back up everything, you simply copy this directory.

If your computer is compromised, your Hister data could be accessed (just like any other data on your computer)

The Hister server only listens on `localhost` by default, meaning it's not accessible from the network — only programs running on your computer can access it.

## Tips for Getting the Most Out of Hister

### 1. Import Your Browser History

When you first install Hister, take the time to import your existing browser history. This gives you months or years of pages to search through immediately, rather than starting from scratch.

### 2. Configure Skip Rules

Think about what you don't want to index. Social media feeds, shopping sites, and entertainment sites probably aren't useful to search through later. Add them to your skip rules to keep your index focused on useful content.

### 3. Use Priority Results

As you use Hister, notice which pages you search for repeatedly. Mark them as priority results with relevant search terms. This makes those important pages instantly accessible.

### 4. Learn the Keyboard Shortcuts

If you search frequently, keyboard shortcuts make Hister much faster:

- Press `?` to see all shortcuts
- `/` to focus search
- `Alt+j/k` to navigate results
- `Enter` to open
- `Alt+o` to fall back to web search

### 5. Use Field-Specific Searches

If you remember something about where you found information:

- `url:github.com` to search only GitHub pages
- `domain:stackoverflow.com` to search only Stack Overflow
- `title:tutorial` to search page titles

### 6. Take Advantage of Readable View

When browsing search results, use the "view" link to preview pages without opening them. This lets you quickly determine if it's what you're looking for.

### 7. Back Up Regularly

Your Hister database is valuable — it's your personal knowledge base. Include `~/.config/hister/` in your regular backup routine.

## Common Questions

**Q: Does Hister slow down my browsing?**
A: No. The browser extension is lightweight and works in the background. You won't notice any difference in browsing speed.

**Q: Does Hister store multimedia content?**
A: No. Hister only stores text data.

**Q: What happens if a website is deleted or changes?**
A: Hister stores the text content as it was when you visited, so you'll still be able to find and read it even if the original page is gone or changed.

**Q: Will Hister index pages in private/incognito mode?**
A: It depends on how you configure your browser extension. You can set the extension to work in private mode if you want, or disable it for privacy.

**Q: How is this different from browser bookmarks?**
A: Bookmarks require you to manually save pages and remember that you saved them. Hister automatically captures everything and lets you search by content, not just title or URL.

**Q: Can I access Hister from my phone?**
A: The web interface works on mobile browsers and Firefox mobile has extension support.
