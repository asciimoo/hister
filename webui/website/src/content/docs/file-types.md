---
date: '2026-07-09T00:00:00+00:00'
draft: false
title: 'File Types'
description: 'Review supported local files, directory filters, document formats, and archive import behavior.'
---

Hister can index local files from configured directories and from explicit imports. Directory indexing is controlled by the `indexer.directories` configuration. After you configure a directory, restart the Hister server. It automatically scans the directory at startup and watches it for later changes. You do not need to run `hister import file` for this automatic tracking.

## Local File Indexing

| File type  | Extensions                     | Indexed content                               | Title source                                                    |
| ---------- | ------------------------------ | --------------------------------------------- | --------------------------------------------------------------- |
| PDF        | `.pdf`                         | Extracted plain text from all readable pages. | File path fallback                                              |
| DOCX       | `.docx`                        | Paragraph text.                               | DOCX metadata title when present, otherwise file path fallback  |
| Markdown   | `.md`, `.markdown`             | Rendered Markdown text.                       | First H1 heading when present, otherwise file path fallback     |
| Org mode   | `.org`                         | Rendered Org text.                            | Org `TITLE` value when present, otherwise file path fallback    |
| HTML       | `.html`, `.htm`                | Extracted page text.                          | Extracted page title when present, otherwise file path fallback |
| Plain text | Any file with valid UTF 8 text | Full file contents.                           | File path fallback                                              |

Files that do not match a specialized handler are treated as plain text. Binary files are skipped.

Org files that read other files through `#+INCLUDE` or `#+SETUPFILE` are rejected. Include the needed content directly in the Org file before indexing it.

For directories watched by the server, symbolic links are indexed only when their targets remain inside the configured directory. The configured directory itself may be a symbolic link. Run `hister reindex` after upgrading to remove previously indexed files reached through links outside those directories.

Click a local file result title to open its full screen preview within Hister.

Plain text previews preserve line breaks, blank lines, and indentation. Long lines wrap to fit the preview panel.

Source code and data files with common extensions are highlighted automatically. Use **Code language** to choose a language for unknown file types, correct the detected language, or select **Plain text**. Language choices are remembered for each document in your browser. Select **Automatic** to restore filename detection.

The **Highlight syntax**, **Wrap lines**, and **Line numbers** controls apply to text previews and are saved in your browser. Colors follow the app's light or dark theme. Large files and very long lines remain readable as plain text without highlighting or line numbers.

Watched HTML files use the same extractor chain as browser submissions, including Readability and structured metadata extraction, before language detection. Their original HTML is kept for previews, and their file identity is preserved even when the page contains a canonical URL. Run `hister reindex` after upgrading to extract text and correct language detection for previously indexed local HTML files. Existing remote HTML snapshots must be imported again from the source files.

The same handlers process snapshots created with `hister import file`. These imports are stored as remote file documents. The client sends prepared text, metadata, and any HTML preview content to the server.

## External PDF Parser

Hister uses its built in PDF reader by default. To replace it with a local command, install Poppler so `pdftotext` is available, then add this to your configuration:

```yaml
indexer:
  pdf_parser:
    command: pdftotext
    args: ['-enc', 'UTF-8', '-', '-']
    timeout: 30
```

In this example, `-enc UTF-8` selects the text encoding. The first `-` reads the PDF from standard input, and the second writes the extracted text to standard output.

The command must read PDF bytes from standard input and write plain UTF 8 text to standard output. It runs directly, with each `args` entry passed literally. Use an executable wrapper script for parsers that need a filename or produce a different output format.

Install the command where parsing runs: on the server for browser uploads and watched directories, and on the client for `hister import file`, including `--watch`. For a container deployment, the command must be installed inside that container. Run `hister reload` or restart the server after changing its configuration. Restart a running `hister import file --watch` command to apply client configuration changes.

`timeout` is a positive number of seconds and defaults to 30. Failed commands, empty text, invalid UTF 8, and output larger than 32 MiB reject the PDF. Hister does not retry with its built in reader. Standard error is discarded because it can contain document text.

Leave `command` empty and `args` empty to restore the built in reader. Previously stored PDFs need to be imported or submitted again; use `hister reindex` for watched local PDFs.

## Directory Filters

The `filetypes` setting on a watched directory is an extension filter. Use names without the leading dot.

```yaml
indexer:
  directories:
    - path: '~/Documents'
      label: 'documents'
      filetypes: ['pdf', 'docx', 'md', 'txt']
```

If `filetypes` is omitted, Hister considers every file that passes the other directory rules. Specialized handlers run first, then valid UTF 8 text files are indexed as plain text.

Other directory rules still apply:

| Rule                         | Behavior                                                                                  |
| ---------------------------- | ----------------------------------------------------------------------------------------- |
| `label`                      | Applies the same searchable label to every file indexed from the directory.               |
| `include_hidden`             | Hidden files and directories are skipped unless this is enabled.                          |
| `excludes`                   | Matching paths are skipped.                                                               |
| `patterns`                   | When set, only matching files are considered.                                             |
| `indexer.max_file_size_mb`   | Files above the configured size limit are skipped.                                        |
| `sensitive_content_patterns` | Matching files are rejected unless the indexing path explicitly allows sensitive content. |

The file size limit also applies to saved HTML pages with source URLs. Files that grow beyond the limit while being read are rejected.

For watched files and imported snapshots, each extracted text, title, and HTML preview is limited to 32 MiB. Plain text, HTML, Markdown, and Org source files also have a 32 MiB limit. DOCX files may contain at most 64 MiB of uncompressed content in total, including images. These extraction limits still apply when you increase `indexer.max_file_size_mb`.

## Import Formats

The `hister import file` command accepts these file formats:

| File type           | Extensions                 | Behavior                                                     |
| ------------------- | -------------------------- | ------------------------------------------------------------ |
| Hister JSON export  | `.json`                    | Imports documents previously written by `hister export`.     |
| 7z archive          | `.7z`                      | Imports a compressed Hister JSON export.                     |
| Saved HTML page     | `.html`, `.htm`            | Extracts the original page URL when present.                 |
| Local file snapshot | Any supported local format | Extracts content locally and submits a remote file document. |

When importing a directory, Hister reads matching files recursively. With no input path, it uses every configured watched directory and applies its filters. This creates remote file snapshots and is intended for directories that the command line client can access but the server cannot. Add `--watch` to keep importing new and changed snapshots while the command runs. Watch mode skips Hister exports, 7z archives, and HTML with source URL metadata. Source removals retain the indexed snapshots. See [Importing Documents](import) for watch mode details.

Use `hister import file` to create snapshots of the PDF, DOCX, Markdown, Org mode, HTML without a source URL, and plain text formats listed under local file indexing. Extraction occurs on the client, so this also works when the server cannot access the filesystem.
