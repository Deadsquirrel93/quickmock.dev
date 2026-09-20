#!/usr/bin/env bash
# Step 3: turn Authenticated Origin Pulls from a probe into enforcement.
#
# Only run this once step 2 reported X-AOP-Verify: SUCCESS. It swaps
# 'optional' for 'on', so nginx refuses any TLS connection that does not
# carry Cloudflare's client certificate.
#
# It verifies itself over the real Cloudflare path afterwards and rolls back
# automatically if the site stops answering, so a mistake here costs seconds,
# not an outage.

set -euo pipefail
[ "$(id -u)" -eq 0 ] || { echo "run this with sudo"; exit 1; }

DOMAIN="${DOMAIN:-quickmock.dev}"
SNIP="${SNIP:-/etc/nginx/snippets/cloudflare-aop.conf}"
ORIGIN_IP="$(curl -fsS --max-time 10 https://api.ipify.org || true)"
[ -f "$SNIP" ] || { echo "$SNIP missing -- run install-2-aop-probe.sh first"; exit 1; }

cp -a "$SNIP" "$SNIP.bak"
rollback() {
    echo "!! rolling back to probe mode"
    cp -a "$SNIP.bak" "$SNIP"
    nginx -t && systemctl reload nginx
    echo "!! rolled back. The site is serving again; do not re-run until the cause is known."
}

sed -i 's/^ssl_verify_client optional;/ssl_verify_client on;/' "$SNIP"
sed -i '/add_header X-AOP-Verify/d' "$SNIP"
sed -i 's/^# PROBE MODE:.*/# ENFORCING: a connection without Cloudflare'"'"'s client certificate is refused./' "$SNIP"
grep -q '^ssl_verify_client on;' "$SNIP" || { echo "could not switch to 'on'"; rollback; exit 1; }

if ! nginx -t; then rollback; exit 1; fi
systemctl reload nginx
echo "enforcement enabled; verifying..."
sleep 2

echo
echo "===== A) the real path: through Cloudflare (must be 200) ====="
code="$(curl -sS -o /dev/null -w '%{http_code}' --max-time 20 https://$DOMAIN/healthz || echo 000)"
echo "  https://$DOMAIN/healthz -> $code"
if [ "$code" != "200" ]; then
    echo "  the site is NOT answering through Cloudflare"
    rollback
    exit 1
fi

if [ -n "$ORIGIN_IP" ]; then
    echo
    echo "===== B) the bypass: straight to the origin (must NOT be 200) ====="
    dcode="$(curl -sS -o /dev/null -w '%{http_code}' --max-time 20 \
             --resolve "$DOMAIN:443:$ORIGIN_IP" https://$DOMAIN/healthz 2>/dev/null || echo refused)"
    echo "  direct to $ORIGIN_IP -> $dcode"
    [ "$dcode" = "200" ] && echo "  WARNING: the bypass still works, investigate before trusting CF-Connecting-IP"
fi

echo
echo "Done. Rollback by hand if ever needed:"
echo "  sudo sed -i 's/^ssl_verify_client on;/ssl_verify_client optional;/' $SNIP && sudo nginx -t && sudo systemctl reload nginx"
