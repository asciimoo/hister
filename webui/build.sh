#!/usr/bin/env bash
# shellcheck shell=bash
set -e

cd "$(dirname -- "$0")/.." || exit

npm install --workspaces --include=optional
npm run build -w @nogon/app

rm -rf server/static/app
mkdir -p server/static
cp -r webui/app/build server/static/app
