# syntax=docker/dockerfile:1

# Build the frontend with only the workspaces required by the embedded app.
FROM node:26-alpine3.24@sha256:ef24c5053d50fdc3e4e56eb4e7ddb7861874ab0fdc797046ba897581deb8e868 AS frontend

WORKDIR /app

COPY package.json package-lock.json ./
COPY webui/app/package.json webui/app/
COPY webui/components/package.json webui/components/

RUN --mount=type=cache,id=hister-npm,target=/root/.npm,sharing=locked \
    npm ci \
      --workspace=@hister/app \
      --workspace=@hister/components

COPY webui/app/ webui/app/
COPY webui/components/ webui/components/

RUN npm run build --workspace=@hister/app

# Build a static Go binary. Cache downloads and compiled packages separately so
# source changes do not force the toolchain to start cold.
FROM golang:1.27-alpine3.24@sha256:cf6fca6641884b8433441b2b0652976f975e1d0fdd26d177eaaf8596087f3125 AS builder

RUN apk add --no-cache gcc musl-dev

WORKDIR /app

COPY go.mod go.sum ./
RUN --mount=type=cache,id=hister-go-mod,target=/go/pkg/mod,sharing=locked \
    go mod download

COPY . .
COPY --link --from=frontend /app/webui/app/build/ server/static/app/

RUN --mount=type=cache,id=hister-go-mod,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,id=hister-go-build,target=/root/.cache/go-build,sharing=locked \
    set -eux; \
    LISTEN_ADDRESS="0.0.0.0:4433"; \
    BASE_URL="http://localhost:4433"; \
    CGO_ENABLED=1 go build \
    -trimpath \
    -tags netgo,osusergo \
    -ldflags "\
      -linkmode external -extldflags '-static' -s -w \
      -X 'github.com/asciimoo/hister/config.DefaultServerAddress=$LISTEN_ADDRESS' \
      -X 'github.com/asciimoo/hister/config.DefaultServerBaseURL=$BASE_URL'" \
    -o /out/hister .

# Fetch a versioned, architecture-specific yt-dlp binary and verify it against
# the checksums published with that release.
FROM alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b AS ytdlp

ARG TARGETARCH=amd64
ARG YT_DLP_VERSION=2026.07.04

RUN set -eux; \
    case "$TARGETARCH" in \
      amd64) \
        asset="yt-dlp_musllinux"; \
        checksum="f7439ec2e3ffe69e06ac233f83f0d9687b89105939129bddcbf74e5de0f2b40e" \
        ;; \
      arm64) \
        asset="yt-dlp_musllinux_aarch64"; \
        checksum="9a6a4de88f35dc68c1763945fbb417e092ebd9afc5d66052ac31b68d405a12a7" \
        ;; \
      *) echo "unsupported TARGETARCH for yt-dlp: $TARGETARCH" >&2; exit 1 ;; \
    esac; \
    mkdir -p /usr/local/bin; \
    wget -qO /usr/local/bin/yt-dlp \
      "https://github.com/yt-dlp/yt-dlp/releases/download/${YT_DLP_VERSION}/${asset}"; \
    echo "${checksum}  /usr/local/bin/yt-dlp" > /tmp/checksums; \
    sha256sum -c /tmp/checksums; \
    chmod 0755 /usr/local/bin/yt-dlp

# Put shared runtime content in one stage so release, root, and debug variants
# reuse the same immutable layers in the registry and on container hosts.
FROM alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b AS runtime

LABEL org.opencontainers.image.title="Hister" \
      org.opencontainers.image.description="Self-hosted browser history search engine" \
      org.opencontainers.image.source="https://github.com/asciimoo/hister" \
      org.opencontainers.image.licenses="AGPL-3.0"

WORKDIR /hister

RUN adduser -D -u 65532 hister \
    && mkdir -p /hister/data \
    && chown 65532:65532 /hister/data

COPY --link --from=ytdlp /usr/local/bin/yt-dlp /usr/local/bin/yt-dlp
COPY --link --from=builder /out/hister /hister/hister

ENV HISTER_DATA_DIR=/hister/data \
    HISTER_CONFIG=/hister/data/config.yml

EXPOSE 4433

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["sh", "-c", "port=\"${HISTER_PORT:-${HISTER__SERVER__ADDRESS##*:}}\"; exec wget -qO /dev/null \"http://localhost:${port:-4433}/health\""]

ENTRYPOINT ["/hister/hister"]
CMD ["listen"]

# latest and vx.x.x
FROM runtime AS release
USER 65532:65532

# latest-root and vx.x.x-root
FROM runtime AS root
USER root

# latest-debug and vx.x.x-debug
FROM runtime AS debug
RUN apk add --no-cache bash curl
USER root
