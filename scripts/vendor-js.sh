#!/usr/bin/env bash
# Vendor HTMX and Alpine.js into web/static/js so we don't load them from a CDN.
# Run once after clone (or to upgrade). The downloaded files are committed.
set -euo pipefail

DIR="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$DIR/web/static/js"
mkdir -p "$OUT"

HTMX_VERSION="${HTMX_VERSION:-1.9.12}"
ALPINE_VERSION="${ALPINE_VERSION:-3.13.10}"

echo "==> HTMX $HTMX_VERSION"
curl -fsSL "https://unpkg.com/htmx.org@${HTMX_VERSION}/dist/htmx.min.js" \
    -o "$OUT/htmx.min.js"

echo "==> Alpine.js $ALPINE_VERSION"
curl -fsSL "https://unpkg.com/alpinejs@${ALPINE_VERSION}/dist/cdn.min.js" \
    -o "$OUT/alpine.min.js"

echo "==> Done. Sizes:"
ls -lh "$OUT"/*.js
