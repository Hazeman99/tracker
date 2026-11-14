# Tracker

A minimal, single-user project tracker:

- **`api/`** — Go API (standard library router, SQLite in WAL mode), deployed on
  a Linux server behind Caddy, reachable only over Tailscale.
- **`desktop/`** — Wails v3 desktop app (React + TypeScript + Vite frontend) for
  macOS (Apple Silicon) and Linux. All network calls are made from Go; the
  session token lives in the OS keychain.
- **`deploy/`** — Docker Compose (API + Caddy), Caddyfile, `.env.example`,
  `renew-cert.sh` (Tailscale certificate).

"Tracker" is a placeholder name. To rename the app, change:
`api/internal/app/app.go` (`Name`, and `EnvPrefix` if you want different
variable names), `desktop/internal/brand/brand.go` (`Name`; the UI reads it from
Go), and the product fields in `desktop/build/config.yml`, the macOS
`Info*.plist` and the Linux packaging files.

See [SECURITY.md](SECURITY.md) for the security design and the OWASP API Top 10
review.

---

## 1. Deploying the API on a Linux server (Tailscale only)

The API is published **only on the server's Tailscale address**: it is
reachable from devices on your tailnet and from nowhere else (not the public
internet, not the LAN). TLS uses a real Let's Encrypt certificate for the
server's MagicDNS name, obtained through Tailscale.

The examples use the MagicDNS name `myserver.tailnet-name.ts.net`, the Tailscale
IP `100.64.0.1` and port `8443`; replace them with yours.

Requirements: a Linux server running Tailscale and Docker Engine 28+ with the
Compose plugin.

1. **Enable HTTPS certificates in Tailscale.** In the Tailscale admin console →
   DNS, enable **MagicDNS** and **HTTPS Certificates**. Note that certificate
   names are published in public Certificate Transparency logs, which reveals
   the machine and tailnet name (not its address).

2. **Find the name and address** on the server:

   ```sh
   tailscale status --self --json | grep -m1 DNSName   # myserver.tailnet-name.ts.net.
   tailscale ip -4                                     # 100.64.0.1
   ```

3. **Let Docker bind the Tailscale address at boot**, even if Docker starts
   before Tailscale has brought the address up:

   ```sh
   echo 'net.ipv4.ip_nonlocal_bind = 1' | sudo tee /etc/sysctl.d/90-tracker.conf
   sudo sysctl --system
   ```

4. **Install Docker** if needed (`curl -fsSL https://get.docker.com | sh`, or
   https://docs.docker.com/engine/install/).

5. **Get the code**: `git clone <your repo> tracker && cd tracker/deploy`.

6. **Configure.**

   ```sh
   cp .env.example .env
   # edit .env: TRACKER_DOMAIN (MagicDNS name, without the trailing dot),
   # TRACKER_BIND_IP (Tailscale IPv4), TRACKER_HTTPS_PORT (8443 unless free 443)
   ```

   Port 443 on the Tailscale address is often already used by
   `tailscale serve`/`funnel`; check with `sudo ss -ltnp 'sport = :443'`.

7. **Fetch the certificate** (the host does this, so the Caddy container never
   gets access to the Tailscale daemon):

   ```sh
   sudo ./renew-cert.sh
   ```

   Then renew it automatically. Tailscale certificates last 90 days and
   `tailscale cert` only fetches a new one when it is close to expiry; the
   script restarts Caddy when the file changes. Add to root's crontab
   (`sudo crontab -e`), adjusting the path:

   ```
   17 4 * * 1  /home/you/tracker/deploy/renew-cert.sh
   ```

8. **Start.**

   ```sh
   sudo docker compose up -d --build
   curl https://myserver.tailnet-name.ts.net:8443/v1/healthz   # → {"status":"ok"}, from any tailnet device
   ```

   `http://myserver.tailnet-name.ts.net` (port 80) redirects to the HTTPS port.

9. **Create your user.** There is no registration endpoint. The password is
   read from the terminal without echo (minimum 12 characters):

   ```sh
   sudo docker compose exec -it api /tracker-api user create --email you@example.com
   ```

In the desktop app, the server address is
`https://myserver.tailnet-name.ts.net:8443`. The computer must be on the tailnet.

Other admin commands (run from `deploy/`):

```sh
# Set a new password; signs out every device.
docker compose exec -it api /tracker-api user reset-password --email you@example.com
# Revoke every session of every user (asks for confirmation).
docker compose exec -it api /tracker-api sessions revoke-all
```

**Updating:** `git pull && sudo docker compose up -d --build` in `deploy/`.
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

Enter `https://myserver.tailnet-name.ts.net:8443`, your email and password on
the Connect screen (the computer must be connected to your tailnet). Each device appears under **Signed-in devices** in the settings popover
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
| `TRACKER_DOMAIN` | — | Compose/Caddy only: the server's MagicDNS name (`*.ts.net`) |
| `TRACKER_BIND_IP` | — | Compose only: the Tailscale IPv4 address ports are published on |
| `TRACKER_HTTPS_PORT` | `8443` | Compose/Caddy only: HTTPS port |

### Repository layout

```
api/
  cmd/tracker-api/       entry point (serve + admin CLI)
  internal/auth          Argon2id, tokens, ids
  internal/db            SQLite open + embedded migrations
  internal/store         data access (every query scoped by user)
  internal/server        HTTP handlers, middleware, tests
  internal/ratelimit     token buckets, login lockout
deploy/                  compose.yaml, Caddyfile, .env.example, renew-cert.sh
desktop/
  main.go                Wails app, CSP headers
  internal/tracker       service bound to the frontend
  internal/api           HTTP client for the API
  internal/secrets       OS keychain
  internal/serverurl     server address validation (https only)
  frontend/src           React UI
```
