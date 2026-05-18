#!/usr/bin/env bash
# Verify locale parity and that no external JS/CSS/font hosts leaked into
# templates. Designed to run in CI.
set -euo pipefail

DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$DIR"

echo "==> Locale parity"
python3 - <<'PY'
import json, re, sys, pathlib
loc = pathlib.Path("locales")
en = json.loads((loc / "en.json").read_text())
fail = False
for p in sorted(loc.glob("*.json")):
    if p.name == "en.json":
        continue
    other = json.loads(p.read_text())
    missing = set(en) - set(other)
    extra = set(other) - set(en)
    if missing:
        print(f"  {p.name}: MISSING {sorted(missing)}")
        fail = True
    if extra:
        print(f"  {p.name}: EXTRA   {sorted(extra)}")
        fail = True
    fmt = re.compile(r"%[sd]")
    for k in set(en) & set(other):
        if fmt.findall(en[k]) != fmt.findall(other[k]):
            print(f"  {p.name}: FORMAT mismatch on {k}")
            fail = True
    if not (missing or extra):
        print(f"  {p.name}: OK ({len(other)} keys)")
sys.exit(1 if fail else 0)
PY

echo "==> No third-party hosts in templates"
BAD=$(grep -rEn 'google-analytics|googletagmanager|plausible\.io|mc\.yandex|fonts\.googleapis|fonts\.gstatic|cdn\.jsdelivr|cdnjs\.cloudflare|unpkg\.com' web/templates/ || true)
if [ -n "$BAD" ]; then
    echo "$BAD"
    echo "Found references to external hosts in templates. See ARCHITECTURE.md > Privacy."
    exit 1
fi
echo "  clean"
