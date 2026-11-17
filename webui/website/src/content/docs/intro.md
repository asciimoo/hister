---
date: '2026-08-01T00:00:00+02:00'
draft: false
title: 'What Nogon Is'
description: 'Understand what Nogon indexes, how retrieval works, who it is for, and where your data remains.'
---

Nogon was formerly known as Hister.

Nogon is a private, self hosted search engine for pages you visit and files you keep. It indexes their full contents so you can find information again from the web interface, terminal, API, or an AI assistant connected through MCP.

Nogon searches your own collection. It is not a general web search engine or a managed cloud service.

<span id="who-hister-is-for"></span>

## Who Nogon Is For

Nogon is for anyone who wants to create a searchable knowledge base from information they consider relevant or important. It brings content from different sources into one collection you control, including web pages, browser history, bookmarks, local files, and crawled sites.

It is not a hosted service, automatic cloud sync system, or records management system. You operate the server and decide how its data is secured and retained.

## How It Works

Nogon uses a client and server architecture. The server stores and searches documents. Clients collect content or run searches. The `nogon` program can act as both, while the browser extension is a client. Everything can run on one computer, or several clients can connect to one server.

Some packages still provide the `hister` command. Use that name in place of `nogon` if it is what your installation provides.

Content can come from the browser extension, imported browser history or bookmarks, watched directories, crawlers, or API clients. The extension captures rendered pages as you visit them. Browser imports read URLs from history or saved bookmarks and fetch their current contents. See [Browser Ingestion](browser-ingestion) to choose how to collect content.

<span id="privacy"></span>

## Privacy and Storage

Nogon has no telemetry and requires no Nogon cloud service. Indexed data is stored on the server you choose and is not encrypted by Nogon. Use disk encryption when needed, and HTTPS when clients connect across an untrusted network.

Documents have no automatic expiry or total storage quota. For retention, storage limits, deletion, versioning, and backups, see [Data Storage and Lifecycle](data-lifecycle).

## Get Started

- Follow the [Quickstart](quickstart) for a local personal setup.
- Review [Server Setup](server-setup) before allowing network access.
