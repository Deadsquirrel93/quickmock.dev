#!/usr/bin/env bash
# Production deploy script. Runs on the server.
#
# Default mode: Docker Compose. Set DEPLOY_MODE=baremetal for the systemd path.

set -euo pipefail

APP_DIR="${APP_DIR:-/var/www/quickmock/current}"
BRANCH="${BRANCH:-main}"
DEPLOY_MODE="${DEPLOY_MODE:-docker}"

cd "$APP_DIR"

echo "==> Fetching latest from origin/$BRANCH"
git fetch --prune origin
git checkout "$BRANCH"
git pull --ff-only origin "$BRANCH"

case "$DEPLOY_MODE" in
    docker)
        echo "==> Rebuilding Docker stack"
        docker compose pull --ignore-pull-failures
        docker compose up -d --build
        ;;
    baremetal)
        echo "==> Building binary"
        go build -trimpath -ldflags="-s -w" -o bin/quickmock ./cmd/server
        echo "==> Running migrations"
        ./bin/quickmock migrate
        echo "==> Restarting systemd unit"
        sudo systemctl restart quickmock
        ;;
    *)
        echo "Unknown DEPLOY_MODE: $DEPLOY_MODE" >&2
        exit 2
        ;;
esac

echo "==> Waiting for /healthz"
for i in $(seq 1 30); do
    if curl -fsS http://127.0.0.1:8080/healthz >/dev/null; then
        echo "==> Deploy complete: $(git rev-parse --short HEAD)"
        exit 0
    fi
    sleep 1
done

echo "!! Service did not become healthy in 30s."
echo "   docker:    docker compose logs --tail=200 app"
echo "   baremetal: journalctl -u quickmock -n 200"
exit 1
