---
date: '2026-03-06T19:45:22-05:00'
draft: false
title: 'Troubleshooting'
description: 'Diagnose server startup, interface, extension, import, memory, and browser debugging problems.'
---

Start with the diagnostic commands below to identify configuration, connection, and indexing problems.

If you still need help, use the Community links in this page's footer.

## Run Diagnostics First

```bash
hister doctor
```

Doctor checks configuration, required extractor programs, server connectivity, authentication, and index compatibility. It reports problems without repairing data. The server must be running for the server checks to complete.

Read the message beside each result:

| Status    | What to do                                                                             |
| --------- | -------------------------------------------------------------------------------------- |
| `ok`      | This check passed. Continue to any warnings or errors.                                 |
| `warning` | Review the message for checks that could not be completed.                             |
| `error`   | Follow the suggested action, then run `hister doctor` again to check whether it helps. |

Use the same `--config`, `--server-url`, and authentication settings as the command you are troubleshooting. In multi user mode, checking the server's index and extractor programs requires an admin token. Older servers may report that diagnostics are unavailable; updating the server enables those checks.

### Locate and Validate Configuration

If the server will not start, or you suspect Hister is using different settings than expected, run:

```bash
hister config path
hister config validate
```

`config path` prints the selected configuration file, even if its YAML is invalid. If it prints `(defaults)`, no configuration file was found and Hister is using its defaults. Environment variables and command line options can still override those defaults.

`config validate` checks configuration without needing a running server. A successful check prints `Configuration valid:` followed by the selected path or `(defaults)`. If it reports an error, correct the named setting and run the command again. A valid configuration does not confirm that the server is reachable or required programs are installed; use `doctor` for those checks.

These commands inspect configuration on the computer where you run them. To check a remote server's configuration file, run them on that server. For a custom file, use the same path you pass when starting Hister:

```bash
hister --config /path/to/config.yml config path
hister --config /path/to/config.yml config validate
```

See [Configuration and Diagnostics](/docs/terminal-client#configuration-and-diagnostics) for more options and output formats.

## Common Issues

### Server won't start

- Check if port 4433 (or whatever was configured instead) is already in use
- Run `hister config validate` and correct any reported settings

### Web interface loads, but looks broken

If the main text loads, but seems jumbled up, and (most) images don't load, check that the `base_url` is correct in the server's config.
(Trailing slashes should be irrelevant, but you can try fiddling with them in the config and/or the browser's address bar; please file a bug report if this fixes the issue.)

### Extension not connecting

- Ensure your Hister server is running and up to date
- Verify the extension is configured with the correct server URL (should be the same as `base_url` in the server's config)
- Check browser console for errors (also, see below for debugging the extension itself)
- Check firewall settings

### Browser import fails

- Ensure your Hister server is running and up to date

### Reindex reports an embedding dimension mismatch

`semantic_search.dimensions` must match an output size supported by your embedding model. For example, `expected 2000, got 768` means the endpoint returned 768 values while Hister expected 2000. Set the server's dimensions to 768, restart Hister, and run `hister reindex` again. Changing this value alone cannot make a model produce a different size.

Reindexing checks the embedding endpoint before clearing existing vectors. If embedding fails later during the rebuild, correct the reported problem and rerun reindexing. `hister config validate` checks configuration values without contacting the embedding endpoint.

## Memory Management

If Hister is consuming too much memory, especially with large browsing histories or many indexed documents, you can reduce memory usage by disabling language detection.

### Reducing Memory Usage

Set `detect_languages: false` in the `indexer` section of your configuration file:

```yaml
indexer:
  detect_languages: false
```

Restart Hister after changing this setting to release language detection models already loaded in memory. Newly processed documents use the default analyzer, which can reduce search accuracy because it does not apply language specific stemming. Existing documents remain searchable with their stored language labels and analyzers.

To also consolidate existing documents into the default index and release the resources used by separate language indexes, run a full reindex:

```bash
hister reindex
```

The reindex operation rebuilds all indexes according to the new setting, while preserving manually pinned languages and their analyzers. Clear those pins with `hister update QUERY --language auto` if you want those documents to use the default analyzer too. Reindexing is optional if you only want to disable detection for newly processed documents. See [Language Settings and Detection](/docs/language-detection) for other ways to balance accuracy and memory use.

## Debugging the Web Extension

The Web extension's logs will not be visible in the default browser console.
Instead:

### Firefox

1. Go to `about:debugging#/runtime/this-firefox`
2. Press the "Inspect" button to the right of "Hister".
