# Security

Tracker is a single-user tool. It was specified for an API exposed to the
public internet and is built to that standard.
This document records how the API maps to the
[OWASP API Security Top 10 (2023)](https://owasp.org/API-Security/editions/2023/en/0x11-t10/),
what the desktop app does to protect the session, and what is **not** fully
addressed.

## Threat model in one paragraph

The API is designed as if any attacker on the internet could reach it — which,
in the default deployment, they can. Narrowing exposure (`TRACKER_BIND_IP`, a
firewall, a VPN) is an optional additional layer, not a replacement for any
control below. Attackers considered: anyone who can reach the API. They do
not have the password or a session token. Secondary threats: a stolen
device or token, a malicious local process on one of the desktop machines,
and a compromised dependency. Out of scope: a compromised server host or
Docker daemon (they own the database file regardless).

## OWASP API Security Top 10 (2023)

### API1 Broken Object Level Authorization — addressed
- Every query that reads or writes user data includes `user_id = ?` in the SQL
  itself (`api/internal/store`). Child rows (tasks, log entries, tag links)
  are only reachable through `project_id IN (SELECT id FROM projects WHERE
  user_id = ?)` or a join on `projects.user_id`; inserts use
  `INSERT … SELECT … FROM projects WHERE id = ? AND user_id = ?`, so a child
  can never be attached to someone else's project.
- Tags are matched only among the caller's own tags; linking checks both the
  project's and the tag's owner in the same statement.
- Another user's resource returns **404**, never 403. Reordering with a
  foreign id returns the same 400 as any malformed order, revealing nothing.
- `TestCrossUserIsolation` runs every endpoint as a second user against the
  first user's project, task, log entry, tag and session, then asserts the
  first user's data is unchanged.

### API2 Broken Authentication — addressed
- No registration endpoint; the user is created with the `tracker-api user
  create` CLI on the server, which reads the password from the terminal
  without echo.
- Passwords: Argon2id, m=19 MiB, t=2, p=1 (OWASP Password Storage Cheat
  Sheet), 16-byte random salt, PHC string format, constant-time comparison,
  minimum 12 characters. Hashing concurrency is capped so a login burst cannot
  exhaust memory.
- Login returns a 256-bit `crypto/rand` token; only its SHA-256 is stored.
  No JWTs, no cookies (so no CSRF surface). Bearer header only.
- Sessions: 30-day sliding idle expiry, 180-day absolute expiry, individually
  revocable (`DELETE /v1/auth/sessions/{id}`), logout revokes the current one,
  password change revokes all others, CLI `user reset-password` and
  `sessions revoke-all` revoke everything.
- Unknown email and wrong password return the identical body and status, and
  both compute an Argon2id hash (dummy hash for unknown emails), so timing does
  not reveal which accounts exist (`TestLoginTimingUnknownEmail`).
- Brute force: 5 attempts/minute per IP and 10 attempts/15 minutes per account,
  then a lockout with exponential backoff (IP: 1 min doubling to 1 h; account:
  15 min doubling to 24 h; the backoff level decays after 24 h without a
  lockout). Account lockouts are keyed by the normalised email, so they behave
  identically for non-existent accounts. Password-change attempts count
  against the same account budget, so a stolen token cannot be used to
  brute-force the current password.
- Every login success, failure, lockout, logout, revocation and password
  change is written to `auth_events` with the client IP.

### API3 Broken Object Property Level Authorization — addressed
- Request bodies are decoded into explicit per-endpoint structs with
  `DisallowUnknownFields`; fields such as `user_id`, `position`, `created_at`
  or `completed_at` cannot be set by a client (mass assignment is impossible).
- Responses are explicit structs. The password hash and token hash are never
  serialised; `Session.UserID` is excluded from JSON.

### API4 Unrestricted Resource Consumption — addressed (see limitations)
- Body caps: 64 KB default, 1 MB on the routes that carry a description or
  log body; enforced with `http.MaxBytesReader` even without `Content-Length`.
  Caddy additionally caps bodies at 2 MB.
- Field limits (code points): name 200, task title 500, description and log
  body 100,000, tag 40, ≤ 20 tags per project, device name 100.
- Global token-bucket rate limit of 120 requests/minute (burst 30) per IP and,
  separately, per session; 429 with `Retry-After`.
- Server timeouts: `ReadHeaderTimeout` 5 s, `ReadTimeout` 15 s,
  `WriteTimeout` 30 s, `IdleTimeout` 60 s, `MaxHeaderBytes` 16 KB.
- Rate-limit state maps are bounded (100k keys, idle keys swept every minute).

### API5 Broken Function Level Authorization — addressed
- There is one role. Administrative functions (create user, reset password,
  revoke all sessions) are CLI-only and not reachable over HTTP.
- Every route except `GET /v1/healthz` and `POST /v1/auth/login` is wrapped in
  the session middleware at registration time (`server.routes`).

### API6 Unrestricted Access to Sensitive Business Flows — addressed
- The only sensitive flows are login and password change; both are covered by
  the lockout guard above.

### API7 Server Side Request Forgery — not applicable
- The API never makes outbound requests. The API container sits on an
  `internal: true` Docker network with no route to the internet.

### API8 Security Misconfiguration — addressed
- TLS terminates at Caddy. With `TRACKER_TLS` set to an e-mail address, Caddy
  obtains and renews Let's Encrypt certificates automatically (ACME); set to
  `internal`, it issues a certificate from its local CA (self-signed, for
  LAN-only servers). Keys live in the `caddy-data` Docker volume. HTTP→HTTPS
  redirect, HSTS (2 years, includeSubDomains), TLS 1.2+ only, `Server`/`Via`
  headers removed, admin endpoint disabled.
- `TRACKER_BIND_IP` narrows the address Caddy's ports are published on
  (default: all interfaces); the API container itself publishes no ports.
- Every API response carries `X-Content-Type-Options: nosniff`,
  `Cache-Control: no-store`, `Referrer-Policy: no-referrer`,
  `Content-Security-Policy: default-src 'none'; frame-ancestors 'none'; …`,
  `X-Frame-Options: DENY`. No CORS headers are ever sent (browser preflights
  are not approved). Tested on 200/400/401/404/429 responses.
- `Content-Type: application/json` is required for every JSON body (415
  otherwise); a single JSON object only.
- Errors are generic; panics are recovered and logged server-side with a
  request id; no stack traces, SQL errors or paths reach clients.
- `GET /v1/healthz` returns `{"status":"ok"}` and no version/build data.
- Containers: multi-stage build; final image is `distroless/static:nonroot`
  (no shell); uid 65532; read-only root filesystem; all capabilities dropped
  (Caddy keeps only `NET_BIND_SERVICE`); `no-new-privileges`. The database
  lives on a named volume; the directory is 0700 and the database, WAL and
  SHM files are 0600 (umask 077 plus explicit chmod). Only Caddy publishes
  ports.
- Configuration is through environment variables only; no secrets exist in the
  repository (`deploy/.env` holds only the host name, TLS mode, bind address
  and port; certificates live in the `caddy-data` Docker volume).
- The client IP is taken from `X-Forwarded-For` only when the socket peer is
  Caddy's fixed address on the internal network, and then only the rightmost
  entry (the one Caddy appended) is used.

### API9 Improper Inventory Management — addressed
- One versioned prefix (`/v1`), one deployment, every route registered in one
  function. Unknown paths return a JSON 404.

### API10 Unsafe Consumption of APIs — not applicable
- The API consumes no third-party APIs.

### Input handling (cross-cutting)
- Parameterised SQL only; there is no string-built SQL with user input (the
  only concatenation joins constant SQL fragments).
- Ids in paths and in the reorder body are validated as canonical UUIDs before
  any database access (400 `invalid id`).
- Single-line fields are trimmed and reject all control characters; long text
  normalises CRLF/CR to LF and rejects control characters other than LF and
  tab. Invalid UTF-8 is rejected.
- Logs are structured JSON with a request id and never contain passwords,
  tokens, the `Authorization` header, query strings or request bodies
  (`TestLogsOmitSecrets`).

## Desktop app

- All HTTP calls are made from the Go side of the Wails app; the frontend
  only calls bound methods. The session token never enters the webview.
- The token is stored in the OS keychain (macOS Keychain / Linux Secret
  Service via `zalando/go-keyring`; on macOS the secret is passed to
  `security` over stdin, not argv). It is never written to a file or to
  `localStorage`. The config file (0600, in the OS config directory) holds
  only the server address, the email and the remembered tag filter.
- The HTTP client never follows redirects, so the bearer token cannot be
  replayed to another host; responses are size-capped.
- Only `https://` server addresses are accepted; `http://localhost` is
  allowed only in builds made with the `dev` build tag (`make desktop-dev`).
  TLS uses the system certificate store. Certificate verification can be
  disabled for a server, but only by the user's explicit opt-in on the
  Connect screen ("Disable SSL certificate verification", off by default,
  shown with a warning, stored in the local config) — see limitation 16.
- The webview loads only bundled assets (fonts included); a strict CSP
  (`default-src 'self'`, no remote origins) is set in `index.html`.
- The Linux packages ship an AppArmor profile (`/etc/apparmor.d/tracker`,
  `flags=(unconfined)`) whose only effect is granting `userns`, so that
  WebKitGTK's own bubblewrap sandbox can keep working on kernels that
  restrict unprivileged user namespaces (Ubuntu 23.10+). It enables the web
  process sandbox rather than weakening anything.
- On any 401 the stored token is deleted and the app returns to the Connect
  screen.

## Known limitations / not fully addressed

1. **Rate-limit and lockout state is in memory.** Restarting the API resets
   the counters (the audit trail in `auth_events` survives). An attacker
   cannot trigger a restart over the API, so this was accepted for a
   single-process deployment.
2. **Distributed brute force.** Per-IP limits do not stop an attacker with many
   IPs; the per-account limit (10 per 15 minutes, escalating to 24 h lockouts)
   is what bounds guessing then. With a 12+ character password this is
   infeasible, but it also means such an attacker can keep the account locked
   out (a denial of service against the legitimate user's *new* logins;
   existing sessions keep working).
3. **`auth_events` grows without bound.** Each failed login writes a row
   (attempts during a lockout do not). There is no retention job; prune old
   rows manually if it ever matters (`DELETE FROM auth_events WHERE
   created_at < …`).
4. **JSON field names are matched case-insensitively** by Go's
   `encoding/json` (`{"NAME": …}` is accepted as `name`). Unknown fields are
   still rejected, so this does not allow setting anything extra.
5. **Unicode case-insensitive tag comparison uses simple case folding**
   (`strings.EqualFold`); e.g. `ß` and `SS` are different tags.
6. **No token binding.** A token copied off a device works from anywhere until
   it is revoked or expires; revoke it from another device's settings or with
   `tracker-api sessions revoke-all`.
7. **Caddy runs as root inside its container** (with all capabilities dropped
   except `NET_BIND_SERVICE`, a read-only root filesystem and
   `no-new-privileges`), because the official image expects it.
8. **The SQLite file is not encrypted at rest.** Protect the server and its
   backups; the backups contain password hashes and session token hashes.
9. **The API is internet-facing in the default deployment.** Anyone can reach
   the login and healthz endpoints; the rate limits and lockouts above bound
   password guessing. Narrow exposure with `TRACKER_BIND_IP`, a firewall or a
   VPN if you do not need public reachability. With Let's Encrypt, the
   domain name is published in Certificate Transparency logs.
10. **govulncheck needs `vuln.go.dev`.** If your network blocks it, run
   `make check VULNCHECK_IN_DOCKER=1` (the database is fetched through a
   container and the scan runs locally against that copy).

### Desktop

11. **macOS Keychain access control.** `zalando/go-keyring` creates the item
    through `/usr/bin/security`, so the item's access list trusts that tool:
    any process running as your macOS user can read the token with
    `security find-generic-password` without a prompt. This is still far
    better than a file (no plaintext on disk, not in backups), but it is not
    per-app isolation. Binding the item to the app would require the native
    Keychain API through cgo (another dependency); not done without your OK.
12. **Linux needs a running Secret Service** (GNOME Keyring, KWallet ≥ 5.97,
    KeePassXC…). Without one, connecting fails with an explicit error; there
    is deliberately no file fallback.
13. **Dev builds are weaker by design.** `make desktop-dev` (build tag `dev`)
    accepts `http://localhost` servers and serves the UI from the Vite dev
    server without the CSP, enables the web inspector and the default context
    menu. Release builds (`make desktop-build`) have none of this.
14. **The macOS app is ad-hoc signed, not notarized.** Gatekeeper will ask for
    confirmation the first time it is opened (right-click → Open).
15. **The Linux build was compiled and vetted in a Debian 13 container (GTK
    4.18, WebKitGTK 2.54) but not exercised interactively** during
    development; the macOS build was.
16. **Disabling certificate verification removes MITM protection.** With
    "Disable SSL certificate verification" enabled, the app accepts any
    certificate for that server: anyone between the device and the server can
    read and modify the traffic, including the password and session token. It
    exists for self-signed deployments (`TRACKER_TLS=internal`); the safer
    alternative is to trust Caddy's root CA on the device. It is off by
    default, never enabled silently, and only affects the device on which it
    was chosen.
