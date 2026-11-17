---
date: '2026-08-01T00:00:00+02:00'
draft: false
title: 'Data Storage and Lifecycle'
description: 'Learn where Nogon stores data and how updates, previews, deletion, cleanup, and backups behave.'
---

On average, storing one page requires around 100 KB of disk space.

Nogon keeps current searchable documents until you replace or delete them. It does not apply an automatic expiry period, a maximum document count, or a total disk quota. Some related SQL records can remain after current document deletion. The server operator is responsible for monitoring storage, creating backups, and applying any required deletion schedule.

## Lifecycle at a Glance

| Event                                                    | Result                                                                                                          |
| -------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------- |
| A new URL is submitted                                   | Nogon creates a searchable document for that URL and owner                                                      |
| The same normalized URL is submitted again               | Nogon replaces the current document content and increases its submission count                                  |
| A versioning rule matches a changed page                 | Nogon also stores a difference record in the SQL database                                                       |
| A watched file changes                                   | Nogon updates its searchable document                                                                           |
| A watched file is removed                                | Nogon keeps the document unless `delete_on_remove: true` is configured                                          |
| A remote file snapshot is imported again                 | Nogon replaces the document with the same source and absolute path                                              |
| `nogon update` changes document attributes               | Nogon updates matching current records and moves identity state when ownership changes                          |
| The source of a remote file snapshot changes             | An active `nogon import file --watch` updates it; otherwise it changes on the next import                       |
| The source of a remote file snapshot moves or is removed | The old snapshot remains; watch mode imports a new path if it is inside a watched directory                     |
| A source bookmark or browser history entry is removed    | No change occurs in Nogon                                                                                       |
| A document is deleted in Nogon                           | The current index record and associated current assets are removed                                              |
| `nogon cleanup` is run                                   | Local documents that no longer match configured directories and unreferenced HTML and favicon files are removed |

## Where Data Lives

The `app.directory` setting is the main Nogon data directory. A default SQLite deployment stores these items there:

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

Browser session records contain session state and a hash of the random identifier sent to the browser. They never contain the configured application access token or the raw session identifier. Sessions expire thirty days after the most recent valid request or immediately after logout. Changing a user's password invalidates their existing browser sessions, including OAuth sessions. Those invalidated records remain until their normal expiration and cleanup. Each valid request refreshes both the database expiry and browser cookie expiry. Expired records are removed during session activity.

Configuration files can live outside `app.directory`. Source files in watched directories are not copied into the SQL database, but their extracted content is placed in the search index. Remote file imports extract content in the command line process and send only prepared document fields to the server. The original bytes remain on the client.

## Storage Limits

Nogon does not enforce a total storage limit. Available disk space and the selected database backend are the practical limits.

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

## Updates and Versioning

The document identity is its normalized URL plus its owner. Nogon removes URL fragments and common `utm` tracking parameters during normalization. Several submissions can therefore resolve to one current document.

On a normal revisit, the new title, text, HTML preview, metadata, and update time replace the prior current values. If the previous HTML or favicon file is no longer referenced by another document, Nogon removes that file.

Versioning is separate. When a URL matches a versioning rule and content changes, Nogon stores text and HTML differences in the SQL database. These records let the preview interface reconstruct earlier content. Version records have no automatic age or count limit.

