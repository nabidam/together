# Operating Together

This guide deploys a released Together binary on Debian or Ubuntu. The same model works on other systemd-based Linux distributions with equivalent package names.

## Requirements

- A supported `linux/amd64` or `linux/arm64` release archive.
- A domain name pointing to the server.
- Caddy for HTTPS and reverse proxying.
- `ffmpeg` and `ffprobe` for upload processing.
- Optional: `sqlite3` and `restic` for the supplied database-backup timer.

Together is a single static binary with its web app embedded. Node.js, npm, Go, and source files are not needed at runtime.

## Install

```sh
sudo apt update
sudo apt install -y caddy ffmpeg

# Verify the release you downloaded, then install its one binary.
sha256sum --check SHA256SUMS
tar -xzf together_linux_amd64.tar.gz
sudo install -m 0755 together /usr/local/bin/together

sudo groupadd --system together
sudo useradd --system --gid together --home /var/lib/together --create-home --shell /usr/sbin/nologin together
sudo install -d -o together -g together -m 0750 /var/lib/together
```

Create `/etc/together.env` with a first-boot admin password. The values are read only when the account is initially seeded; keep the file private afterward.

```sh
sudo sh -c 'cat > /etc/together.env <<EOF
ADMIN_USER=admin
ADMIN_PASS=replace-with-a-unique-long-password
EOF'
sudo chown root:together /etc/together.env
sudo chmod 0640 /etc/together.env
```

Install and start the service:

```sh
sudo install -m 0644 deploy/together.service /etc/systemd/system/together.service
sudo systemctl daemon-reload
sudo systemctl enable --now together
sudo systemctl status together
curl --noproxy '*' http://127.0.0.1:8080/healthz
```

## Caddy reverse proxy and TLS

Copy `deploy/Caddyfile` into your Caddy configuration, replace `together.example.com` with your domain, then validate and reload it:

```sh
sudo install -m 0644 deploy/Caddyfile /etc/caddy/Caddyfile
sudoedit /etc/caddy/Caddyfile
sudo caddy validate --config /etc/caddy/Caddyfile
sudo systemctl reload caddy
```

Caddy obtains and renews HTTPS certificates automatically when public DNS for the domain points at the server and ports 80 and 443 are reachable. Caddy proxies WebSockets without extra configuration. Leave Together bound to `127.0.0.1:8080`; expose only Caddy publicly.

## Firewall

Allow SSH before enabling a firewall, then allow HTTP/HTTPS only:

```sh
sudo ufw allow OpenSSH
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp
sudo ufw enable
```

Do not allow port 8080 from the network.

## Backups and restore

The supplied restic timer backs up the SQLite database only. It does **not** back up uploaded or processed media, so configure separate durable storage or backup for `/var/lib/together/media` before relying on it for irreplaceable files.

```sh
sudo apt install -y sqlite3 restic
sudo install -m 0755 deploy/backup.sh /usr/local/bin/together-backup.sh
sudo install -m 0644 deploy/together-backup.service /etc/systemd/system/
sudo install -m 0644 deploy/together-backup.timer /etc/systemd/system/
sudo sh -c 'umask 077; head -c 32 /dev/urandom | base64 > /etc/together-restic.pass'
sudo chown together:together /etc/together-restic.pass
sudo chmod 0600 /etc/together-restic.pass
sudo install -d -o together -g together -m 0700 /var/backups/together
sudo systemctl daemon-reload
sudo systemctl enable --now together-backup.timer
```

Test the timer with `sudo systemctl start together-backup.service`. To restore a database, first stop Together and make a copy of the current database. Then restore a tagged snapshot and copy the restored `together.db` into place:

```sh
sudo systemctl stop together
sudo cp -a /var/lib/together/together.db /var/lib/together/together.db.before-restore
sudo -u together env \
  RESTIC_REPOSITORY=/var/backups/together \
  RESTIC_PASSWORD_FILE=/etc/together-restic.pass \
  restic restore latest --tag together-db --target /var/tmp/together-restore
sudo install -o together -g together -m 0600 \
  /var/tmp/together-restore/together.db \
  /var/lib/together/together.db
sudo systemctl start together
```

Confirm the service starts before deleting `together.db.before-restore`. If your restic repository or backup path differs, replace it in the restore command.

## Upgrade and rollback

1. Back up the database and media.
2. Download and verify the new release archive.
3. Stop the service, replace `/usr/local/bin/together`, then start it.
4. Check `systemctl status together`, `journalctl -u together`, and `/healthz`.

Rooms and chat are intentionally in-memory and vanish on every process restart. A binary rollback does not roll back database schema; take a database backup before upgrading.

```sh
sudo systemctl stop together
sudo install -m 0755 together /usr/local/bin/together
sudo systemctl start together
```

## Troubleshooting

- `ffprobe` or `ffmpeg` errors: install the `ffmpeg` package and check `journalctl -u together`.
- The service will not start: verify `/etc/together.env` permissions, ownership, and `TOGETHER_DATA` write access.
- HTTPS does not issue: confirm public DNS and that ports 80/443 reach Caddy.
- A room disappears after restart: expected; create a new room after the server returns.
