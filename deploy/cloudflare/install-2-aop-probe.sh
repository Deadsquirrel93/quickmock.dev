#!/usr/bin/env bash
# Step 2: install Cloudflare's origin-pull CA and wire it into the quickmock
# vhost in PROBE mode -- `ssl_verify_client optional`.
#
# Probe mode asks for the client certificate and records the verdict in an
# X-AOP-Verify response header, but rejects nothing. That makes it safe to
# apply before you know whether Cloudflare is really sending a certificate:
# if the zone toggle is still off, the site keeps working and the header
# simply reads NONE. Step 3 turns it into enforcement.

set -euo pipefail
[ "$(id -u)" -eq 0 ] || { echo "run this with sudo"; exit 1; }

DOMAIN="${DOMAIN:-quickmock.dev}"
VHOST="${VHOST:-/etc/nginx/sites-available/quickmock}"
CA_DIR=/etc/nginx/cloudflare
CA="$CA_DIR/authenticated_origin_pull_ca.pem"
SNIP=/etc/nginx/snippets/cloudflare-aop.conf
CA_URL=https://developers.cloudflare.com/ssl/static/authenticated_origin_pull_ca.pem

mkdir -p "$CA_DIR" /etc/nginx/snippets
curl -fsS --max-time 20 --retry 3 -o "$CA.new" "$CA_URL" || { echo "could not download the CA"; exit 1; }

# Refuse anything that is not the certificate we expect.
subj="$(openssl x509 -in "$CA.new" -noout -subject 2>/dev/null || true)"
case "$subj" in
    *origin-pull.cloudflare.net*) : ;;
    *) echo "downloaded CA is not Cloudflare's origin pull root: $subj"; rm -f "$CA.new"; exit 1 ;;
esac
openssl x509 -in "$CA.new" -noout -checkend 0 >/dev/null || { echo "CA is expired"; rm -f "$CA.new"; exit 1; }
mv "$CA.new" "$CA"; chmod 0644 "$CA"
echo "CA installed: $subj"

cat > "$SNIP" <<CONF
# Cloudflare Authenticated Origin Pulls.
#
# Cloudflare presents a TLS client certificate signed by its origin-pull CA
# on every connection to this origin, provided the zone has Authenticated
# Origin Pulls switched on. Verifying it means a request that did not come
# through Cloudflare cannot be served -- which is what makes the
# CF-Connecting-IP header (QUICKMOCK_REAL_IP_HEADER) trustworthy.
#
# PROBE MODE: 'optional' verifies the certificate when one is offered but
# still serves the request when it is absent. X-AOP-Verify reports the
# verdict (SUCCESS / NONE / FAILED). Step 3 swaps this for 'on'.
ssl_client_certificate $CA;
ssl_verify_client optional;
add_header X-AOP-Verify \$ssl_client_verify always;
CONF
echo "snippet written: $SNIP"

cp -a "$VHOST" "$VHOST.bak-$(date +%Y%m%d-%H%M%S)"

python3 - "$VHOST" "$SNIP" <<'PY'
import re, sys
path, snip = sys.argv[1], sys.argv[2]
src = open(path).read()
include = "include %s;" % snip
if include in src:
    print("include already present, vhost untouched")
    raise SystemExit(0)
out, n = [], 0
for line in src.splitlines(keepends=True):
    out.append(line)
    if re.match(r'^(\s*)listen\s+443\s+ssl', line):
        indent = re.match(r'^(\s*)', line).group(1)
        out.append("%s%s\n" % (indent, include))
        n += 1
if n == 0:
    sys.exit("no 'listen 443 ssl' line found -- vhost not modified")
open(path, "w").write("".join(out))
print("include added to %d TLS server block(s)" % n)
PY

if ! nginx -t; then
    echo "nginx config test FAILED -- restoring the backup"
    cp -a "$(ls -t "$VHOST".bak-* | head -1)" "$VHOST"
    exit 1
fi
systemctl reload nginx
echo

echo "===== probe: what does Cloudflare actually send? ====="
# DNS for the site points at Cloudflare, so this request leaves the box,
# reaches Cloudflare, and comes back to this origin over the real path.
verdict="$(curl -sS -I --max-time 20 https://$DOMAIN/healthz | tr -d '\r' | awk -F': ' 'tolower($1)=="x-aop-verify"{print $2}')"
echo "X-AOP-Verify: ${verdict:-<header not seen>}"
case "$verdict" in
    SUCCESS) echo; echo "Cloudflare IS presenting a valid client certificate. Safe to run install-3-aop-enforce.sh." ;;
    NONE)    echo; echo "No client certificate. Turn ON SSL/TLS -> Origin Server -> Authenticated Origin Pulls -> Global for $DOMAIN, then re-run this script." ;;
    *)       echo; echo "Unexpected verdict. Do NOT run step 3 until this reads SUCCESS." ;;
esac
