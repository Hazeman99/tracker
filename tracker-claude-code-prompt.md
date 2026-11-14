# Build "Tracker": a minimal personal project tracker (API + desktop app)

Build a two-part application in one repository:

1. **`api/`** — a backend API that will run on my public Linux server, reachable from the internet.
2. **`desktop/`** — a desktop app that runs on my personal MacBook (Apple Silicon) and my work Linux PC, and talks to that API.

It is a single-user tool for me. Keep it minimal: no feature that is not written here. Because the API is exposed to the public internet, **security is the top priority and overrides convenience everywhere** (see the Security section; treat every item there as a requirement, not a suggestion).

Before writing code, give me a short plan and the list of dependencies you intend to use, then wait for my OK. Build the API first (with tests), then the desktop app.

## Stack

- **API:** Go (current stable), standard library `net/http` router, SQLite in WAL mode via a pure-Go or CGO driver of your choice, SQL migrations embedded in the binary. Keep third-party dependencies to a minimum and justify each one.
- **Desktop:** Wails (current stable release; check the official docs for the current version and project layout rather than relying on memory) with a React + TypeScript + Vite frontend. Plain CSS with CSS variables; no UI component library, no Tailwind.
- **Deploy:** Docker Compose with two services: the API and Caddy as the TLS-terminating reverse proxy.

"Tracker" is a placeholder name. Keep it in one constant/config value so it is easy to rename.

## Product

One user has many projects. Each project has:

- **name** (short text)
- **description** (long plain text, multiple paragraphs)
- **log entries** — dated paragraphs I add whenever I make progress or want to record something; shown newest first
- **tasks** — a title and a done/not-done state
- **tags** — zero or more short labels such as "Mobile App" or "Freelance", used to filter the board

All text is plain text with line breaks preserved. No Markdown rendering, no attachments, no due dates, no sharing.

### Data model

- `users` (id, email, password_hash, created_at)
- `sessions` (id, user_id, token_hash, device_name, created_at, last_used_at, expires_at, revoked_at)
- `projects` (id, user_id, name, description, position, archived_at, created_at, updated_at)
- `log_entries` (id, project_id, body, created_at, updated_at)
- `tasks` (id, project_id, title, done, position, completed_at, created_at, updated_at)
- `tags` (id, user_id, name, created_at) — name unique per user, compared case-insensitively, stored as first typed
- `project_tags` (project_id, tag_id)
- `auth_events` (id, user_id nullable, kind, ip, created_at) — audit trail of logins, failures, lockouts, revocations

All ids are random UUIDs. Foreign keys enforced, cascading deletes from project to its entries, tasks and tag links. A tag no longer attached to any project is deleted automatically.

### API (JSON, all under `/v1`)

- `POST /auth/login` → session token; `POST /auth/logout`; `GET /auth/sessions`; `DELETE /auth/sessions/{id}`; `POST /auth/password` (change password, revokes every other session)
- `GET /projects` (returns each project with its tasks and its 2 most recent log entries, enough to draw the board in one request); `POST /projects`; `GET /projects/{id}` (full project: all tasks, all log entries); `PATCH /projects/{id}`; `DELETE /projects/{id}`; `PATCH /projects/order`
- Tags: every project in every response carries its `tags` as an array of names. `POST /projects` and `PATCH /projects/{id}` accept a `tags` array of names that replaces the project's whole tag set, creating tags that do not exist yet. `GET /tags` returns all of the user's tags with a project count each. `PATCH /tags/{id}` renames a tag everywhere. Filtering happens in the desktop app, not on the server.
- `POST /projects/{id}/log`; `PATCH /log/{id}`; `DELETE /log/{id}`
- `POST /projects/{id}/tasks`; `PATCH /tasks/{id}`; `DELETE /tasks/{id}`
- `GET /healthz` (unauthenticated, returns no version or build information)

There is **no registration endpoint**. The user is created on the server with a CLI subcommand of the API binary (`tracker-api user create`), which prompts for the password on the terminal without echo. Also provide `tracker-api user reset-password` and `tracker-api sessions revoke-all`.

## Security requirements (API)

**Transport**
- The API container listens only on the internal Docker network; only Caddy publishes ports 80/443. Caddy obtains certificates automatically, redirects HTTP to HTTPS, sends HSTS, and allows TLS 1.2+ only.
- The domain is supplied through an environment variable; use `tracker.example.com` as the placeholder in examples.

**Authentication**
- Passwords hashed with Argon2id using current OWASP-recommended parameters; minimum length 12; compare in constant time.
- Login returns an opaque session token: 256 bits from `crypto/rand`. Store only its SHA-256 hash in the database. No JWTs.
- Sessions expire after 30 days without use (sliding) and 180 days absolute. Every session is individually revocable. Changing the password revokes all other sessions.
- Token is sent as `Authorization: Bearer …`. No cookies, so no CSRF surface.
- Login failures return one generic error and take the same time whether or not the email exists (run a dummy hash for unknown emails).

