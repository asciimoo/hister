---
date: '2026-03-06T19:45:22-05:00'
draft: false
title: 'Troubleshooting'
description: 'Diagnose server startup, interface, extension, import, memory, and browser debugging problems.'
---

We are sorry that you are here. 🙁 Fingers crossed it won't be for long?

If all else fails, you can try asking for help&mdash;see the Community links in this page's footer.

## Common Issues

### Server won't start

- Check if port 4433 (or whatever was configured instead) is already in use
- Verify the configuration file syntax
- If you installed a background service, run `hister service status` and check the logs:
  - macOS: `~/Library/Logs/hister.log` and `~/Library/Logs/hister-error.log`
  - Linux: `journalctl --user -u hister`
- If your shell says `hister: command not found`, run `./hister service status` from the directory containing the binary, or move the binary to a stable directory on `PATH`. The service can still be running because launchd/systemd uses the recorded absolute binary path.
- `hister service install` refuses Homebrew Cellar and `/nix/store` binaries, Nix-managed units, a missing or unreadable `--config` file, and unpersisted `HISTER__*` or `HISTER_PORT` environment variables. Move the binary to a stable path (for example `$(brew --prefix)/bin/hister`) or use the Nix module instead.
- systemd user services stop at logout unless lingering is enabled (`loginctl enable-linger`). Hister does not enable lingering for you.
- `hister service` is not supported on Windows yet; run `hister.exe listen` in a terminal.

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

## Memory Management

If Hister is consuming too much memory, especially with large browsing histories or many indexed documents, you can reduce memory usage by disabling language detection.

### Reducing Memory Usage

Set `detect_languages: false` in the `indexer` section of your configuration file:

```yaml
indexer:
  detect_languages: false
```

This setting disables automatic language detection for indexed pages, which reduces memory consumption by using a single default analyzer instead of maintaining separate language-specific indexes. This comes at the cost of potentially less accurate search results.

**Important**: After changing this setting, you must run a full reindex:

```bash
hister reindex
```

The reindex operation will rebuild all indexes according to the new setting, compacting your existing data to a single index.

## Debugging the Web Extension

The Web extension's logs will not be visible in the default browser console.
Instead:

### Firefox

1. Go to `about:debugging#/runtime/this-firefox`
2. Press the "Inspect" button to the right of "Hister".
