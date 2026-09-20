#!/usr/bin/env bash
# Step 1: restrict ports 80/443 to Cloudflare's edge ranges, and install the
# weekly timer that keeps that list current.
#
# Ordering matters and is deliberate: the Cloudflare rules go in FIRST, and
# only once they are live are the open-to-the-world rules removed. There is
# never a moment when nothing can reach the web ports.

set -euo pipefail
[ "$(id -u)" -eq 0 ] || { echo "run this with sudo"; exit 1; }
HERE="$(cd "$(dirname "$0")" && pwd)"

# Guard: never proceed if SSH would be the only thing left unreachable.
if ! ufw status | grep -qiE '(^|[[:space:]])(22/tcp|22|OpenSSH)'; then
    echo "REFUSING: no SSH allow rule found in ufw -- fix that first"; exit 1
fi

echo "===== ufw BEFORE ====="
ufw status numbered

install -m 0755 "$HERE/cf-ufw-sync.sh"      /usr/local/sbin/cf-ufw-sync.sh
install -m 0644 "$HERE/cf-ufw-sync.service" /etc/systemd/system/cf-ufw-sync.service
install -m 0644 "$HERE/cf-ufw-sync.timer"   /etc/systemd/system/cf-ufw-sync.timer

echo
echo "===== adding Cloudflare ranges ====="
/usr/local/sbin/cf-ufw-sync.sh

echo
echo "===== removing the open-to-the-world web rules ====="
for rule in "Nginx Full" "Nginx HTTP" "Nginx HTTPS" "80/tcp" "443/tcp" "80" "443"; do
    if ufw delete allow "$rule" >/dev/null 2>&1; then echo "  removed: allow $rule"; fi
done

systemctl daemon-reload
systemctl enable --now cf-ufw-sync.timer

echo
echo "===== ufw AFTER ====="
ufw status numbered
echo
systemctl list-timers cf-ufw-sync.timer --no-pager || true
echo
echo "Done. Rollback if anything looks wrong:  sudo ufw allow 80 && sudo ufw allow 443"