**Brute-force and abuse protection**
- Login: at most 5 attempts per minute per IP and 10 per 15 minutes per account, then a lockout with exponential backoff. Record every failure and lockout in `auth_events`.
- Global rate limit per IP and per session for all other endpoints, returning 429 with `Retry-After`.
- Take the client IP from `X-Forwarded-For` only when the request comes from the Caddy container; otherwise use the socket address.

**Authorization**
- Every query is scoped by the authenticated `user_id` in the SQL itself, including child rows reached through a project. A resource that belongs to someone else returns 404, never 403.
- Even though there is one user today, write this as if there were many, and test it with two users.

**Input handling**
- Request bodies capped (64 KB default, 1 MB for description and log bodies); JSON decoded strictly (unknown fields rejected, single object only); `Content-Type` must be `application/json`.
- Length limits on every field (name 200, task title 500, description and log body 100,000 characters, tag name 40). At most 20 tags per project; tag names are trimmed, must not be empty, and must not contain control characters. Validate UUIDs before touching the database.
- Parameterised SQL only. No string-built queries anywhere.

**Server hardening**
- Set `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout`, `IdleTimeout` and `MaxHeaderBytes`; graceful shutdown.
- Response headers on everything: `X-Content-Type-Options: nosniff`, `Cache-Control: no-store`, `Referrer-Policy: no-referrer`, a deny-all `Content-Security-Policy`, `X-Frame-Options: DENY`. No CORS headers at all: the only client is the desktop app, never a browser.
- Errors returned to the client are generic; no stack traces, SQL errors or internal paths. Panics are recovered and logged.
- Structured logs with a request id. Never log passwords, tokens, `Authorization` headers or request bodies.
- Container: multi-stage build, minimal final image (distroless or scratch), runs as a non-root user, read-only root filesystem, all capabilities dropped, `no-new-privileges`, database on a named volume with 0600 permissions.
- Configuration through environment variables only; nothing secret in the repository; provide `.env.example`.

**Verification**
- Tests must cover: login success and failure, lockout, expired and revoked tokens, a second user being unable to read, change or attach the first user's projects, entries, tasks and tags (every endpoint), oversized bodies, unknown fields and malformed ids.
- Add `make check` that runs `go vet`, the tests with the race detector, `govulncheck` and `gosec`. It must pass before you call any step done.
- When the API is finished, review it against the OWASP API Security Top 10 and write the result, including anything you could not fully address, to `SECURITY.md`.

## Security requirements (desktop)

- All HTTP calls to the API are made from the **Go side** of the Wails app and exposed to the frontend as bound methods. The session token never reaches the webview's JavaScript.
- Store the session token in the OS keychain (macOS Keychain, Linux Secret Service). Never write it to a plain file or to localStorage. The server address goes in the normal app config file.
- Refuse any server address that is not `https://` (allow `http://localhost` only in dev builds). Use the system certificate store; never disable certificate verification.
- The webview loads only bundled local assets: fonts are bundled with the app, no CDN or remote requests of any kind from the frontend. Set a strict CSP for the webview.
- On a 401, clear the stored token and return to the Connect screen.

## Desktop app behaviour

**Connect screen** (first run, or after sign-out): app name, the line "Connect this computer to your server. You only do this once per device.", fields for server address, email and password, and a Connect button. Centred, 380 px wide. The device name sent with login defaults to the machine's hostname.

**Board (main screen)**
- Top bar: app name at left, then a sync indicator (small dot plus "Synced · <host>", or "Offline" / "Syncing…"); a primary "New project" button at right.
- Tag filter bar, directly under the top bar and hidden when no tags exist: one chip per tag in use, sorted by name, wrapping onto more lines if needed. Clicking a chip toggles it. With one or more chips selected, the board shows only projects that have **at least one** of the selected tags; a "Clear" text button appears at the end of the row. The selection is remembered on this device between launches. If the filter hides every project, show "No projects with these tags." in place of the columns.
- Below it, one column per project, 340 px wide, in a horizontally scrolling row. Each column scrolls vertically on its own and contains, in order: the project name (click opens the project page), a count line such as "4 open · 1 done", the project's tags as small chips (clicking one selects that tag in the filter bar), the description clamped to 4 lines, a "Tasks" section (checkbox rows, then an "Add a task…" input that adds on Enter), a "Log" section showing the 2 most recent entries (date, text clamped to 3 lines), and pinned at the bottom a "Write log entry" button that expands an inline composer in the column (Cmd/Ctrl+Enter saves, Esc cancels).
- After the last column, a dashed ghost column "New project": clicking it (or the top-bar button, or Cmd/Ctrl+N) turns it into a name input; Enter creates the project.
- Columns can be reordered by dragging the header.

