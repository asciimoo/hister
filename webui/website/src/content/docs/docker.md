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
