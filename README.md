# Quickmock

> Spin up an HTTP mock endpoint in 30 seconds. No signup, no config files, no SDK. Just a URL.

🇷🇺 **Читать на русском:** [README.ru.md](README.ru.md)

**[quickmock.dev →](https://quickmock.dev)**

Quickmock is a free service for developers who need a fake HTTP endpoint *right now* — to unblock a frontend, simulate an upstream API, or reproduce a flaky edge case. You paste a response body, pick a status code, and get back a public URL that behaves exactly the way you told it to.

**No trackers. No analytics. No accounts.** See [Privacy](#privacy) below.

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Made with Go](https://img.shields.io/badge/Made%20with-Go-00ADD8.svg)](https://go.dev)
[![No trackers](https://img.shields.io/badge/Trackers-0-success.svg)](#privacy)

---

## Quick start (30 seconds)

1. Open **[quickmock.dev](https://quickmock.dev)**
2. Paste your response body (JSON, XML, plain text — anything).
3. Click **Create Mock**.
4. Copy the generated URL: `https://quickmock.dev/m/abc123`
5. Use it anywhere:

```bash
curl https://quickmock.dev/m/abc123
```

That's it. No account, no API key, no waiting for an email.

> ⚠️ **Don't paste real secrets.** Mocks are public — anyone with the URL can read the response body. Never put real API keys, access tokens, passwords, or production data into a mock.

---

## Features

- **Create a mock in one form** — method, body, status, headers, delay.
- **All HTTP methods** — `GET`, `POST`, `PUT`, `PATCH`, `DELETE`, or `ANY`.
- **Custom response headers** — set `Content-Type`, CORS headers, anything.
- **Simulate slow APIs** — fixed delay, or a min–max range for random jitter.
- **Simulate flaky APIs** — an ordered response sequence cycled per call (1st → 200, 2nd → 500, repeat — classic retry testing) plus a configurable error rate that injects an alternate response for N% of requests.
- **CORS toggle** — one checkbox adds a permissive, credential-free `Access-Control-*` preset and answers `OPTIONS` preflight, so a mock is callable from browser JS on any origin.
- **Auto-expiring mocks** — TTL from 1 hour to 30 days.
- **Live inspector & config export** — requests stream in over SSE, and any mock's config can be exported/imported as JSON.
- **Copy as cURL** — one click to grab a ready-made test command.
- **Dynamic tokens** — drop `{{faker.uuid}}`, `{{now.iso8601}}`, etc. into the body; fresh values on every hit. See [Dynamic tokens](#dynamic-tokens).
- **Request echo** — `{{request.*}}` tokens reflect the incoming request back: method, path, IP, query params, headers, raw body, or a JSON field from the body.
- **Use-case guides** — `/guide` has ready-to-run recipes: retry testing, flaky/slow APIs, webhook inspection, fake data, and more.
- **Ready-made templates** — [`/templates`](https://quickmock.dev/templates) has ten one-click mock configurations (Stripe/Shopify webhooks, GitHub push events, OAuth2/OIDC discovery, JWKS, RFC 9457 errors, and more), each explaining its payload and how it differs from the real service.
- **Admin token** — manage your mock from any device: creating a mock returns a one-time admin token, and editing it, deleting it, or clearing its logs needs that token (`Authorization: Bearer`) from then on. Reading a mock, its logs, and the live inspector stay slug-only.
- **No signup** — mocks live in your browser's `localStorage`, and the slug alone still lets anyone read a mock, its logs, and its live inspector; editing, deleting, or clearing logs needs the mock's one-time admin token.
- **Bilingual UI** — English and Russian out of the box; more languages planned.

## Dynamic tokens

Need a different value every call instead of a frozen blob? Put a token in the response body — Quickmock stores the template as-is and substitutes a fresh value on every request. Unknown tokens are left alone, so existing template syntax in your payload won't be mangled.

```json
{
  "id": "{{faker.uuid}}",
  "name": "{{faker.name}}",
  "email": "{{faker.email}}",
  "created_at": "{{now.iso8601}}"
}
```

| Namespace   | Tokens                                                                                                                                                            |
| ----------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `faker.*`   | `name`, `firstname`, `lastname`, `username`, `email`, `phone`, `url`, `ipv4`, `uuid`, `int`, `bool`, `word`, `sentence`, `color`, `company`, `city`               |
| `now.*`     | `iso8601` (RFC 3339), `unix` (seconds), `unix_ms`, `date` (YYYY-MM-DD), `time` (HH:MM:SS), `rfc1123` (HTTP date)                                                  |
| `request.*` | `method`, `path`, `ip`, `query.<name>`, `header.<name>`, `body` (raw, first 16 KB), `body.<json.dot.path>` (e.g. `body.user.name`, `body.items.0.sku`)            |

`request.*` tokens echo the incoming request back into the response. Values are inserted verbatim — token-looking text inside a query param or body field is never re-expanded — and nothing extra is stored:

```json
{
  "echo_method": "{{request.method}}",
  "echo_id": "{{request.query.id}}",
  "echo_trace": "{{request.header.x-request-id}}",
  "echo_user": "{{request.body.user.name}}"
}
```

The full list with descriptions also lives inside the create form under "Dynamic tokens".

## Roadmap

Optional accounts, mock collections, conditional responses, webhook forwarding, a CLI, public templates.

---

## Privacy

Quickmock is run on enthusiasm, not on ad revenue. It is built to be useful — not to harvest you.

**What we do _not_ do:**

- No third-party analytics (Google Analytics, Plausible, Yandex.Metrika, anything).
- No tracking pixels.
- No browser fingerprinting.
- No external JavaScript, no CDN-hosted libraries — HTMX and Alpine.js are served from quickmock.dev.
- No external fonts — system fonts only.
- No ads, ever.
- No accounts, no email collection, no marketing list.
- No data sold or shared with anyone.
- No error-reporting service (Sentry/Bugsnag/etc.) by default. If that ever changes, it'll be opt-in and disclosed here.

**What we _do_ log, and why:**

| Data                              | Why                                               | Lifetime                                                                                   |
| --------------------------------- | ------------------------------------------------- | ------------------------------------------------------------------------------------------ |
| Your IP, in Redis                 | Rate limiting (5000 mock hits / IP / 8 hours)     | 8 hours, then deleted                                                                      |
| Creator IP, on each mock          | Enforce "50 active mocks per IP"                  | Until the mock is deleted or expires. Never shown in the UI.                               |
| Requests to your mocks            | This is literally the inspector feature           | Up to 100 newest per mock; auto-deleted when the mock expires; you can clear them any time |
| `lang` cookie                     | Remember your language choice                     | 1 year, or until you clear cookies. This is the only cookie.                               |
| Server-side request logs (stdout) | Operational debugging                             | systemd / Docker journal default — rotates with the OS                                     |

That's the entire list.

---

## Tech stack

- **Backend:** Go 1.22+
- **Frontend:** HTMX + Alpine.js + Go templates (no Node.js, no bundler)
- **Storage:** PostgreSQL (mocks, request logs) + Redis (rate limiting, ephemeral state)
- **Reverse proxy:** Nginx + Let's Encrypt

---

## Author

Made with ❤️ by **Nikita Chernykh** — backend developer & tech lead.

- Telegram: [@deadsquirrel93](https://t.me/deadsquirrel93)
- LinkedIn: [chernykh-nikita](https://www.linkedin.com/in/chernykh-nikita/)

---

## License

MIT — see [LICENSE](LICENSE).

---

## Support the project

Quickmock is built and maintained by one person on evenings and weekends. If it saved you time, you can say thanks:

- ⭐ **Star the repo** — costs nothing and genuinely helps with discoverability.
- 🧡 **Patreon:** [patreon.com/deadsquirrel93](https://www.patreon.com/c/deadsquirrel93)
- ☕ **Boosty:** [boosty.to/deadsquirrel93](https://boosty.to/deadsquirrel93)
- 💸 **Crypto (USDT, TRC20):** `TDfYMsk1hBxaZX7znM7yF1Zaezj3PRb4k5`

For developers who don't want to wait for the backend team.
