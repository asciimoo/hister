#!/bin/sh

set -eu

DATASETS="go-stdlib mdn-css mdn-html mdn-js powershell python-docs rust-docs nodejs-api typescript-docs react-docs django-docs postgresql-docs kubernetes-docs owasp-cheatsheets rfcs"

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

if [ -f "$SCRIPT_DIR/../config.yml" ]; then
  cd "$SCRIPT_DIR/.."
fi

NOGON_BIN=${NOGON:-${HISTER:-}}
if [ -n "$NOGON_BIN" ]; then
  :
elif [ -x ./nogon ]; then
  NOGON_BIN=./nogon
elif [ -x ./hister ]; then
  NOGON_BIN=./hister
elif [ -x "$SCRIPT_DIR/../nogon" ]; then
  NOGON_BIN=$SCRIPT_DIR/../nogon
elif [ -x "$SCRIPT_DIR/../hister" ]; then
  NOGON_BIN=$SCRIPT_DIR/../hister
elif command -v nogon >/dev/null 2>&1; then
  NOGON_BIN=nogon
else
  NOGON_BIN=hister
fi

print_options() {
  printf 'Usage: %s DATASET\n\n' "$0"
  printf 'Datasets:\n'
  for dataset in $DATASETS; do
    printf '  %s\n' "$dataset"
  done
  printf '  all\n'
}

export_dataset() {
  dataset=$1
  label=$2

  "$NOGON_BIN" export "$dataset.json" "label:$label"
}

fetch_go_stdlib() {
  label=golang

  "$NOGON_BIN" index --recursive \
    --allowed-pattern="^https://pkg\.go\.dev/.+@go1\.26\.4$" \
    --exclude-pattern="/internal/" \
    --label="$label" \
    --delay=2 \
    --allow-sensitive \
    "https://pkg.go.dev/std?v=@go1.26.4"
  export_dataset "go-stdlib" "$label"
}

fetch_mdn_css() {
  label=css

  "$NOGON_BIN" index --recursive \
    --allowed-domain=developer.mozilla.org \
    --allowed-pattern="https://developer.mozilla.org/en-US/docs/Web/CSS/Reference/.*" \
    --exclude-pattern="contributors.txt" \
    --label="$label" \
    https://developer.mozilla.org/en-US/docs/Web/CSS/Reference/
  export_dataset "mdn-css" "$label"
}

fetch_mdn_html() {
  label=html

  "$NOGON_BIN" index --recursive \
    --allowed-domain=developer.mozilla.org \
    --allowed-pattern="https://developer.mozilla.org/en-US/docs/Web/HTML/Reference/.*" \
    --exclude-pattern="contributors.txt" \
    --label="$label" \
    https://developer.mozilla.org/en-US/docs/Web/HTML/Reference/
  export_dataset "mdn-html" "$label"
}

fetch_mdn_js() {
  label=javascript

  "$NOGON_BIN" index --recursive \
    --allowed-domain=developer.mozilla.org \
    --allowed-pattern="https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/.*" \
    --exclude-pattern="contributors.txt" \
    --label="$label" \
    https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/
  export_dataset "mdn-js" "$label"
}

fetch_powershell() {
  label=powershell

  "$NOGON_BIN" index --recursive \
    --allowed-domain=learn.microsoft.com \
    --allowed-pattern="https://learn.microsoft.com/en-us/powershell/.*(powershell-7\.6|windowsserver2025-ps)$" \
    --label="$label" \
    --backend=chromedp \
    "https://learn.microsoft.com/en-us/powershell/scripting/how-to-use-docs?view=powershell-7.6" --force
  export_dataset "powershell" "$label"
}

fetch_python_docs() {
  label=python

  "$NOGON_BIN" index --recursive \
    --allowed-domain=docs.python.org \
    --allowed-pattern="^https://docs\.python\.org/3/library/.*" \
    --label="$label" \
    --delay=1 \
    --allow-sensitive \
    "https://docs.python.org/3/library/index.html"
  export_dataset "python-docs" "$label"
}

fetch_rust_docs() {
  label=rust

  "$NOGON_BIN" index --recursive \
    --allowed-domain=doc.rust-lang.org \
    --allowed-pattern="^https://doc\.rust-lang\.org/std/.*" \
    --label="$label" \
    --delay=1 \
    --allow-sensitive \
    "https://doc.rust-lang.org/std/"
  "$NOGON_BIN" index --recursive \
    --allowed-domain=doc.rust-lang.org \
    --allowed-pattern="^https://doc\.rust-lang\.org/book/.*" \
    --label="$label" \
    --delay=1 \
    --allow-sensitive \
    "https://doc.rust-lang.org/book/"
  export_dataset "rust-docs" "$label"
}

fetch_nodejs_api() {
  label=nodejs

  "$NOGON_BIN" index --recursive \
    --allowed-domain=nodejs.org \
    --allowed-pattern="^https://nodejs\.org/api/.*" \
    --exclude-pattern=".+\.json$" \
    --label="$label" \
    --delay=1 \
    --allow-sensitive \
    "https://nodejs.org/api/"
  export_dataset "nodejs-api" "$label"
}

