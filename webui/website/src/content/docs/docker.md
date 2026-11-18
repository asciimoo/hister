---
date: '2026-03-06T22:13:54-05:00'
draft: false
title: 'Docker'
description: 'Deploy Nogon with Docker Compose, persistent storage, external access, and a reverse proxy.'
---

## Docker Setup

Nogon provides official Docker images for both AMD64 and ARM64 architectures.

Use `ghcr.io/asciimoo/nogon:latest` for stable releases. The image runs as a nonroot
user with UID and GID `65532`. If you need to run as root, use
`ghcr.io/asciimoo/nogon:latest-root`.

Until the first Nogon stable image is published, use `ghcr.io/asciimoo/nogon:master`
for a development build, or `master-root` for its root variant. Existing stable
installations can continue using `ghcr.io/asciimoo/hister:latest`.

The examples below are for new installations. If you already run Nogon or Hister
in Docker, follow [Upgrading an Existing Container](#upgrading-an-existing-container)
to keep your data attached.

### Configuring Nogon in a Container

Nogon can be fully [configured using environment variables](configuration#environment-variables).
This is the **recommended approach for containerized environments** (Docker, Kubernetes, etc.) as it avoids the need to manage configuration files inside the container or mounted volumes.

The Compose examples below use `/nogon/data` for both `working_dir` and
`NOGON_DATA_DIR`, with your host's `./data` directory mounted there. A config file
at `./data/config.yml` on the host is loaded automatically. Without it, Nogon uses
defaults and environment variables. Relative paths resolve from `/nogon/data`
inside the container.
To require a specific config file, set `NOGON_CONFIG` or pass `--config`; a missing
file then prevents startup.

If you prefer using a configuration file instead of environment variables, generate a commented reference using Docker:

```bash
docker run --rm ghcr.io/asciimoo/nogon:latest config create > config.yml
```

Uncomment each setting you want to activate and its enclosing section, preserving indentation.
Leave unrelated settings commented. The listen address example follows the container build's
default. To print all defaults as active YAML values, use `config create --full` instead.
Shell redirection replaces an existing destination file, so choose a new filename to keep an
existing configuration.

### Basic Docker Compose

Create a data directory beside your `compose.yml` and give the container user
ownership before starting Nogon:

```bash
mkdir -p ./data
sudo chown 65532:65532 ./data
```

Save this as `compose.yml`. Your data stays in `./data` between container updates:

```yaml
services:
  nogon:
    image: ghcr.io/asciimoo/nogon:latest
    container_name: nogon
    restart: unless-stopped
    working_dir: /nogon/data
    environment:
      NOGON_DATA_DIR: /nogon/data
    volumes:
      - ./data:/nogon/data
    ports:
      - 4433:4433
```

Start Nogon with `docker compose up -d`, then open <http://localhost:4433>.

### Upgrading an Existing Container

Update the image in your existing Compose file. Keep your service and container
names, volume or bind mount, and container data path. Older setups usually use
`hister`, `hister_data`, and `/hister/data`. Replacing your existing setup with the
fresh installation example above can point Nogon at an empty directory instead
of opening your existing data. The image retains user ID `65532`.

If you move or rename the checkout directory, keep the original Compose project
name with `docker compose -p ORIGINAL_PROJECT` so Compose selects the same volume.
Do not use `down --volumes` during an upgrade.

After updating your image selection, run:

```bash
docker compose pull
docker compose up -d
```

Nogon images also provide the `hister` command and `/hister/hister` entrypoint
alias. Existing `HISTER_` configuration variables remain accepted; see
[environment variable precedence](configuration#environment-variables). Keep those
names while running an older image that does not support Nogon names.

The repository's `compose.yml` preserves the older service and volume names. Set
`NOGON_IMAGE=ghcr.io/asciimoo/nogon:latest` in its `.env` file to select the Nogon
stable image, or use `:master` for development builds. `NOGON_CACHE_IMAGE` selects
a build cache. The existing `HISTER_IMAGE` and `HISTER_CACHE_IMAGE` aliases still
work; a nonempty Nogon value takes precedence.

### Docker Compose with External Access

To make Nogon accessible from other devices, extend the `environment` section
under the `nogon` service in your Compose file:

```yaml
environment:
  NOGON_DATA_DIR: /nogon/data
  NOGON__SERVER__ADDRESS: 0.0.0.0:4433
  NOGON__SERVER__BASE_URL: http://192.168.1.100:4433 # Use your actual IP/hostname
```

### Docker Compose Behind Reverse Proxy

When running behind a reverse proxy, use your public URL instead:

```yaml
environment:
  NOGON_DATA_DIR: /nogon/data
  NOGON__SERVER__ADDRESS: 0.0.0.0:4433
  NOGON__SERVER__BASE_URL: https://nogon.example.com # Your public URL
```

See [Server Setup](server-setup) for reverse proxy examples and access control.

### Using a Host Directory

The example above uses `./data` beside your Compose file. To use another host
directory, create it and give UID and GID `65532` ownership, then change the left
side of the mount. Keep `/nogon/data` as the container path so it matches
`working_dir` and `NOGON_DATA_DIR`:

```yaml
volumes:
  - /path/to/data:/nogon/data
```

On a host with SELinux enabled, add the `Z` option so Docker assigns an appropriate label:

```yaml
volumes:
  - ./data:/nogon/data:Z
```

## Browser Backends with Docker

Browser fetching happens in the `nogon index` process. The Nogon server can stay in Docker or
LXC while you run that command and the browser on your workstation. The official Nogon image
does not include Firefox or Chromium.

For the local Compose setup above, install the Nogon binary on the host and start Firefox using
the [Firefox with BiDi instructions](crawler#firefox-with-bidi). Then index a page from the host:

```bash
nogon --server-url http://127.0.0.1:4433 index \
  --backend bidi \
  --backend-option socket=ws://127.0.0.1:9222/session \
  https://example.com
```

Or let Nogon launch a locally installed Chromium:

```bash
nogon --server-url http://127.0.0.1:4433 index \
  --backend chromedp \
  --backend-option exec_path=/usr/bin/chromium \
  https://example.com
```

Replace `--server-url` with your reachable Nogon URL if the server is on another machine, and
add `--token TOKEN` if it requires token authentication. The browser endpoint and the Nogon
server address are separate: port `9222` controls Firefox, while port `4433` receives documents.

If you run `nogon index` inside a container, apply its crawler configuration there. For
`chromedp`, build an image with Chrome or Chromium and its runtime dependencies installed. For
`bidi`, start Firefox separately and make its BiDi endpoint reachable from the indexing process.

With normal container network isolation, `127.0.0.1` refers to the container itself. A browser
listening on the host's loopback interface is not reachable through that address inside a
container. Firefox's Remote Agent listens on loopback, so putting two containers on the same
Docker network alone does not expose it. Run Firefox and the indexing command in the same
network namespace, or arrange a protected tunnel to the browser's loopback endpoint. See
[Docker networking](https://docs.docker.com/engine/network/#container-networks) for sharing a
container's network namespace. Keep the browser debugging endpoint private; it provides control
of the browser without Nogon's authentication.

For protocol errors or connection failures, see
[browser connection troubleshooting](crawler#browser-connection-troubleshooting).
