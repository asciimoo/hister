#!/usr/bin/env bash
# shellcheck shell=bash

set -e
cd "$(dirname -- "$0")/.." || exit

AIR="$(go env GOPATH)/bin/air"
if ! [ -x "$AIR" ]; then
    echo "air not found — installing..."
    go install github.com/air-verse/air@latest
fi

# Initial SPA build so the Go server has something to embed on first start.
# Uses development mode (unminified) for faster builds.
npm run build -w @nogon/app -- --mode development
rm -rf server/static/app
mkdir -p server/static
cp -r webui/app/build server/static/app

cleanup() {
    kill 0 2>/dev/null
    wait 2>/dev/null
}
trap cleanup EXIT INT TERM

# Go backend with air (auto-rebuild on .go changes)
"$AIR" -c webui/.air.toml -- listen "$@" &

# Wait for Go server to be ready before starting vite
echo "Waiting for Go server on 127.0.0.1:4433..."
while ! curl -s -o /dev/null http://127.0.0.1:4433/api/config 2>/dev/null; do
    sleep 0.5
done
echo "Go server ready."

# Vite dev server (frontend hot reload, proxies /api + /static to Go)
npm run dev -w @nogon/app &

wait