fetch_typescript_docs() {
  label=typescript

  "$NOGON_BIN" index --recursive \
    --allowed-domain=www.typescriptlang.org \
    --allowed-pattern="^https://www\.typescriptlang\.org/docs/.*" \
    --label="$label" \
    --delay=1 \
    --allow-sensitive \
    "https://www.typescriptlang.org/docs/"
  "$NOGON_BIN" index --recursive \
    --allowed-domain=www.typescriptlang.org \
    --allowed-pattern="^https://www\.typescriptlang\.org/tsconfig/.*" \
    --label="$label" \
    --delay=1 \
    --allow-sensitive \
    "https://www.typescriptlang.org/tsconfig/"
  export_dataset "typescript-docs" "$label"
}

fetch_react_docs() {
  label=react

  "$NOGON_BIN" index --recursive \
    --allowed-domain=react.dev \
    --allowed-pattern="^https://react\.dev/learn.*" \
    --label="$label" \
    --delay=1 \
    --allow-sensitive \
    "https://react.dev/learn"
  "$NOGON_BIN" index --recursive \
    --allowed-domain=react.dev \
    --allowed-pattern="^https://react\.dev/reference/.*" \
    --label="$label" \
    --delay=1 \
    --allow-sensitive \
    "https://react.dev/reference/react"
  export_dataset "react-docs" "$label"
}

fetch_django_docs() {
  label=django

  "$NOGON_BIN" index --recursive \
    --allowed-domain=docs.djangoproject.com \
    --allowed-pattern="^https://docs\.djangoproject\.com/en/stable/.*" \
    --label="$label" \
    --delay=1 \
    --allow-sensitive \
    "https://docs.djangoproject.com/en/stable/"
  export_dataset "django-docs" "$label"
}

fetch_postgresql_docs() {
  label=postgresql

  "$NOGON_BIN" index --recursive \
    --allowed-domain=www.postgresql.org \
    --allowed-pattern="^https://www\.postgresql\.org/docs/current/.*" \
    --label="$label" \
    --delay=1 \
    --allow-sensitive \
    "https://www.postgresql.org/docs/current/"
  export_dataset "postgresql-docs" "$label"
}

fetch_kubernetes_docs() {
  label=kubernetes

  "$NOGON_BIN" index --recursive \
    --allowed-domain=kubernetes.io \
    --allowed-pattern="^https://kubernetes\.io/docs/.*" \
    --exclude-pattern="^https://kubernetes\.io/docs/reference/generated/" \
    --label="$label" \
    --delay=1 \
    --allow-sensitive \
    "https://kubernetes.io/docs/home/"
  export_dataset "kubernetes-docs" "$label"
}

fetch_owasp_cheatsheets() {
  label=owasp

  "$NOGON_BIN" index --recursive \
    --allowed-domain=cheatsheetseries.owasp.org \
    --allowed-pattern="^https://cheatsheetseries\.owasp\.org/cheatsheets/.*" \
    --label="$label" \
    --delay=1 \
    --allow-sensitive \
    "https://cheatsheetseries.owasp.org/cheatsheets/"
  export_dataset "owasp-cheatsheets" "$label"
}

fetch_rfcs() {
  HELPERS_DIR="$SCRIPT_DIR/fetch_dataset_helpers"
  MIRROR="$HELPERS_DIR/my-rfc-mirror"

  rsync -a --delete \
    --include='*.html' --include='*.json' --include='*.txt' --include='*/' --exclude='*' \
    rsync.rfc-editor.org::rfcs "$MIRROR"

  python3 "$HELPERS_DIR/fetch_rfcs.py"
}

fetch_dataset() {
  case "$1" in
    go-stdlib)
      fetch_go_stdlib
      ;;
    mdn-css)
      fetch_mdn_css
      ;;
    mdn-html)
      fetch_mdn_html
      ;;
    mdn-js)
      fetch_mdn_js
      ;;
    powershell)
      fetch_powershell
      ;;
    python-docs)
      fetch_python_docs
      ;;
    rust-docs)
      fetch_rust_docs
      ;;
    nodejs-api)
      fetch_nodejs_api
      ;;
    typescript-docs)
      fetch_typescript_docs
      ;;
    react-docs)
      fetch_react_docs
      ;;
    django-docs)
      fetch_django_docs
      ;;
    postgresql-docs)
      fetch_postgresql_docs
      ;;
    kubernetes-docs)
      fetch_kubernetes_docs
      ;;
    owasp-cheatsheets)
      fetch_owasp_cheatsheets
      ;;
    rfcs)
      fetch_rfcs
      ;;
    all)
      for dataset in $DATASETS; do
        fetch_dataset "$dataset"
      done
      ;;
    *)
      printf 'Unknown dataset: %s\n\n' "$1" >&2
      print_options >&2
      exit 1
      ;;
  esac
}

if [ "$#" -eq 0 ]; then
  print_options
  exit 0
fi

if [ "$#" -ne 1 ]; then
  printf 'Expected exactly one argument.\n\n' >&2
  print_options >&2
  exit 1
fi

fetch_dataset "$1"