Disabling a versioning rule stops future differences from being recorded. Use [purge](#purging-retained-data) to remove old differences already stored.

## Preview Retention

Full HTML previews are enabled by default. To stop storing new HTML:

```yaml
app:
  disable_previews: true
```

Restart Nogon after changing the setting. Running `nogon reindex` while previews are disabled removes previously stored HTML files and rebuilds the search indexes without preview content. Extracted plain text remains searchable. Favicons are not affected.

See [Disable Previews](configuration#disable-previews) for configuration details.

## Deleting Documents

Delete one result from the web or terminal interface, or prune every document matching a search query from the search web UI or command line client.

Bulk deletion in the web interface has the same scope as `nogon delete`: it removes every
document matching the query, including matches beyond the currently displayed results.

To delete matching documents from the command line:

```bash
nogon delete 'domain:example.com'
```

Always preview a broad deletion query first:

```bash
nogon delete --dry --verbose 'updated:>90d'
```

Relative time filters describe elapsed age, so `updated:>90d` matches documents not updated for more than 90 days. The command asks for confirmation unless `--yes` is supplied.

Dry runs and confirmation prompts count documents using the same permissions and query as deletion. `--verbose` lists matching URLs visible in search, which can differ from the documents selected for deletion on a shared server. The count includes every deletion match even when its URL is not listed. Documents added or changed after the preview can affect the final deletion.

For each matching current document, deletion removes:

- The searchable document from the Bleve indexes.
- Its HTML preview and favicon when no other indexed document references the same content.
- Its semantic chunks and pending embedding job.
- Matching opened result history associations for the same URL and owner. Unreferenced query and URL records can remain.

Document deletion does not remove:

- The source browser history entry, bookmark, local file, or remote page.
- Stored version differences in the SQL database.
- Persistent crawl job records.
- Server logs, exported JSON files, filesystem snapshots, or backups.
- Search history records that are unrelated to the deleted URL.

If a complete erasure is required, the server operator must also address version records, job metadata, logs, exports, and backups according to the deployment. Nogon does not currently provide one command that erases every related copy.

### Avoiding Automatic Reappearance

A deleted document can return if an active collector submits it again. Before deletion, pause the browser extension, add a skip rule, remove the URL from a crawl queue, or change the watched directory configuration as appropriate.

For watched files, `delete_on_remove: true` makes future file removal delete the corresponding indexed document automatically. This option is disabled by default.

## Cleanup And Local Files

`nogon cleanup` reconciles indexed local document paths with the current directory configuration, then scans stored HTML and favicon files and removes files that no current indexed document references:

```bash
nogon cleanup
```

During local document reconciliation, cleanup removes indexed documents that are outside the configured directories, no longer match their `filetypes`, `patterns`, `excludes`, or hidden path settings, or have a different configured owner. It evaluates fields already stored in the index and does not walk, stat, or read local files. Cleanup therefore does not discover new files, update changed files, or detect missing source files. Startup directory indexing and the file watcher handle those operations. Cleanup reports how many indexed local documents it checked, skipped, and removed.

Normal updates and deletions already attempt to remove unreferenced assets. The stored data cleanup handles leftovers, such as files left by an interrupted operation. Cleanup does not delete web documents, version differences, crawl jobs, or search history.

## Purging Retained Data

Use `nogon purge` to permanently remove selected retained data from the running server. It applies across all users and requires administrator access in multi user mode. Current searchable documents and their previews remain intact.

Choose at least one category and an explicit cutoff:

```bash
nogon purge --versions --history --crawl-jobs --before 2026-01-01 --dry
nogon purge --versions --history --crawl-jobs --before 2026-01-01
```

| Flag           | Records removed                                                                                                                 |
| -------------- | ------------------------------------------------------------------------------------------------------------------------------- |
| `--versions`   | Stored version differences created before the cutoff, including versions of deleted documents                                   |
| `--history`    | Unpinned opened result history last used before the cutoff, plus old query and URL records with no remaining history references |
| `--crawl-jobs` | Completed crawl jobs last updated before the cutoff and their URL records                                                       |

Pinned history, running or interrupted crawl jobs, and jobs with pending URLs remain. Removing version differences makes those earlier versions unavailable. Removing crawl records does not delete the documents they indexed.

`--before` accepts a date at midnight UTC or an RFC3339 timestamp with a timezone. Records exactly at the cutoff remain. The command previews counts before asking for confirmation; `--dry` stops after the preview and `--yes` skips confirmation. Counts can change between preview and deletion if other work modifies the data. If a database error interrupts deletion, Nogon rolls back the purge.

Purging does not erase source files, browser history, logs, exports, or backups, and does not guarantee immediate disk space reclamation. It does not configure automatic expiry.

## Crawl Job Lifecycle

Persistent crawls and browser imports keep queue state in the SQL database so they can resume after interruption. Completed and interrupted job records remain available for inspection, together with their completed and failed URL rows.

List and remove them explicitly:

```bash
nogon crawl list
nogon crawl delete JOB_ID
```

Deleting a crawl job removes its queue and status records. It does not delete documents that the job already indexed.

## Multi User Ownership

All users share the server storage and search index files, but each current document has an owner ID. Normal searches for an authenticated user include that user's documents and global documents owned by user ID `0`.

A regular user can delete only their own current documents. An administrator deletion request has no automatic owner restriction and can affect other users. The command line `--dry` count includes those documents. On a shared server, constrain an administrator deletion query with `user_id:ID` to select one owner's documents.

Use `nogon update QUERY --user-id ID` to change ownership without exporting and importing current
documents. The operation preserves current indexed content and stored version records. It skips a
move when the destination owner already has the same URL. Search history remains associated with
the user who created it. A regular user can update labels, titles, and languages only on their own
documents, while ownership changes require an administrator.

Deleting a user with `nogon user delete USERNAME --purge` removes the named user's current indexed documents and then soft deletes the account on the same server. This requires an administrator token. Stop imports and crawls for that user first because deletion does not block work already in progress. Verify the result because this is not complete data erasure; stored versions, crawl records, backups, and other retained data can remain. See [`user delete`](user-handling#user-delete) for details.

## Backups and Exports

For a complete server backup, stop Nogon and back up the entire `app.directory`, plus any configuration and `tui.yaml` files stored elsewhere. PostgreSQL deployments also require a database backup. Watched source files need their own backup plan. Remember that exports, filesystem snapshots, container volume backups, and remote database backups create additional retained copies.

`nogon export` exports current indexed documents and any current preview content available to the caller. It is useful for migration, but it is not a complete server backup. It does not preserve user accounts, search result history, crawl jobs, stored version differences, rules, sessions, or all server configuration.

Protect backups as carefully as the live server because they can contain full page text, private previews, local file content, and credentials accidentally captured from pages.
