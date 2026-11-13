#!/usr/bin/env bash
set -euo pipefail
cd /workspace/hister_fork
mkdir -p /workspace/toolchains /workspace/.cache/npm /workspace/.cache/go-build /workspace/.cache/go-mod /workspace/hister-dev
if ! test -x /workspace/toolchains/go/bin/go; then
  curl -fsSL --max-time 180 https://go.dev/dl/go1.26.11.linux-amd64.tar.gz -o /workspace/hister-dev/go.tar.gz
  printf '%s  %s\n' 9849f4e04ee35303714e34fbc3ac8ab44ea3e448a95aa182f91e3b9b79c710ec /workspace/hister-dev/go.tar.gz | sha256sum -c -
  tar -xzf /workspace/hister-dev/go.tar.gz -C /workspace/toolchains
fi
export PATH=/workspace/toolchains/go/bin:$PATH
export GOCACHE=/workspace/.cache/go-build GOMODCACHE=/workspace/.cache/go-mod GOMAXPROCS=4 GOTOOLCHAIN=local
# Preserve module checksum and TLS verification. Large proxy archives redirect
# to a host unavailable in this environment; direct Git retrieval works.
export GOPROXY=direct
 go version
 npm ci --cache /workspace/.cache/npm
 go mod download
 go mod verify
 npm run build -w @hister/app
 mkdir -p server/static/app
 cp -a webui/app/build/. server/static/app/
 go build -p 4 -o hister .
