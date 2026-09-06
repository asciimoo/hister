---
date: '2026-07-16T12:00:00+02:00'
draft: false
title: 'Website Crawler'
description: 'Index individual URLs or run persistent crawls with scope rules, browser backends, and resumable jobs.'
---

Hister can fetch individual pages, crawl links recursively, or process a saved list of URLs from
the terminal. Persistent crawl jobs keep their queue in the database, so a long crawl can be
stopped and continued later without starting over.

The two relevant commands have different responsibilities:

| Command        | Purpose                                                                      |
| -------------- | ---------------------------------------------------------------------------- |
| `hister index` | Fetch pages, extract their content, send documents to Hister, and run jobs.  |
| `hister crawl` | Inspect or delete persistent job records. It does not fetch or resume pages. |

The Hister server must be running while `hister index` adds documents. Global connection options
such as `--server-url`, `--token`, and `--client-timeout` are covered in the
[Terminal Client](terminal-client) documentation.

## Choose an Index Mode

The arguments passed to `hister index` select one of four modes:

| Invocation                                     | Persistent | Follows links | Result                                                |
| ---------------------------------------------- | ---------- | ------------- | ----------------------------------------------------- |
| `hister index URL...`                          | No         | No            | Fetch each positional URL once.                       |
| `hister index --recursive [--job-id NAME] URL` | Yes        | Yes           | Create or resume a recursive crawl job.               |
| `hister index --input FILE`                    | Yes        | No            | Queue every URL in the input without following links. |
| `hister index --job-id NAME`                   | Yes        | Saved setting | Resume the pending queue of an existing job.          |

Add `--recursive` to `--input` when links discovered on the listed pages should also be queued.

## Index Individual URLs

Pass one or more URLs without `--recursive`, `--job-id`, or `--input` for a direct indexing run:

```bash
hister index https://example.com/a https://example.com/b
```

This mode does not create a crawl job. Each URL is fetched once and discovered links are ignored.
Already indexed URLs are skipped unless `--force` is present:

```bash
hister index --force https://example.com/a
```

Direct indexing supports the same backend, header, cookie, timeout, ownership, and labeling flags
as persistent crawling.

## Start a Persistent Recursive Crawl

Use `--recursive`, or its short form `-r`, with a starting URL:

```bash
hister index --recursive https://example.com
```

Hister creates a job with a random eight character ID and prints it before fetching begins:

```text
Starting crawl job: a1b2c3d4
```

Keep that ID to inspect or resume the crawl later. The queue is processed breadth first. Every
discovered URL is recorded with its depth and status before the crawl moves on.

By default, recursive crawling is not restricted to the starting domain. Add `--allowed-domain`
when the crawler should remain on one site:

```bash
hister index --recursive \
  --allowed-domain example.com \
  https://example.com
```

The allowed domain also includes its subdomains. Repeat the flag to permit more than one domain.

## Give a Job a Custom Name

The job ID is also its human readable name. Supply one with `--job-id` when creating the crawl:

```bash
hister index --recursive \
  --job-id example-docs \
  --allowed-domain docs.example.com \
  https://docs.example.com
```

If `example-docs` does not exist, this command creates it. If it already exists, the same command
resumes it. This makes named jobs convenient in scripts because the creation and continuation
command can be identical:

```bash
hister index --recursive \
  --job-id example-docs \
  --allowed-domain docs.example.com \
  https://docs.example.com
```

When the ID already exists, Hister uses the starting URL and traversal rules saved in that job.
Positional URLs and traversal flags from the new invocation do not replace the saved values.

Creating a new named job requires all three of these pieces: `--recursive`, an unused `--job-id`,
and at least one URL. Passing an unknown job ID without `--recursive` produces an error instead of
silently creating a job.

## Stop and Resume a Job

It is safe to stop a persistent crawl. To continue it with the shortest form, pass only its ID:

```bash
hister index --job-id example-docs
```

