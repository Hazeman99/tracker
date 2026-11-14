#!/bin/sh
# Fetches (or renews) the Tailscale TLS certificate for TRACKER_DOMAIN into
# deploy/certs/ and restarts Caddy if the certificate changed.
# Run as root: once before the first `docker compose up`, then weekly from cron.
set -eu
cd "$(dirname "$0")"

domain=$(sed -n 's/^TRACKER_DOMAIN=//p' .env | tail -n 1)
case "$domain" in
  *.ts.net) ;;
  *) echo "TRACKER_DOMAIN in deploy/.env must be this machine's *.ts.net name" >&2; exit 1 ;;
esac

umask 077
mkdir -p certs
before=$(cat certs/cert.pem 2>/dev/null | sha256sum)
tailscale cert --cert-file certs/cert.pem --key-file certs/key.pem "$domain"
chmod 600 certs/cert.pem certs/key.pem
after=$(sha256sum < certs/cert.pem)

if [ "$before" != "$after" ] && docker compose ps --status running --services 2>/dev/null | grep -qx caddy; then
  docker compose restart caddy
fi
