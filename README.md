# Tracker

A minimal, single-user project tracker:

- **`api/`** — Go API (standard library router, SQLite in WAL mode), deployed on
  a public Linux server behind Caddy.
- **`desktop/`** — Wails v3 desktop app (React + TypeScript + Vite frontend) for
  macOS (Apple Silicon) and Linux. All network calls are made from Go; the
  session token lives in the OS keychain.
- **`deploy/`** — Docker Compose (API + Caddy), Caddyfile, `.env.example`.

"Tracker" is a placeholder name. To rename the app, change:
`api/internal/app/app.go` (`Name`, and `EnvPrefix` if you want different
variable names), `desktop/internal/brand/brand.go` (`Name`; the UI reads it from
Go), and the product fields in `desktop/build/config.yml`, the macOS
`Info*.plist` and the Linux packaging files.

See [SECURITY.md](SECURITY.md) for the security design and the OWASP API Top 10
review.

---

## 1. Deploying the API to a fresh Linux server

Requirements: a Linux server with a public IP, Docker Engine with the Compose
plugin, and a domain name you control. `tracker.example.com` is used below;
replace it with yours.

1. **DNS.** Create an `A` record (and an `AAAA` record if the server has IPv6)
   for `tracker.example.com` pointing at the server. Wait until
   `dig +short tracker.example.com` returns the server's address. Caddy needs
   this to obtain a certificate.

2. **Firewall.** Allow inbound TCP 80 and 443 (and UDP 443 for HTTP/3). Nothing
   else needs to be open; the API port is never published. For example, with
   ufw:

   ```sh
   sudo ufw allow 22/tcp && sudo ufw allow 80/tcp && sudo ufw allow 443/tcp && sudo ufw allow 443/udp
   sudo ufw enable
   ```