There is no `hister crawl resume` subcommand. Resuming is always done through `hister index`.

On resume, Hister restores the saved starting URL, traversal rules, label, and URL queue. Any URL
left with `in_progress` status is moved back to `pending`. URLs already marked `done`, `failed`, or
`skipped` are not fetched again. Pending URLs continue in their original queue order.

A process stopped abruptly can leave the job status as `running`. This does not mean its queue was
lost. Use `hister crawl show NAME` to check the queue counts, then resume it normally.

### Saved Settings and Runtime Settings

The settings that define crawl scope are stored when a job is created:

| Saved with the job     | Examples                                                      |
| ---------------------- | ------------------------------------------------------------- |
| Starting URL           | `https://docs.example.com`                                    |
| Traversal rules        | `--max-depth`, `--max-links`, domain flags, and pattern flags |
| Recursive behavior     | Whether discovered links are added to the queue               |
| Initial document label | `--label docs`                                                |
| Queue and URL statuses | Pending, in progress, done, failed, and skipped URL records   |

Fetch and destination settings are not restored from the job. Keep them in
[crawler configuration](configuration#crawler-section), repeat them when resuming, or rerun the
same complete command. These include `--backend`, `--backend-option`, `--proxy`, `--header`, `--cookie`,
`--delay`, `--timeout`, `--user-agent`, `--no-robots`, `--force`, `--global`, and `--user-id`.

For example, resume a job with Chromium and a one second request delay:

```bash
hister index --job-id example-docs \
  --backend chromedp \
  --delay 1
```

An explicit `--label` on a resume changes the label for documents indexed during that invocation.
Without it, Hister uses the original stored label.

## Queue URLs From Input

Use `--input` with a text file containing one URL per line:

```text
https://example.com/guide
https://example.com/reference
https://example.org/manual
```

```bash
hister index --input documentation-urls.txt
```

Blank lines and surrounding whitespace are ignored. Positional URLs are ignored when input is
present. Empty input produces an error.

Before fetching starts, Hister creates a persistent job and inserts the complete list into its
queue in one database transaction. The file base name becomes the job ID. In this example, the ID
is `documentation-urls.txt`. If that ID exists, Hister tries `documentation-urls.txt-2`, then
`documentation-urls.txt-3`, and continues increasing the suffix until it finds a free name.

To choose a recognizable name for this kind of job, give the file that name before running the
command. `--input` and `--job-id` cannot be used together.

Running the `--input` command again creates a new suffixed job. It does not continue the earlier
one. Resume the printed job ID instead:

```bash
hister index --job-id documentation-urls.txt
```

Only the listed URLs are fetched by default. Add `--recursive` to follow links from every seed URL:

```bash
hister index --recursive \
  --input documentation-urls.txt \
  --allowed-domain example.com
```

Use `--input -` to read URLs from standard input:

```bash
printf '%s\n' https://example.com/guide https://example.com/reference | hister index --input -
```

Standard input jobs use `stdin` as their base ID. Later jobs use `stdin-2`, `stdin-3`, and so on.

## Control Crawl Scope

Traversal flags apply when a persistent job is created and are then stored with it:

| Flag                       | Behavior                                                                             |
| -------------------------- | ------------------------------------------------------------------------------------ |
| `--recursive`, `-r`        | Follow discovered links. Without it, only explicitly queued URLs are fetched.        |
| `--max-depth N`            | Visit links up to depth `N`. The starting URL has depth zero. Zero means unlimited.  |
| `--max-links N`            | Stop after allowing `N` pages. Zero means unlimited.                                 |
| `--allowed-domain DOMAIN`  | Allow this domain and its subdomains. Repeatable. An empty list allows every domain. |
| `--exclude-domain DOMAIN`  | Skip this domain and its subdomains. Repeatable.                                     |
| `--allowed-pattern REGEXP` | Fetch URLs matching at least one allowed regular expression. Repeatable.             |
| `--exclude-pattern REGEXP` | Skip URLs matching any excluded regular expression. Repeatable.                      |

Domain and pattern checks apply to the starting URL too. Make sure an allowed pattern includes the
seed URL.

This example crawls at most 500 pages from two documentation domains while excluding sign in pages:

```bash
hister index --recursive \
  --job-id product-docs \
  --allowed-domain docs.example.com \
  --allowed-domain api.example.com \
  --exclude-pattern '/(login|logout)(/|$)' \
  --max-depth 5 \
  --max-links 500 \
  https://docs.example.com
```

## Select a Fetch Backend

The default `http` backend is fast and works well for server rendered pages. Hister also supports
`chromedp` for pages that need Chrome or Chromium, and `bidi` for an already running browser with a
WebDriver BiDi endpoint.

```bash
hister index --recursive \
  --job-id dynamic-docs \
  --backend chromedp \
  --backend-option exec_path=/usr/bin/chromium \
  --allowed-domain docs.example.com \
  https://docs.example.com
```

The request related flags are:

| Flag                         | Behavior                                                                           |
| ---------------------------- | ---------------------------------------------------------------------------------- |
| `--backend NAME`             | Select `http`, `chromedp`, or `bidi`.                                              |
| `--backend-option KEY=VALUE` | Set a backend option such as `exec_path`. Repeatable.                              |
| `--proxy URL`                | Route crawler and robots.txt requests through an HTTP or SOCKS5 proxy.             |
| `--header KEY=VALUE`         | Add or replace an HTTP header. Repeatable.                                         |
| `--cookie VALUE`             | Add a cookie in `Set-Cookie` format. Repeatable and requires a `Domain` attribute. |
| `--delay N`                  | Wait `N` seconds between requests.                                                 |
| `--timeout N`                | Set the request timeout in seconds.                                                |
| `--user-agent VALUE`         | Override the crawler user agent.                                                   |
| `--no-robots`                | Disable robots.txt checks. By default Hister follows robots.txt rules.             |

For all backend options and configuration examples, see the
[Crawler Configuration](configuration#crawler-backend-options) reference.

For example, route a crawl through a local SOCKS5 proxy:

```bash
hister index --recursive \
  --proxy socks5://127.0.0.1:1080 \
  https://docs.example.com
```

## Indexing and Ownership Options

These flags control how fetched documents are added to Hister:

| Flag                 | Behavior                                                                            |
| -------------------- | ----------------------------------------------------------------------------------- |
| `--label VALUE`      | Attach a label to documents indexed by this run.                                    |
| `--force`            | Fetch and reindex URLs even when they already exist.                                |
| `--allow-sensitive`  | Bypass sensitive content checks for the indexed documents.                          |
| `--global`           | Make documents available to all users. Requires an administrator in multiuser mode. |
| `--user-id ID`       | Index documents for a specific user. Requires an administrator in multiuser mode.   |
| `--format FORMAT`    | Print the indexing summary as `text`, `json`, `jsonl`, or `csv`.                    |
| `--failed-urls PATH` | Save failed URLs, one per line, replacing the file's contents.                      |

`--global` and `--user-id` cannot be used together.

Without `--force`, a persistent crawl marks already indexed pending URLs as skipped before
fetching them. Since skipped pages are not fetched, Hister cannot discover new links from them.
Use `--force` when starting a fresh recursive job over pages already present in the index.

Indexing prints indexed, skipped, and failed counts. Persistent jobs also report their job ID
and pending count. These are cumulative stored job counts, including previous runs and redirect
tracking URLs. Job progress messages go to stderr so stdout contains only the selected summary.

An indexing run exits with status `2` if it finishes with failed URLs, including failures already
stored in a resumed job. Setup, output, and background crawl errors return status `1`. A configured
crawl limit can leave pending URLs without causing an error. See
[Indexing Results And Exit Status](terminal-client#indexing-results-and-exit-status) for details.

Save a retry list directly from plain indexing, an input job, or a recursive job:

```bash
hister index --input urls.txt --failed-urls failed-urls.txt --format json
hister index --force --input failed-urls.txt --failed-urls still-failed.txt
```

The output file is created before indexing and is empty when no URLs failed. For persistent jobs,
it lists every URL currently marked failed, including failures from previous runs.

## Inspect Jobs With `hister crawl`

The `list`, `show`, `urls`, `queue`, and `errors` commands accept `--format` / `-f` with
`text` (the default), `json`, `jsonl`, or `csv`:

```bash
hister crawl list --format json
hister crawl show example-docs --format json
hister crawl urls example-docs --status failed --format jsonl
hister crawl errors example-docs --format csv > failures.csv
hister crawl urls example-docs --status pending --count --format json
```

JSON output is always an array, including a single job or count. JSONL emits one object per line.
CSV includes a header. With `--count`, structured output contains a single `count` field.

Job records contain `id`, `status`, `start_url`, `label`, `created_at`, `updated_at`, and counts named
`pending`, `in_progress`, `done`, `failed`, and `skipped`. Timestamps include the time zone. The
`show` command also includes the saved `rules` object. If stored rules cannot be decoded, that
field contains the original string and a warning is logged.

URL records contain `status`, `depth`, and `url`. Error records contain `error_code`, `url`, and
`error`. Structured error messages preserve their original tabs and newlines; text output replaces
those characters with spaces to keep each failure on one line.

### List Jobs

```bash
hister crawl list
```

Each entry includes its ID, stored status, starting URL, creation time, and counts for pending,
done, failed, and skipped URLs. The displayed job status values are `unfinished`, `completed`, and
`interrupted`. `unfinished` means the job was started but has not recorded completion. It does not
mean that a crawler process is currently executing it.

### Show One Job

```bash
hister crawl show example-docs
```

This prints job metadata, counts for every URL state, and the saved traversal rules. It is the best
command for confirming the limits and filters that will be restored on resume.

### Inspect Job URLs

```bash
hister crawl urls example-docs
```

The output contains tab separated status, depth, and URL fields for every row in queue order. The
possible URL states are `pending`, `in_progress`, `done`, `failed`, and `skipped`.

Filter the output to one status with `--status`. The accepted values are `pending`,
`failed`, `done`, and `skipped`:

```bash
hister crawl urls example-docs --status failed
```

Print only the number of matching rows with `--count` or `-c`:

```bash
hister crawl urls example-docs --status pending --count
```

Without `--status`, the count includes every status. The older `hister crawl queue` command remains
available and lists every row without filtering.

### Inspect Failures

```bash
hister crawl errors example-docs
```

This prints the stored error code, URL, and error message as three tab separated fields for each
failed row. Failed rows are not retried when the same job resumes. A simple retry workflow is to
extract the URLs into a new input job:

```bash
hister crawl errors example-docs | cut -f2 > failed-urls.txt
hister index --force --input failed-urls.txt
```

### Delete a Job

```bash
hister crawl delete example-docs
```

Deleting a job removes its metadata and URL queue. Documents already added to the search index are
not deleted.

To create a completely new crawl with the same name, delete the old job, then start it again. Add
`--force` if its previously indexed pages must be fetched again so links can be rediscovered:

```bash
hister crawl delete example-docs
hister index --recursive \
  --force \
  --job-id example-docs \
  --allowed-domain docs.example.com \
  https://docs.example.com
```

## A Complete Resumable Workflow

Start a named and bounded crawl:

```bash
hister index --recursive \
  --job-id example-docs \
  --allowed-domain docs.example.com \
  --max-depth 4 \
  --delay 1 \
  https://docs.example.com
```

Stop it when needed, inspect its durable state, and continue it later:

```bash
hister crawl show example-docs
hister index --job-id example-docs --delay 1
```

When it finishes, review the queue and any failed pages:

```bash
hister crawl queue example-docs --count
hister crawl errors example-docs
```
