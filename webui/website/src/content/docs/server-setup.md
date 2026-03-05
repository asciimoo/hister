---
date: '2026-02-21T16:18:00+01:00'
draft: false
title: 'Server Setup and Configuration'
---

This guide explains how to run and configure the Hister server in different environments.

## Basic Localhost Setup

The simplest setup requires no configuration at all. Simply start the server:

```bash
./hister listen
```

The server will start on `http://127.0.0.1:4433` and be accessible only from your local machine. This is perfect for personal use on a single computer.

## Running on a Different Host

If you want to access Hister from other devices on your network or run it on a server, you need to configure two settings.

### Generate Configuration File

You can generate a default configuration file using the `create-config` command:

```bash
./hister create-config config.yml
```

If no filename is provided, it will print the default configuration to `stdout`.

> **Note**: You can also configure Hister entirely using **Environment Variables**, which is often easier for server setups. See the [Configuration via Environment Variables](#configuration-via-environment-variables) section below for details.

### Bind to All Network Interfaces

Edit your configuration file (e.g., `~/.config/hister/config.yml`):

```yaml
server:
  address: '0.0.0.0:4433' # or the target interface's address
  base_url: 'http://192.168.1.100:4433' # Use your actual server IP or hostname
```

Replace `192.168.1.100` with your server's actual IP address or domain name.

The `base_url` must match exactly how you access Hister in your browser. If you access it via `https://hister.example.com`, set that as your `base_url`.

**Important**: After changing the configuration, restart the Hister server.

## Running Behind a Reverse Proxy

When running Hister behind a reverse proxy (like Nginx, Caddy, or Traefik), you need to ensure WebSocket connections work properly for the `/search` endpoint.

### Required Configuration

The reverse proxy must pass the `Upgrade` HTTP header to enable WebSocket connections:

#### Nginx Example

```nginx
location / {
        location /search {
                proxy_pass http://127.0.0.1:4433/search;
                proxy_http_version 1.1;
                proxy_set_header Upgrade $http_upgrade;
                proxy_set_header Connection "upgrade";
                proxy_read_timeout 86400;

        }
        # add_header Access-Control-Allow-Origin *;
        proxy_set_header        Host    $http_host;
        proxy_set_header        X-Real-IP $remote_addr;
        proxy_set_header        X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header        X-Forwarded-Proto $scheme;
        proxy_set_header        X-Scheme $scheme;
        proxy_pass http://127.0.0.1:4433/;

        client_max_body_size 100m; # A *lot* of data is sometimes sent when indexing pages.
}
```

#### Caddy Example

```caddy
hister.example.com {
    # only bind to your VPN or local network interface:
    bind fd00:1122:3344:5566::1
    reverse_proxy localhost:4433
}
```

Caddy handles WebSocket upgrades automatically.

#### Traefik Example

Traefik also handles WebSocket upgrades automatically with no special configuration needed.

### Hister Configuration

Update your `~/.config/hister/config.yml` to use the public URL:

```yaml
server:
  address: '127.0.0.1:4433' # Keep localhost since proxy handles external access
  base_url: 'https://hister.example.com' # Your public URL
```

## Configuration via Environment Variables

Hister can be fully configured using environment variables. This is the **recommended approach for containerized environments** (Docker, Kubernetes, etc.) as it avoids the need to manage configuration files inside the container or mounted volumes.

### Environment Variable Format

All configuration options can be set using environment variables with the prefix `HISTER__`. Nested keys are separated by double underscores (`__`).

| Variable                   | Description                                                           |
| -------------------------- | --------------------------------------------------------------------- |
| `HISTER__SERVER__ADDRESS`  | The address and port the server binds to (default: `127.0.0.1:4433`)  |
| `HISTER__SERVER__BASE_URL` | The external URL used to access Hister (e.g., `https://hister.com`)   |
| `HISTER__SERVER__DATABASE` | The filename of the SQLite database (default: `db.sqlite3`)           |
| `HISTER__APP__DIRECTORY`   | The directory where Hister stores its data (shorthand: `DATA_DIR`)    |
| `HISTER__APP__LOG_LEVEL`   | Logging verbosity: `debug`, `info`, `warn`, `error` (default: `info`) |
| `HISTER_PORT`              | Shorthand to override only the port in `server.address`               |

## Docker Setup

Hister provides official Docker images for both AMD64 and ARM64 architectures. Using environment variables is the preferred way to configure Hister in Docker.

> **Note on Permissions**: The `latest` image runs as a **non-root user** (UID/GID 1000) by default for better security. Ensure the mounted volume (e.g., `./data`) has the correct permissions. If you need to run as root, use the `ghcr.io/asciimoo/hister:latest-root` image.

### Generating Configuration via Docker

If you prefer using a configuration file instead of environment variables, you can generate a default one using Docker:

```bash
docker run --rm ghcr.io/asciimoo/hister:latest create-config > config.yml
```

### Basic Docker Compose

For a simple local setup:

```yaml
services:
  hister:
    image: ghcr.io/asciimoo/hister:latest
    container_name: hister
    user: '1000:1000'
    restart: unless-stopped
    volumes:
      - ./data:/hister/data
    ports:
      - 4433:4433
```

### Docker Compose with External Access

To make Hister accessible from other devices, use the `environment` section in your `compose.yml`:

```yaml
services:
  hister:
    image: ghcr.io/asciimoo/hister:latest
    container_name: hister
    user: '1000:1000'
    restart: unless-stopped
    environment:
      - HISTER__SERVER__ADDRESS=0.0.0.0:4433
      - HISTER__SERVER__BASE_URL=http://192.168.1.100:4433 # Use your actual IP/hostname
    volumes:
      - ./data:/hister/data
    ports:
      - 4433:4433
```

### Docker Compose Behind Reverse Proxy

When running behind a reverse proxy, set the `base_url` to your public domain:

```yaml
services:
  hister:
    image: ghcr.io/asciimoo/hister:latest
    container_name: hister
    user: '1000:1000'
    restart: unless-stopped
    environment:
      - HISTER__SERVER__ADDRESS=0.0.0.0:4433
      - HISTER__SERVER__BASE_URL=https://hister.example.com # Your public URL
    volumes:
      - ./data:/hister/data
    ports:
      - 4433:4433
```