3. **Install Docker** (Debian/Ubuntu):

   ```sh
   curl -fsSL https://get.docker.com | sh
   ```

   (or follow https://docs.docker.com/engine/install/ for your distribution).

4. **Get the code** onto the server, e.g. `git clone <your repo> tracker && cd tracker`.

5. **Configure.**

   ```sh
   cd deploy
   cp .env.example .env
   # edit .env and set TRACKER_DOMAIN=tracker.example.com
   ```

6. **Start.**

   ```sh
   docker compose up -d --build
   docker compose logs -f caddy   # watch the certificate being issued, then Ctrl-C
   curl https://tracker.example.com/v1/healthz   # → {"status":"ok"}
   ```

7. **Create your user.** There is no registration endpoint. The password is
   read from the terminal without echo (minimum 12 characters):

   ```sh
   docker compose exec -it api /tracker-api user create --email you@example.com
   ```

Other admin commands (run from `deploy/`):

```sh
# Set a new password; signs out every device.
docker compose exec -it api /tracker-api user reset-password --email you@example.com
# Revoke every session of every user (asks for confirmation).
docker compose exec -it api /tracker-api sessions revoke-all
```

**Updating:** `git pull && docker compose up -d --build` in `deploy/`.
Migrations run automatically at start-up.

## 2. Backing up and restoring the database

The database is the file `tracker.db` (plus its `-wal`/`-shm` companions) in
the Docker volume `tracker_tracker-data`. It contains password and token
*hashes*; store backups somewhere private.

**Backup** (stops the API for a second so the copy is consistent):

```sh
cd deploy
docker compose stop api
docker run --rm -v tracker_tracker-data:/data:ro -v "$PWD":/backup alpine \
  tar czf /backup/tracker-$(date +%F).tar.gz -C /data .
docker compose start api
```

**Restore:**

```sh
cd deploy
docker compose stop api
docker run --rm -v tracker_tracker-data:/data -v "$PWD":/backup alpine \
  sh -c 'rm -f /data/tracker.db /data/tracker.db-wal /data/tracker.db-shm &&
         tar xzf /backup/tracker-YYYY-MM-DD.tar.gz -C /data &&
         chown -R 65532:65532 /data && chmod 700 /data && chmod 600 /data/tracker.db*'
docker compose start api
```

Automate the backup with cron (e.g. nightly) and copy the archives off the
server.

## 3. Building the desktop app

Common prerequisites: **Go 1.27** (the `toolchain` line in `go.mod` downloads it
automatically if you have an older Go), **Node.js 20+** with npm, and the Wails
CLI:

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0
export PATH="$(go env GOPATH)/bin:$PATH"
```

### macOS (Apple Silicon)

1. Install the Xcode command line tools: `xcode-select --install`.
2. From the repository root:

   ```sh
   make desktop-build
   ```

3. The app is `desktop/bin/tracker.app`. Move it to `/Applications`. It is
   ad-hoc signed, so the first time, right-click it and choose **Open**.

### Linux

1. Install the build dependencies (GTK 4.10+ and WebKitGTK 6; Debian 12's GTK 4.8 is too old):

   - Debian 13+/Ubuntu 24.04+: `sudo apt install build-essential pkg-config libgtk-4-dev libwebkitgtk-6.0-dev`
   - Fedora: `sudo dnf install gcc pkgconf-pkg-config gtk4-devel webkitgtk6.0-devel`

2. Make sure a Secret Service provider is running (GNOME Keyring is the
   default on GNOME; KWallet on KDE). The app stores its session there and
   refuses to sign in without one.
3. From the repository root:

   ```sh
   make desktop-build
   ```

4. The binary is `desktop/bin/tracker`. To produce a `.deb` or `.rpm` instead,
   run `cd desktop && wails3 task linux:create:deb` (or `linux:create:rpm`).

### First run

Enter `https://tracker.example.com`, your email and password on the Connect
screen. Each device appears under **Signed-in devices** in the settings popover
(click the sync indicator), where it can be revoked.

## 4. Local development

```sh
# Terminal 1 — API on http://127.0.0.1:8080 with api/dev.db
make api-run

# Create a local user (in another terminal)
cd api && TRACKER_DB_PATH=./dev.db go run ./cmd/tracker-api user create --email me@example.com

# Terminal 2 — desktop app with hot reload. Dev builds (build tag `dev`)
# also accept http://localhost / http://127.0.0.1 server addresses.
make desktop-dev
```

Then connect the app to `http://127.0.0.1:8080`.

Checks (must pass before anything is considered done):

```sh
make check                         # vet, race tests, govulncheck, gosec, frontend type check
make check VULNCHECK_IN_DOCKER=1   # if vuln.go.dev is blocked on your network
```

Other targets: `make api-build` (static API binary in `api/bin/`),
`make desktop-frontend` (generate bindings and build the frontend only).

### Configuration reference (API)

| Variable | Default | Meaning |
| --- | --- | --- |
| `TRACKER_LISTEN_ADDR` | `:8080` | Listen address |
| `TRACKER_DB_PATH` | `/data/tracker.db` | SQLite database file |
| `TRACKER_TRUSTED_PROXY` | *(none)* | IP of the reverse proxy allowed to set `X-Forwarded-For` (set to Caddy's fixed address by `deploy/compose.yaml`) |
| `TRACKER_DOMAIN` | — | Compose/Caddy only: the public domain |

### Repository layout

```
api/
  cmd/tracker-api/       entry point (serve + admin CLI)
  internal/auth          Argon2id, tokens, ids
  internal/db            SQLite open + embedded migrations
  internal/store         data access (every query scoped by user)
  internal/server        HTTP handlers, middleware, tests
  internal/ratelimit     token buckets, login lockout
deploy/                  compose.yaml, Caddyfile, .env.example
desktop/
  main.go                Wails app, CSP headers
  internal/tracker       service bound to the frontend
  internal/api           HTTP client for the API
  internal/secrets       OS keychain
  internal/serverurl     server address validation (https only)
  frontend/src           React UI
```
