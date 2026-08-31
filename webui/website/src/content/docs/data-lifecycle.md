---
date: '2026-08-01T00:00:00+02:00'
draft: false
title: 'Data Storage and Lifecycle'
description: 'Learn where Hister stores data and how updates, previews, deletion, cleanup, and backups behave.'
---

On average, storing one page requires around 100 KB of disk space.

Hister keeps current searchable documents until you replace or delete them. It does not apply an automatic expiry period, a maximum document count, or a total disk quota. Some related SQL records can remain after current document deletion. The server operator is responsible for monitoring storage, creating backups, and applying any required deletion schedule.

## Lifecycle at a Glance

| Event                                                 | Result                                                                                                          |
| ----------------------------------------------------- | --------------------------------------------------------------------------------------------------------------- |
| A new URL is submitted                                | Hister creates a searchable document for that URL and owner                                                     |
| The same normalized URL is submitted again            | Hister replaces the current document content and increases its submission count                                 |
| A versioning rule matches a changed page              | Hister also stores a difference record in the SQL database                                                      |
| A watched file changes                                | Hister updates its searchable document                                                                          |
| A watched file is removed                             | Hister keeps the document unless `delete_on_remove: true` is configured                                         |
| A remote file snapshot is imported again              | Hister replaces the document with the same source and absolute path                                             |
| `hister update` changes document attributes           | Hister updates matching current records and moves identity state when ownership changes                         |
| The source of a remote file snapshot changes or moves | No change occurs until the client imports it again                                                              |
| A source bookmark or browser history entry is removed | No change occurs in Hister                                                                                      |
| A document is deleted in Hister                       | The current index record and associated current assets are removed                                              |
| `hister cleanup` is run                               | Local documents that no longer match configured directories and unreferenced HTML and favicon files are removed |

## Where Data Lives

The `app.directory` setting is the main Hister data directory. A default SQLite deployment stores these items there:

| Location                       | Contents                                                                                                |
| ------------------------------ | ------------------------------------------------------------------------------------------------------- |
| `index.db` and `index_LANG.db` | Searchable document fields and full text indexes                                                        |
| `data/html/`                   | Compressed HTML previews, addressed by content hash                                                     |
| `data/favicon/`                | Compressed favicons, addressed by content hash                                                          |
| `db.sqlite3`                   | Users, browser sessions, search result history, crawl jobs, version differences, and internal job state |
| `vectors.sqlite3`              | Semantic search chunks and vectors when semantic search uses SQLite                                     |
| `rules.json`                   | Rules and aliases in single user mode                                                                   |
| `.secret_key`                  | The secret used to derive authentication proofs for global token sessions                               |

Language detection can create more than one `index_LANG.db` directory. Identical HTML or favicons share the same content addressed file, which reduces duplicate storage.

When `server.database` is a PostgreSQL connection string, SQL data is stored in PostgreSQL instead of `db.sqlite3`. Semantic vectors are also stored in PostgreSQL. The Bleve search indexes and HTML and favicon data remain under `app.directory`.

Browser session records contain session state and a hash of the random identifier sent to the browser. They never contain the configured application access token or the raw session identifier. Sessions expire thirty days after the most recent valid request or immediately after logout. Each valid request refreshes both the database expiry and browser cookie expiry. Expired records are removed during session activity.

Configuration files can live outside `app.directory`. Source files in watched directories are not copied into the SQL database, but their extracted content is placed in the search index. Remote file imports extract content in the command line process and send only prepared document fields to the server. The original bytes remain on the client.

## Storage Limits

Hister does not enforce a total storage limit. Available disk space and the selected database backend are the practical limits.

Two settings are easy to mistake for quotas:

- `indexer.max_file_size_mb` limits an individual watched file or remote file snapshot source. Its default is 1 MiB. It does not limit browser pages, serialized document imports, or total index size.
- `server.max_batch_body_size` limits one batch request or JSON add request. Its default is 40 MiB. The setting is not a total storage quota.

Full HTML previews, large document text, version differences, and semantic vectors all increase storage use. Browser history size alone is not a reliable estimate because page content varies greatly.

To reduce growth:

- Set `app.disable_previews: true` if saved HTML previews are not needed.
- Use skip rules to exclude noisy, private, or low value URLs before capture.
- Apply versioning rules only to pages whose earlier content matters.
- Delete stale documents with a date query after reviewing the matches.
- Remove completed persistent crawl jobs when their resume data is no longer useful.
- Monitor the data directory, PostgreSQL database, and backup storage with the tools used for your deployment.

