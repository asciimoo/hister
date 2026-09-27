---
date: '2026-03-06T22:13:54-05:00'
draft: false
title: 'Docker'
description: 'Deploy Hister with Docker Compose, persistent storage, external access, and a reverse proxy.'
---

## Docker Setup

Hister provides official Docker images for both AMD64 and ARM64 architectures.

The `latest` image runs as the nonroot user with UID and GID `65532`. The examples below use a Docker managed volume so Docker can initialize it with the permissions from the image. If you need to run as root, use the `ghcr.io/asciimoo/hister:latest-root` image.

### Configuring Hister in a Container

Hister can be fully [configured using environment variables](configuration#environment-variables).
This is the **recommended approach for containerized environments** (Docker, Kubernetes, etc.) as it avoids the need to manage configuration files inside the container or mounted volumes.

If you prefer using a configuration file instead of environment variables, you can generate a default one using Docker:

```bash
docker run --rm ghcr.io/asciimoo/hister:latest config create > config.yml
```

### Basic Docker Compose

For a simple local setup:

```yaml
services:
  hister:
    image: ghcr.io/asciimoo/hister:latest
    container_name: hister
    restart: unless-stopped
    volumes:
      - hister_data:/hister/data
    ports:
      - 4433:4433

volumes:
  hister_data:
```

### Docker Compose with External Access

To make Hister accessible from other devices, use the `environment` section in your `compose.yml`:

```yaml
services:
  hister:
    image: ghcr.io/asciimoo/hister:latest
    container_name: hister
    restart: unless-stopped
    environment:
      - HISTER__SERVER__ADDRESS=0.0.0.0:4433
      - HISTER__SERVER__BASE_URL=http://192.168.1.100:4433 # Use your actual IP/hostname
    volumes:
      - hister_data:/hister/data
    ports:
      - 4433:4433

volumes:
  hister_data:
```

### Docker Compose Behind Reverse Proxy

When running behind a reverse proxy, set the `base_url` to your public domain:

```yaml
services:
  hister:
    image: ghcr.io/asciimoo/hister:latest
    container_name: hister
    restart: unless-stopped
    environment:
      - HISTER__SERVER__ADDRESS=0.0.0.0:4433
      - HISTER__SERVER__BASE_URL=https://hister.example.com # Your public URL
    volumes:
      - hister_data:/hister/data
    ports:
      - 4433:4433

volumes:
  hister_data:
```

### Using a Host Directory

If you want to store the data in a host directory instead of a Docker managed volume, create the directory and give the container user ownership before starting Hister:

```bash
mkdir -p ./data
sudo chown 65532:65532 ./data
```

Then replace the volume entry with:

```yaml
volumes:
  - ./data:/hister/data
```

On a host with SELinux enabled, add the `Z` option so Docker assigns an appropriate label:

```yaml
volumes:
  - ./data:/hister/data:Z
```

## Browser Backends with Docker

Browser fetching happens in the `hister index` process. The Hister server can stay in Docker or
LXC while you run that command and the browser on your workstation. The official Hister image
does not include Firefox or Chromium.

For the local Compose setup above, install the Hister binary on the host and start Firefox using
the [Firefox with BiDi instructions](crawler#firefox-with-bidi). Then index a page from the host:

```bash
hister --server-url http://127.0.0.1:4433 index \
  --backend bidi \
  --backend-option socket=ws://127.0.0.1:9222/session \
  https://example.com
```

Or let Hister launch a locally installed Chromium:

```bash
hister --server-url http://127.0.0.1:4433 index \
  --backend chromedp \
  --backend-option exec_path=/usr/bin/chromium \
  https://example.com
```

Replace `--server-url` with your reachable Hister URL if the server is on another machine, and
add `--token TOKEN` if it requires token authentication. The browser endpoint and the Hister
server address are separate: port `9222` controls Firefox, while port `4433` receives documents.

If you run `hister index` inside a container, apply its crawler configuration there. For
`chromedp`, build an image with Chrome or Chromium and its runtime dependencies installed. For
`bidi`, start Firefox separately and make its BiDi endpoint reachable from the indexing process.

With normal container network isolation, `127.0.0.1` refers to the container itself. A browser
listening on the host's loopback interface is not reachable through that address inside a
container. Firefox's Remote Agent listens on loopback, so putting two containers on the same
Docker network alone does not expose it. Run Firefox and the indexing command in the same
network namespace, or arrange a protected tunnel to the browser's loopback endpoint. See
[Docker networking](https://docs.docker.com/engine/network/#container-networks) for sharing a
container's network namespace. Keep the browser debugging endpoint private; it provides control
of the browser without Hister's authentication.

For protocol errors or connection failures, see
[browser connection troubleshooting](crawler#browser-connection-troubleshooting).
