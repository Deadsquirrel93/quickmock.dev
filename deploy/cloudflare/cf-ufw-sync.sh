#!/usr/bin/env bash
# cf-ufw-sync.sh — keep ufw's 80/443 allow-list in step with Cloudflare's
# published edge ranges, so the origin only accepts web traffic that actually
# came through Cloudflare.
#
# Fails safe. If the lists can't be fetched, or come back malformed or
# implausibly short, this exits non-zero and leaves every existing rule
# exactly as it was — it never widens or clears the firewall on bad input.
#
# It only touches rules it created itself, tracked in its own state file.
# The SSH rule and anything you added by hand are out of its reach.

set -euo pipefail

TAG="cf-origin"
PORTS="80,443"
V4_URL="https://www.cloudflare.com/ips-v4"
V6_URL="https://www.cloudflare.com/ips-v6"
# Cloudflare has published 15 IPv4 and 7 IPv6 ranges for years. These floors
# are a tripwire against a truncated or tampered response, not exact counts.
MIN_V4=10
MIN_V6=4
# Overridable so the script can be exercised outside root in a test harness.
STATE_DIR="${CF_UFW_STATE_DIR:-/var/lib/cf-ufw-sync}"
STATE="$STATE_DIR/applied.txt"

log() { logger -t cf-ufw-sync -- "$*" 2>/dev/null || true; printf '%s\n' "$*"; }
die() { log "ABORT: $* -- firewall left unchanged"; exit 1; }

[ "$(id -u)" -eq 0 ] || die "must run as root"
command -v ufw >/dev/null 2>&1 || die "ufw is not installed"

tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT

fetch() { curl -fsS --max-time 20 --retry 3 --retry-delay 5 -o "$2" "$1"; }
fetch "$V4_URL" "$tmp/v4.raw" || die "could not fetch $V4_URL"
fetch "$V6_URL" "$tmp/v6.raw" || die "could not fetch $V6_URL"

tr -d '\r' < "$tmp/v4.raw" | sed '/^[[:space:]]*$/d' > "$tmp/v4"
tr -d '\r' < "$tmp/v6.raw" | sed '/^[[:space:]]*$/d' > "$tmp/v6"

if grep -qvE '^[0-9]{1,3}(\.[0-9]{1,3}){3}/[0-9]{1,2}$' "$tmp/v4"; then
    die "the IPv4 list contains a line that is not a CIDR"
fi
if grep -qvE '^[0-9A-Fa-f:]+/[0-9]{1,3}$' "$tmp/v6"; then
    die "the IPv6 list contains a line that is not a CIDR"
fi

# grep -c, not wc -l: Cloudflare's lists end without a trailing newline,
# which makes wc undercount by one.
n4="$(grep -c . "$tmp/v4")"; n6="$(grep -c . "$tmp/v6")"
[ "$n4" -ge "$MIN_V4" ] || die "only $n4 IPv4 ranges returned, expected at least $MIN_V4"
[ "$n6" -ge "$MIN_V6" ] || die "only $n6 IPv6 ranges returned, expected at least $MIN_V6"

# `awk 1` rather than `cat`: neither list ends with a newline, so cat would
# glue the last IPv4 range onto the first IPv6 one and hand ufw a single
# malformed address. awk re-emits every record with a proper terminator.
awk 1 "$tmp/v4" "$tmp/v6" | sort -u > "$tmp/desired"

# Validate the MERGED list, not just the two inputs. Checking the inputs
# alone is what let the concatenation bug through: each file was individually
# well-formed, and the corruption only existed at the seam between them.
if grep -qvE '^([0-9]{1,3}(\.[0-9]{1,3}){3}|[0-9A-Fa-f:]+)/[0-9]{1,3}$' "$tmp/desired"; then
    die "merged list has a malformed entry: $(grep -m1 -vE '^([0-9]{1,3}(\.[0-9]{1,3}){3}|[0-9A-Fa-f:]+)/[0-9]{1,3}$' "$tmp/desired")"
fi
n_all="$(grep -c . "$tmp/desired")"
[ "$n_all" -eq "$((n4 + n6))" ] || die "merged list has $n_all entries, expected $((n4 + n6))"

# The state file, not `ufw status` output, is what we diff against: parsing
# ufw's human-readable table is brittle across versions, and a misparse here
# would mean deleting rules we meant to keep.
mkdir -p "$STATE_DIR"; touch "$STATE"
sort -u "$STATE" > "$tmp/current"

comm -23 "$tmp/desired" "$tmp/current" > "$tmp/add"
comm -13 "$tmp/desired" "$tmp/current" > "$tmp/del"

added=0; removed=0
while IFS= read -r cidr; do
    [ -n "$cidr" ] || continue
    ufw allow proto tcp from "$cidr" to any port "$PORTS" comment "$TAG" >/dev/null \
        || die "ufw rejected the range $cidr"
    added=$((added + 1))
done < "$tmp/add"

while IFS= read -r cidr; do
    [ -n "$cidr" ] || continue
    if ufw delete allow proto tcp from "$cidr" to any port "$PORTS" >/dev/null 2>&1; then
        removed=$((removed + 1))
    else
        log "warning: stale rule for $cidr could not be deleted, remove it by hand"
    fi
done < "$tmp/del"

cp "$tmp/desired" "$STATE"

# Cross-check the state file against reality, so hand edits surface in the
# log instead of silently drifting.
live="$(ufw status | grep -cF "# $TAG" || true)"
want="$(wc -l < "$tmp/desired")"
[ "$live" -eq "$want" ] || log "warning: $live tagged rules live but $want expected -- check 'ufw status numbered'"

log "ok: $want Cloudflare ranges allowed on $PORTS (added $added, removed $removed)"