See [Storage by Domain](#storage-by-domain) to find which sites are worth acting on first.

## Storage by Domain

Storage is reported per domain, largest first, in the **Storage** page of the web interface and with the command line client:

```bash
hister stats domains
```

Four figures are given for each domain:

| Column  | Meaning                                                  |
| ------- | -------------------------------------------------------- |
| Text    | Length of the indexed text                               |
| HTML    | Size of the stored HTML previews on disk                 |
| Favicon | Size of the stored favicons on disk                      |
| Shared  | Stored data the domain references but does not own alone |

HTML and favicon figures are exact compressed sizes on disk. The text figure estimates a domain's share of the search index rather than measuring it, because the index also holds term vectors and postings that belong to no single document.

Stored data is content addressed, so identical content is stored once and can be referenced by several domains. Such files are reported in the Shared column and excluded from a domain's total, because deleting that domain alone does not release them. A domain whose pages are all duplicated elsewhere therefore shows a small total and a large shared figure.

The totals answer one question: how much a domain would release if it were deleted. Once a domain is identified, remove it as described in [Deleting Documents](#deleting-documents).

The report reads every indexed document, so it takes longer on a large index. It never reads the stored HTML files themselves, only their sizes.

## Updates and Versioning

The document identity is its normalized URL plus its owner. Hister removes URL fragments and common `utm` tracking parameters during normalization. Several submissions can therefore resolve to one current document.

On a normal revisit, the new title, text, HTML preview, metadata, and update time replace the prior current values. If the previous HTML or favicon file is no longer referenced by another document, Hister removes that file.

Versioning is separate. When a URL matches a versioning rule and content changes, Hister stores text and HTML differences in the SQL database. These records let the preview interface reconstruct earlier content. Version records have no automatic age or count limit.

Disabling a versioning rule stops future differences from being recorded. It does not remove differences already stored.

## Preview Retention

Full HTML previews are enabled by default. To stop storing new HTML:

```yaml
app:
  disable_previews: true
```

Restart Hister after changing the setting. Running `hister reindex` while previews are disabled removes previously stored HTML files and rebuilds the search indexes without preview content. Extracted plain text remains searchable. Favicons are not affected.

See [Disable Previews](configuration#disable-previews) for the user interface effects.

## Deleting Documents

Delete one result from the web or terminal interface, or delete every document matching a query with the command line client:

```bash
hister delete 'domain:example.com'
```

Always preview a broad deletion query first:

```bash
hister delete --dry --verbose 'updated:>90d'
```

Relative time filters describe elapsed age, so `updated:>90d` matches documents not updated for more than 90 days. The command asks for confirmation unless `--yes` is supplied.

For each matching current document, deletion removes:

- The searchable document from the Bleve indexes.
- Its HTML preview and favicon when no other indexed document references the same content.
- Its semantic chunks and pending embedding job.
- Matching opened result history associations for the same URL and owner. These associations use soft deletion in the SQL database.

Document deletion does not remove:

- The source browser history entry, bookmark, local file, or remote page.
- Stored version differences in the SQL database.
- Persistent crawl job records.
- Server logs, exported JSON files, filesystem snapshots, or backups.
- Search history records that are unrelated to the deleted URL.

If a complete erasure is required, the server operator must also address version records, job metadata, logs, exports, and backups according to the deployment. Hister does not currently provide one command that erases every related copy.

### Avoiding Automatic Reappearance

A deleted document can return if an active collector submits it again. Before deletion, pause the browser extension, add a skip rule, remove the URL from a crawl queue, or change the watched directory configuration as appropriate.

For watched files, `delete_on_remove: true` makes future file removal delete the corresponding indexed document automatically. This option is disabled by default.

## Cleanup And Local Files

`hister cleanup` reconciles indexed local document paths with the current directory configuration, then scans stored HTML and favicon files and removes files that no current indexed document references:

```bash
hister cleanup
```

During local document reconciliation, cleanup removes indexed documents that are outside the configured directories, no longer match their `filetypes`, `patterns`, `excludes`, or hidden path settings, or have a different configured owner. It evaluates fields already stored in the index and does not walk, stat, or read local files. Cleanup therefore does not discover new files, update changed files, or detect missing source files. Startup directory indexing and the file watcher handle those operations. Cleanup reports how many indexed local documents it checked, skipped, and removed.

Normal updates and deletions already attempt to remove unreferenced assets. The stored data cleanup handles leftovers, such as files left by an interrupted operation. Cleanup does not delete web documents, version differences, crawl jobs, or search history.

## Crawl Job Lifecycle

Persistent crawls and browser imports keep queue state in the SQL database so they can resume after interruption. Completed and interrupted job records remain available for inspection, together with their completed and failed URL rows.

List and remove them explicitly:

```bash
hister crawl list
hister crawl delete JOB_ID
```

Deleting a crawl job removes its queue and status records. It does not delete documents that the job already indexed.

## Multi User Ownership

All users share the server storage and search index files, but each current document has an owner ID. Normal searches for an authenticated user include that user's documents and global documents owned by user ID `0`.

A regular user can delete only their own current documents. An administrator deletion request has no automatic owner restriction and can affect other users. The command line `--dry` check uses normal administrator search scope, so it does not reveal matching documents owned by other users. On a shared server, constrain an administrator deletion query with `user_id:ID` and never infer its full impact from the dry run count alone.

Use `hister update QUERY --user-id ID` to change ownership without exporting and importing current
documents. The operation preserves current indexed content and stored version records. It skips a
move when the destination owner already has the same URL. Search history remains associated with
the user who created it. A regular user can update labels, titles, and languages only on their own
documents, while ownership changes require an administrator.

Deleting a user with `hister delete-user USERNAME --purge` asks the server to remove current indexed documents detected by its preflight, then soft deletes the account. Verify the result because this is not complete data erasure and records or current documents missed by the preflight can remain. See [`delete-user`](user-handling#delete-user) for details.

## Backups and Exports

For a complete server backup, stop Hister and back up the entire `app.directory`, plus any configuration and `tui.yaml` files stored elsewhere. PostgreSQL deployments also require a database backup. Watched source files need their own backup plan. Remember that exports, filesystem snapshots, container volume backups, and remote database backups create additional retained copies.

`hister export` exports current indexed documents and any current preview content available to the caller. It is useful for migration, but it is not a complete server backup. It does not preserve user accounts, search result history, crawl jobs, stored version differences, rules, sessions, or all server configuration.

Protect backups as carefully as the live server because they can contain full page text, private previews, local file content, and credentials accidentally captured from pages.
