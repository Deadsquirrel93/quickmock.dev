# Locking the origin behind Cloudflare

Everything in this directory exists to answer one question: **can you trust the client IP
your application sees?**

If your app rate-limits, blocks or counts anything per IP, and it sits behind a CDN, the
answer is "only if the origin refuses to talk to anyone but that CDN". This is the write-up
of getting there, including the parts that went wrong.

---

## The problem, concretely

Behind a proxy the TCP peer is the proxy, not the visitor. So the visitor's address has to
arrive in a header, and the application has to decide which header to believe.

That decision is a trap, because the obvious candidates are **append-only**:

| Header | Set by | Safe to trust? |
| --- | --- | --- |
| `X-Forwarded-For` | every hop, appending | **No.** A spec-abiding proxy *adds* its hop and keeps what the client sent, so the client's own forged value stays in front. |
| `X-Real-IP` | your own nginx, overwriting | Only when nginx is the *first* hop. Behind a CDN it holds the CDN edge's address, not the visitor's. |
| `CF-Connecting-IP` | Cloudflare, overwriting | Yes — Cloudflare replaces whatever the client sent. This is the one to read. |

So the app reads `CF-Connecting-IP` and only when the TCP peer is loopback or private, i.e. the
request demonstrably came from our own nginx. In this repo that is
`internal/middleware/realip.go`, driven by `QUICKMOCK_REAL_IP_HEADER`.

**That is not enough on its own**, and this is the whole point of this directory. Cloudflare
only overwrites the header for traffic that goes *through* Cloudflare. If the origin also
answers on its public address, anyone who knows it can connect directly and send whatever
`CF-Connecting-IP` they like. Every per-IP control then becomes decoration — and worse than
decoration, because an attacker can pin their traffic on an innocent address.

## "But nobody knows my origin IP"

They do, or they can. Hiding it is the weakest layer of a CDN, not the protection.

- The whole IPv4 space is scanned continuously on 443. Scanners record the certificate each
  address presents, and those datasets are publicly searchable. An origin serving a valid
  certificate for your domain is therefore *findable by name*, without anything leaking.
- Historical DNS records predate the move to a CDN.
- Mail headers, misconfigured subdomains, old CI logs, `SPF`/`MX` records that point at the
  same box.

Treat the origin address as public. The job is to make knowing it useless.

## The two controls

Neither is sufficient alone. They fail in opposite directions, which is why both are here.

### 1. Firewall: only Cloudflare may open a connection

`ufw` allows 80/443 from Cloudflare's published ranges only. An attacker never completes a TCP
handshake, so nginx never spends a cycle on them.

What it cannot do: Cloudflare's ranges are shared by **every** Cloudflare customer. Anyone can
sign up for free, point their own zone at your origin, and arrive from an allowed address. The
firewall cannot tell whose Cloudflare traffic it is.

### 2. Authenticated Origin Pulls: only Cloudflare may complete a TLS handshake

Cloudflare presents a TLS **client** certificate on each connection to the origin; nginx
verifies it against Cloudflare's origin-pull CA and refuses connections that lack it.

What it cannot do: it only guards TLS on 443, per vhost. Port 80 has no client certificates at
all. And rejection happens *after* a full TLS handshake, which is CPU-expensive — a handshake
flood is still a denial-of-service on a small box even when every request is refused.

**Together:** the firewall filters the path, AOP proves the tenant. Each covers the other's
blind spot.

> The **Global** AOP toggle uses a certificate shared across all Cloudflare customers, so
> strictly it proves "came from Cloudflare", not "came through *your* zone". That is enough to
> make `CF-Connecting-IP` trustworthy, because *any* path through Cloudflare has the header
> overwritten. Use **Zone-level** AOP with your own uploaded certificate if you also need to
> keep other Cloudflare tenants off your origin entirely.

## Order of operations

Sequence matters — get it wrong and you take the site down.

1. **Cloudflare dashboard** → SSL/TLS → Origin Server → Authenticated Origin Pulls → **Global: on**.
   Safe to do first: nginx is not asking for a certificate yet, so it simply ignores the one
   being offered. The dangerous order is the reverse.
2. **`install-2-aop-probe.sh`** — installs the CA and sets `ssl_verify_client optional`, which
   verifies a certificate when offered but rejects nothing. It reports the verdict in an
   `X-AOP-Verify` response header and curls the site through Cloudflare to read it back. Do not
   continue until that prints `SUCCESS`.
3. **`install-3-aop-enforce.sh`** — switches to `ssl_verify_client on`, then verifies over the
   real Cloudflare path and **rolls back automatically** if the site stops answering.
4. **`install-1-ufw.sh`** — adds the Cloudflare ranges *first*, and only once they are live
   removes the open-to-the-world rules, so there is never a window where nothing can reach the
   web ports. Installs the sync timer. It refuses to run if no SSH rule exists.

Each script is idempotent and prints what it changed. Override `DOMAIN`, `VHOST` or `SNIP` by
environment variable to reuse them on another project.

## Keeping the ranges current, unattended

`cf-ufw-sync.sh` + `cf-ufw-sync.timer` refresh the allow-list weekly. The interesting part is
the failure behaviour, because a firewall sync that goes wrong fails **closed** — it can take a
site off the internet:

- Cannot fetch the lists, or they are malformed, or implausibly short → **changes nothing** and
  exits non-zero. It never widens or clears the firewall on bad input.
- It diffs against its own state file (`/var/lib/cf-ufw-sync/applied.txt`), not against parsed
  `ufw status` output. Parsing a human-readable table is brittle, and a misparse here means
  deleting rules you meant to keep.
- It only ever touches rules carrying its own `cf-origin` comment. The SSH rule is out of reach
  by construction.

## Pitfalls worth knowing

**Cloudflare's published lists do not end with a newline.** `cat ips-v4 ips-v6` therefore glues
the last IPv4 range onto the first IPv6 one and produces a single malformed address like
`131.0.72.0/222400:cb00::/32`, which `ufw` rejects with `ERROR: Bad source address`. Use
`awk 1` instead of `cat`. For the same reason `wc -l` undercounts each file by one — count with
`grep -c .`.

**Validate the merged list, not the inputs.** The bug above slipped through validation that
checked each file separately: both were individually well-formed, and the corruption existed
only at the seam between them.

**ACME renewal keeps working.** DNS points at Cloudflare, so Let's Encrypt validates through
Cloudflare and the challenge arrives from an allowed range. No exception rule is needed.

**The firewall is per-port, not per-vhost.** Every site on the box is affected. Confirm they are
all behind Cloudflare before restricting anything.

**After this, the origin address answers nothing.** `curl --resolve site:443:<origin>` will hang
rather than reply. That is correct, not an outage — diagnose through the domain.

## Rollback

```bash
# firewall
sudo ufw allow 80 && sudo ufw allow 443

# authenticated origin pulls
sudo sed -i 's/^ssl_verify_client on;/ssl_verify_client optional;/' \
    /etc/nginx/snippets/cloudflare-aop.conf && sudo nginx -t && sudo systemctl reload nginx
```

## Verifying it actually works

Through Cloudflare the site answers; straight to the origin address nothing does, including a
request carrying a forged `CF-Connecting-IP`. Then confirm the application resolves the real
visitor address — the simplest check is a mock whose body is `{{request.ip}}`, called from a
machine whose public address you know.