**Project page**
- Top bar: "← All projects" at left (Esc also goes back), sync indicator at right.
- Content max-width 1240 px, two panes. Left: project name as a large heading with an "Edit description" button, a row of the project's tags (each chip has a small × to remove it, followed by an "Add tag" input that adds on Enter or comma and suggests existing tags as I type), the full description, then the "Log" section: a composer card (textarea, "Add entry" button) followed by every entry, newest first, each with its date in a narrow left column. Right (340 px): a "Tasks" card with the count line, an add-task input with a + button, open tasks first and done tasks after.
- Name, description, task titles and log entries are edited in place. Delete actions for a task, a log entry and the project itself live in a small "…" menu and ask for confirmation. Projects can be archived instead of deleted; a small "Archived" link on the board lists them and can restore them.

**Data and sync**
- Online-only for now: no offline write queue. Updates are optimistic and roll back with a small inline error if the request fails. Refetch when the window regains focus and every 60 seconds. If the server is unreachable, show "Offline", keep the last loaded data visible and disable editing.
- A small settings popover (from the sync indicator) shows the server address, the list of signed-in devices with a revoke button each, change password, and sign out.

## Visual design

Dark mode only. Quiet, flat, no gradients, no shadows, no emoji. Define these as CSS variables and use nothing outside them:

| Role | Value |
| --- | --- |
| App background | `#0D0E10` |
| Surface (columns, cards) | `#15171A` |
| Input background | `#1B1E22` |
| Border | `#272B31` |
| Strong border (ghost buttons, timeline rule) | `#30353C` |
| Dashed border (new-project column) | `#3A4048` |
| Empty checkbox border | `#6B727C` |
| Text | `#E7E9EC` |
| Body text, softer | `#C9CED5` |
| Secondary text | `#A3A9B2` |
| Labels, dates, done tasks | `#8A919B` |
| Accent (primary buttons, checked boxes, sync dot, focus ring) | `#E9B44C` |
| Text on accent | `#0D0E10` |

- **Type:** Inter for UI and body; Space Grotesk (600) for the app name, project names and page headings; JetBrains Mono for dates, counts and the server address. Bundle all three locally.
- **Sizes:** base 14 px; project name in a column 18 px, on the project page 32 px; description on the board 13 px / 1.55, on the project page 15 px / 1.65 with a 68-character measure; section labels ("Tasks", "Log") 11 px, uppercase, 0.08em tracking, weight 600; dates and counts 12 px mono.
- **Shape:** columns and cards have a 1 px border and 10 px radius; inputs and ghost buttons 6 px; primary buttons 8 px. Column padding 16–18 px, 16 px gap between columns, 20/24 px board padding. Top bar has a 1 px bottom border.
- **Tasks:** each row is a real `<button>`, at least 40 px tall, with a 16 px checkbox (4 px radius). Not done: 1.5 px outline, no fill. Done: accent fill with a dark check mark, and the title in the label grey with a line through it.
- **Tags:** chips are 12 px Inter, weight 500, 24 px tall on a project and 30 px tall in the filter bar, 6 px radius, 1 px strong border, transparent background, secondary text colour. A selected filter chip has the accent fill with the dark text-on-accent colour and no border. The filter bar has 12/24 px padding and a 1 px bottom border, with 8 px between chips.
- **Log entries on the board:** date above the text, with a 1 px rule on the left of the entry. On the project page: date in a 72 px left column, text beside it.
- **Buttons:** primary = accent fill, dark text, weight 600. Secondary = transparent with a strong border. Icons are simple inline stroke SVGs (plus, check, back arrow, pencil).
- **Accessibility:** real buttons, links, labels and inputs throughout; visible 2 px accent focus ring; everything reachable by keyboard.

## Deliverables

- `api/`, `desktop/`, `deploy/` (Compose file, Caddyfile, `.env.example`), a root `Makefile`, `README.md` and `SECURITY.md`.
- README covers: deploying to a fresh Linux server step by step (DNS, `.env`, `docker compose up`, creating the user), backing up and restoring the SQLite file, building the desktop app on macOS (Apple Silicon) and on Linux, and local development.
- `make` targets: `check`, `api-run`, `desktop-dev`, `desktop-build`.

## How to work

- Work in small commits, one logical step each.
- Do not add features, endpoints, dependencies or settings beyond this document. If something here is ambiguous or you think a requirement is wrong, stop and ask me rather than guessing.
- Never weaken a security requirement to make something easier; tell me about the conflict instead.
