---
date: '2026-04-30T19:00:00+02:00'
draft: false
title: 'Hister: The Most Privacy-Respecting Search Engine'
description: 'A detailed look at the privacy landscape of web search from big online engines to self-hosted metasearch, and why Hister offers a fundamentally stronger privacy guarantee than any of them.'
---

Privacy and web search are in constant tension. Every time you type a query into a search engine you are handing over something valuable: a window into what you are thinking. Most people accept this trade-off without much thought. This post breaks down the real privacy risks at every layer of the search stack and explains exactly where Hister fits in.

We will take a look at different privacy issues related to online search services, metasearch engines and opening search results. Then examining Hister's solutions to these problems.

## Privacy problems with web search

### Online search services

Popular search engines like Google, Bing, and their derivatives are advertising businesses. Your search queries are the raw material. They are stored, profiled, cross-referenced with your browsing history, location, and demographics, and used to model your behaviour far beyond any single search session.

The deeper structural issue is **unverifiability**. Even a search engine advertises it as privacy respecting, it cannot be audited. You cannot confirm whether queries are truly deleted, whether there is no logging, whether their system isn't compromised, whether your IP address is separated from your query log, or whether anonymised data is actually anonymised. You are asked to trust a black box operated by an organisation whose commercial interests are usually directly served by retaining as much data about you as possible.

### Self-hosted metasearch engines

Self-hosted metasearch engines like SearXNG (I'm the original author of Searx btw) and similar projects are a meaningful step forward. By routing your queries through your own server, they decouple significant amount of metadata from the query as seen by Google or Bing. That is a real and valuable guarantee.

But the guarantee has a hard ceiling: **metasearch engines are permanently dependent on external search providers**. Every query still leaves your infrastructure and travels (potentially both) to Google, Bing, or other providers. Those providers see a query, a timestamp, and an IP address (even if it is your server's IP rather than your personal IP). If the upstream provider correlates queries from the same source IP over time, or if your metasearch instance is the only one making requests from that IP, the anonymisation shrinks considerably.

Another important disadvantage is that metasearch engines can provide no protection against data leakage through the search queries. The search terms are always forwarded to the external providers even if the search query contains sensitive data.

### Visiting search results

This privacy surface is rarely discussed when talking about search engine privacy, but it is significant.

When you click a search result you visit a website that does not know you arrived from a private search engine. That website may load dozens of third-party trackers, advertising networks, analytics platforms, social widgets each of which observes your visit and can correlate it with your identity across the web. Many of these trackers operate at the network layer and cannot be blocked at the browser level without breaking the page.

Beyond passive tracking, pages can be tempered, can contain malicious scripts, credential-harvesting forms, or drive-by exploits. Before you visit a page you have no way to inspect it. The act of visiting is, in itself, an exposure.

## How Hister addresses each layer

### A fully local, self-contained index

Hister indexes content you choose to index: pages you visit via the browser extension, URLs you crawl explicitly, or local files on your machine. The index lives entirely on your own hardware. There is no remote server, no third-party cloud storage, no sync service. A query never leaves your infrastructure.

This eliminates the entire trust problem that applies to online services. There is nothing to verify because there is no external party involved. Your query log is a file on your machine that you control completely.

### No external search provider calls

Unlike metasearch engines, Hister does not call Google, Bing, or any other search provider at query time. The search runs entirely against the local index. There is no outbound network request triggered by a search query, not even to a self-hosted upstream.

This solves the biggest privacy issue of metasearch engines. Your queries produce zero external network traffic.

### Offline previews as a tracker firewall

Hister's most distinctive privacy feature is its **offline preview**. When a page is indexed, its readable content is stored locally. When you open a result in preview mode, you read the locally stored content.

This means you can read a result, follow an idea, and return to the page days later without the remote server ever knowing you visited. Trackers embedded in the page never execute. Third-party scripts never load. You are completely invisible to the origin and to every analytics or advertising service the site uses.

This is a qualitatively different protection from anything a browser extension or DNS-level blocker can offer, because the page content simply never reaches the network.

## Hister's honest limitations

No tool offers unlimited capability, and Hister is no exception.

**Index coverage.** Hister can only search what it has indexed. A conventional search engine has crawled hundreds of billions of pages; Hister has crawled whatever you have pointed it at. For exploratory searches on topics you have never researched before, the local index may come up empty.

**Indexing exposes you.** Building the index requires visiting pages. When the browser extension records a page you visit, or when you run `hister index` against a URL, a network request goes to the origin server. The privacy protections apply _after_ indexing, not during it. If you index a hostile page, that page can observe the visit. Hister mitigates this with support for a Chromedp backend and configurable headers and cookies, but the fundamental exposure during indexing cannot be eliminated entirely.

## Where does Hister stand?

|                               | Online search | Metasearch      | Hister                              |
| ----------------------------- | ------------- | --------------- | ----------------------------------- |
| Query leaves your machine     | Yes           | Yes (via proxy) | **No**                              |
| Dependent on external index   | Yes           | Yes             | **No**                              |
| Tracking when reading results | Yes           | Yes             | **No (offline preview)**            |
| Index coverage                | Comprehensive | Comprehensive   | Limited to what you index           |
| Verifiable privacy            | No            | Partial         | **Yes, free software, self-hosted** |

Hister is not the right tool for every search. It is the right tool when privacy is non-negotiable, when you need to be certain, not just hopeful, that your searches stay private.

If that matters to you, [try the demo](https://demo.nogon.eu/) or follow the [quickstart guide](/docs/quickstart) to run your own instance in minutes.
