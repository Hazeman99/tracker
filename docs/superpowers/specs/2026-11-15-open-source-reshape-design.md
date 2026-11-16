# Open-source reshape — design

Date: 2026-11-15. Approved decisions: MIT license, Caddy automatic HTTPS
(with a self-signed option), publish the API image to ghcr.io, desktop
release targets stay macOS (Apple Silicon) + Linux.

## Goal

Turn the repo into a publishable open-source project: MIT license,
contributor-oriented README, GitHub Actions CI + tag-driven releases for the
API and the desktop app, no Tailscale dependency in the deployment, and an
explicit per-user option on the desktop Connect screen to disable TLS
certificate verification (for self-signed servers).

## 1. License

`LICENSE` at the repo root: MIT, `Copyright (c) 2026 Hazem Hadi`.
README gains a License section and badge.

## 2. Desktop: "disable SSL verification" option

- `desktop/internal/api`: `New(base string, insecureTLS bool)`. When set,
  the transport uses `InsecureSkipVerify: true` (annotated `#nosec G402` —
  deliberate, user-chosen, off by default). TLS 1.2+ and the
  no-redirect policy are unchanged. The certificate-error message now points
  at the new option.
- `desktop/internal/config`: `Config.InsecureTLS bool` (`insecure_tls`),
  saved next to server + email so the session restore after restart uses it.
- `desktop/internal/tracker`: `Connect(ctx, server, email, password,
  insecureTLS)`; `Status` gains `insecure_tls` so the form is prefilled;
  the keychain-restore path in `New` passes the saved flag; `newAPI`
  signature updated (and in tests).
- `desktop/frontend`: checkbox on the Connect screen, default from
  `status.insecure_tls`, labelled "Disable SSL certificate verification"
  with a warning line. `api.connect` passes it through.
- `serverurl` stays https-only in release builds: the option skips
  *verification*, it does not allow plaintext.

## 3. Deploy without Tailscale

- Delete `deploy/renew-cert.sh`; drop the `certs/` mount.
- `deploy/.env.example`: `TRACKER_DOMAIN` (any DNS name),
  `TRACKER_TLS` = ACME e-mail address (Let's Encrypt, public servers) or
  `internal` (Caddy's self-signed CA, LAN servers — pairs with the new
  desktop option), `TRACKER_BIND_IP` (optional, default `0.0.0.0`),
  `TRACKER_HTTPS_PORT` (default 443).
- `deploy/Caddyfile`: `tls {$TRACKER_TLS:internal}`; certificates are
  stored in the existing `caddy-data` volume. Explicit HTTP→HTTPS redirect
  kept (correct for non-443 ports; ACME HTTP-01 is handled by Caddy before
  site routes). Headers, body cap, reverse_proxy unchanged.
- `deploy/compose.yaml`: ports bound to `${TRACKER_BIND_IP:-0.0.0.0}`,
  HTTPS port defaults to 443; container hardening (read-only, cap_drop,
  internal backend network, distroless API) unchanged. A commented `image:`
  line points at `ghcr.io/hazeman99/tracker-api` as an alternative to
  building locally.

## 4. GitHub Actions

- `.github/workflows/ci.yml` (pushes to main + PRs): an `api` job
  (vet, race tests, govulncheck, gosec) and a `desktop` job on ubuntu-24.04
  (GTK4/WebKitGTK deps, wails3 bindings, npm build = type check, vet, race
  tests incl. the `dev`-tag serverurl test, govulncheck, gosec) — the same
  steps as `make check`.
- `.github/workflows/release.yml` (tags `v*`):
  - API binaries: linux amd64 + arm64, CGO-free, tar.gz.
  - API image: multi-arch (amd64+arm64) to `ghcr.io/<owner>/tracker-api`,
    tagged with the version and `latest`.
  - Desktop macOS: `wails3 package` on an arm64 macOS runner, zipped with
    `ditto` (ad-hoc signed; README documents right-click → Open).
  - Desktop Linux: ubuntu-24.04 + ubuntu-24.04-arm, `wails3 build` then the
    `linux:create:deb`/`linux:create:rpm` tasks: a .deb and .rpm per arch
    (plus the bare binary tar.gz).
  - Final job: SHA256SUMS + `gh release create --generate-notes` with all
    artifacts.

## 5. README rewrite

Open-source shape: pitch, features, badges; Install (from Releases);
Self-hosting quickstart (compose + Caddy auto-HTTPS, self-signed option,
user creation, backup/restore, updating); Building from source;
Development; Configuration reference (Tailscale rows replaced by
`TRACKER_TLS`); repository layout; Security link; Contributing; License.

## 6. SECURITY.md updates

Replace Tailscale-specific text: threat model assumes public exposure (it
already did by design); API8 TLS bullet describes Caddy ACME / `tls
internal`; the desktop section documents the opt-in skip-verify flag; the
"Tailscale-specific" limitation is replaced by notes on public exposure and
a new limitation describing what disabling certificate verification gives
up (MITM protection) and the safer alternative (trusting the CA cert).

## Testing

- `api`: unchanged, full `make api-check` locally.
- `desktop`: unit tests for the new config field, client TLS behavior
  (self-signed httptest server: fails closed by default, succeeds with the
  flag), service pass-through; run locally (no GTK needed for the internal
  packages). Full desktop build + frontend type check verified by CI.
