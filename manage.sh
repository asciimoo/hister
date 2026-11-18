#!/bin/sh

BASE_DIR="$(dirname -- "`readlink -f -- "$0"`")"
ACTION="$1"
[ -z "$ACTION" ] || shift

EXT_CHROME_ZIP='nogon_ext_chrome.zip'
EXT_FF_ZIP='nogon_ext_ff.zip'
EXT_SRC_ZIP='nogon_ext_src.zip'

cd -- "$BASE_DIR"
set -e


help() {
	[ -z "$1" ] || printf 'Error: %s\n' "$1"
	echo "Nogon manage.sh help

Commands
========
help                 - Display help

Dependencies
------------------
install_js_deps      - Install or install frontend dependencies (required only for development)

Tests
-----
run_unit_tests       - Run unit tests
run_extractor_tests [filter]
                     Run extractor tests against public websites

 Build
 -----
 build                - Build main Nogon application
 build_addon          - Build addon
 build_addon_artifact - Build addon artifacts to distribute to addon stores
 build_website        - Build website

 ========

 Execute 'go run nogon.go' or 'go build && ./nogon' for application related actions
 "
	[ -z "$1" ] && exit 0 || exit 1
}

check_npm() {
    if ! command -v npm >/dev/null 2>&1; then
        echo "!!!!!Error: NPM IS NOT INSTALLED!!!!! Please install npm from https://nodejs.org/en/download"
        exit 1
    fi
}

install_js_deps() {
    check_npm
    npm install --workspaces
}

run_unit_tests() {
    go test ./...
}

run_extractor_tests() {
    NOGON_LIVE_CASE="${NOGON_LIVE_CASE-${HISTER_LIVE_CASE:-$1}}" \
    NOGON_LIVE_ARTIFACT_DIR="${NOGON_LIVE_ARTIFACT_DIR-${HISTER_LIVE_ARTIFACT_DIR:-/tmp/nogon-live-extractors}}" \
    go test -count=1 -tags=live -run '^TestLiveExtractors$' -v ./server/extractor
}

build() {
    check_npm
    go generate && go build
}

build_addon() {
    check_npm
    echo "[!] Warning: The default manifest.json is for chrome browsers, overwrite it with manifest_ff.json for firefox"
    npm run build -w @nogon/ext
}

build_website() {
    check_npm
    npm run build -w @nogon/website
}

build_addon_artifact() {
    build_addon
    [ -e "webui/ext/$EXT_SRC_ZIP" ] && rm "webui/ext/$EXT_SRC_ZIP" || :
    [ -e "webui/ext/$EXT_CHROME_ZIP" ] && rm "webui/ext/$EXT_CHROME_ZIP" || :
    [ -e "webui/ext/$EXT_FF_ZIP" ] && rm "webui/ext/$EXT_FF_ZIP" || :
    zip -r "webui/ext/$EXT_SRC_ZIP" LICENSE package.json package-lock.json \
        webui/ext/src webui/ext/assets webui/ext/package.json \
        webui/ext/tsconfig.json webui/ext/svelte.config.js webui/ext/vite.config.ts \
        webui/components/src webui/components/package.json webui/components/tsconfig.json
    cd webui/ext/dist
    zip "../$EXT_CHROME_ZIP" ./* assets/* assets/icons/*
    cd ../
    cp "$EXT_CHROME_ZIP" "$EXT_FF_ZIP"
    zip -d "$EXT_CHROME_ZIP" manifest_ff.json
    zip -d "$EXT_FF_ZIP" manifest.json
    printf "@ manifest_ff.json\n@=manifest.json\n" | zipnote -w "$EXT_FF_ZIP"
}

[ "$(command -V "$ACTION" | grep ' function$')" = "" ] \
	&& help "action not found" \
	|| "$ACTION" "$@"
