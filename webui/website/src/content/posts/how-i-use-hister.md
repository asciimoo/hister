---
date: '2026-03-31T10:00:00+00:00'
draft: false
title: 'How I Use Hister'
description: 'A personal walkthrough of how I maximize productivity and privacy with Hister: instant access via global hotkeys, query aliases, skip rules, pre-indexing reference material, and seamless fallback to external search engines.'
---

In this post I'd like to share how I ([@asciimoo](https://github.com/asciimoo/) - the author of Hister) use Hister to maximize my productivity and privacy.

The tools I mention are interchangeable: the concepts apply equally well to different environments and operating systems.

## The Search Workflow

My core workflow comes down to three steps:

1. Open Hister as quickly as possible
2. Check with the fewest keystrokes whether the result is already in Hister
3. Fall back to a traditional search engine if nothing relevant is found

Let me walk through each step.

### 1. Opening Hister Instantly

Setting Hister as your browser's default search engine is a good start, but it still requires you to switch to a browser window, open a new tab, and hit Enter before results appear.

A much faster approach is to bind a **global hotkey** in your window manager that jumps directly to your browser and opens a new Hister tab. With this setup, a two-key combination always puts a fresh search prompt in front of you.

I use the i3 window manager. The relevant config line is:

```
bindsym Mod4+s exec xdg-open "http://127.0.0.1:4433/"
```

`xdg-open` (part of the freedesktop.org `xdg-utils` package) opens its argument with whichever application is registered for that URL scheme: typically your default browser.

My default URL-opening application for `xdg-open` is a small shell script:

```bash
#!/bin/sh
chromium --incognito "$1"
i3-msg "workspace web"
```

This spawns a new browser window (or tab, if a browser is already running) and then tells i3 to switch to my browser workspace. If you use multiple browsers, use this instead of the last `i3-msg` command:

```bash
sleep 0.1 && i3-msg "[urgent=latest] focus"
```

This waits briefly for the new window to appear and focuses it by urgency hint, ensuring you land in the right browser regardless of which one opened.

Note: `xdg-open` is fully optional. You can configure your hotkeys to directly manage focus and browser opening. I prefer using `xdg-open` to get consistent behaviour across all my applications.

---

Even if your setup is completely different, the principle is the same: **bind a hotkey that opens a new browser tab with Hister and focuses it**.

### 2. Making Searches Efficient

There are three things I rely on to keep searches fast inside Hister.

#### Learning the Query Language

The [query language](/docs/query-language) lets you quickly narrow down results. I regularly use field filters, exclusions, and synonyms to get precise results in fewer keystrokes: for example, `domain:github.com -type:local indexer` to find GitHub pages about the indexer while excluding local files.

#### Search Aliases

I define aliases for recurring search patterns. There are two flavours I use:

**Synonyms**: when a topic has multiple common names, I use one as the alias. For example, `go` resolves to `(go|golang)` so I always find both spellings without thinking about which was used on a given page.

**Targeted filters**: for context-specific searches I use a `!` prefix to make the alias distinct from regular words. A good example is `!hi` ("Hister issues"), which resolves to `url:https://github.com/asciimoo/hister/issues/*`. Typing `!hi indexer` instantly lists all Hister GitHub issues mentioning the indexer.

Aliases like these compress multi-word filter expressions into a single short token, which makes repeated searches faster.

#### Keyboard Navigation

Hister's hotkeys let you move through results and open the **readability view** (`alt+v` by default) entirely from the keyboard. The readability view renders a clean version of the page directly inside Hister, so I can often get the information I need without ever leaving the search interface.

Configure the hotkeys to match your habits: the defaults are a reasonable starting point (especially for vim users).

### 3. Falling Back to External Search

Hister adds overhead to the search workflow when the information you need isn't in your index. My goal is to make this overhead as small as possible.

There are two distinct situations:

#### 1. You Know Before Searching That It Won't Be in Hister

Start or end your query with `!!` and press Enter. Hister immediately redirects to your configured external search engine with the same query. This works both from the Hister interface and from the browser's URL bar when Hister is set as the default search engine. The only overhead is two extra characters.

#### 2. You Discover Mid-Search That the Result Isn't There

Press the configured hotkey (`Alt+o` by default) or click the **Web** link below the search input. This opens the current query in your configured search engine without you having to retype it. You avoid switching to the search engine's page and re-entering the query manually.

## Keeping the Index Clean

Aside the search workflow optimiziation, efficiency can be increased with a well maintained index.

A growing index is only useful if it stays relevant. Two habits help with this.

### Skip Rules

Not every page you visit is worth indexing. E.g. social media feeds add noise without adding value. I use skip rules to prevent them from landing in the index in the first place.

I like to pay attention in general to my index and add skip rules for patterns of noise.

### Pruning Stale Entries

Even with good skip rules, the index accumulates entries that become irrelevant: accidentally opened content, documentation for libraries I no longer use or pages for projects I abandoned. I do pruning every time I discover useless content in my index using the `delete` command:

```bash
hister delete "domain:old-framework.io"
hister delete "url:https://jobs.example.com/*"
```

The `--dry` flag lets you preview what would be deleted before committing:

```bash
hister delete --dry "domain:old-framework.io"
```

You can also prune directly from the search web UI. Enter a query such as `domain:old-framework.io`, review the results, and adjust the query until it selects the content you want to remove. Then open **Actions**, click **Delete all matching results**, and confirm. Like `hister delete`, this deletes every document matching the query. Being able to refine the query while reviewing matches makes it quick to clean up unwanted results without leaving the search page.

## Pre-indexing Reference Material

The browser extension indexes pages as you visit them, which means documentation you have never opened is invisible to Hister. I close this gap by using the crawler to pre-index reference material I expect to look up repeatedly.

```bash
hister index --recursive --allowed-pattern=pkg.go.dev/some/library https://pkg.go.dev/github.com/some/library
```

This crawls the library's documentation and adds everything to the index.

I use this for:

- API and library documentation for tools I use regularly
- Project wikis and internal documentation
- Long-form reference pages I know I will return to

## Conclusion

A bit of upfront configuration (a global hotkey, a handful of aliases, skip rules, and familiarity with the query syntax) makes Hister significantly more effective as a daily tool. Pre-indexing reference material and periodically pruning stale entries keeps the index sharp as it grows.

I'll keep refining the workflow and I'm always interested in how you use it.

Share your tips, use cases, and ideas on [GitHub](https://github.com/asciimoo/hister), [Discord](https://discord.gg/beEyuHxRSs), [Codeberg](https://codeberg.org/asciimoo/hister), or in `#hister` on IRCNet.
