# Operating Together

This guide deploys a released Together binary on Debian or Ubuntu. The same model works on other systemd-based Linux distributions with equivalent package names.

## Requirements

- A supported `linux/amd64` or `linux/arm64` release archive.
- A domain name pointing to the server (for Caddy or Nginx production deployments).
- One reverse proxy: Caddy **or** Nginx for HTTPS and reverse proxying.
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

Create `/etc/together.env` with first-boot administrator credentials. A new database refuses to start unless `ADMIN_USER` is present and `ADMIN_PASS` has at least 12 Unicode code points. Once any user exists, seed values are ignored; keep the file private afterward. Set an upload ceiling that leaves adequate disk headroom (the default is 20 GiB per upload).

```sh
sudo sh -c 'cat > /etc/together.env <<EOF
ADMIN_USER=admin
ADMIN_PASS=replace-with-a-unique-long-password
TOGETHER_MAX_UPLOAD_BYTES=21474836480
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

## Reverse proxy and TLS

Choose exactly one production proxy. Both supported configurations preserve WebSocket upgrades and forward the client IP. Keep Together bound to `127.0.0.1:8080`; expose only the proxy publicly.

### Option A: Caddy

Copy `deploy/Caddyfile` into your Caddy configuration, replace `together.example.com` with your domain, then validate and reload it:

```sh
sudo install -m 0644 deploy/Caddyfile /etc/caddy/Caddyfile
sudoedit /etc/caddy/Caddyfile
sudo caddy validate --config /etc/caddy/Caddyfile
sudo systemctl reload caddy
```

Caddy obtains and renews HTTPS certificates automatically when public DNS for the domain points at the server and ports 80 and 443 are reachable. Caddy proxies WebSockets without extra configuration. Leave Together bound to `127.0.0.1:8080`; expose only Caddy publicly.

### Option B: Nginx

Install Nginx, Certbot, and the application dependency instead of Caddy:

```sh
sudo apt update
sudo apt install -y nginx certbot python3-certbot-nginx ffmpeg
```

Create the certificate before loading the supplied TLS configuration. Certbot's Nginx plugin needs a temporary HTTP server block for the domain; follow its interactive flow, then replace the resulting site configuration with `deploy/nginx.conf`.

```sh
sudo certbot --nginx -d together.example.com
sudo install -m 0644 deploy/nginx.conf /etc/nginx/sites-available/together
sudoedit /etc/nginx/sites-available/together
sudo ln -s /etc/nginx/sites-available/together /etc/nginx/sites-enabled/together
sudo rm /etc/nginx/sites-enabled/default
sudo nginx -t
sudo systemctl reload nginx
```

Replace `together.example.com` in both `server_name` directives and certificate paths. Set `client_max_body_size` to at least `TOGETHER_MAX_UPLOAD_BYTES`; the provided file matches the 20 GiB default. Certbot renewals continue using its Nginx authenticator; test them with `sudo certbot renew --dry-run`.

Together uses the socket peer IP for login and registration rate limits. It accepts the first valid `X-Forwarded-For` value only from a loopback peer, so this loopback-only proxy topology is required; do not put an untrusted local proxy in front of the application.

## Local sharing with ngrok

ngrok is a temporary local-serving option, not a production host. It gives a public HTTPS URL to a locally running Together process without opening inbound firewall ports. Install the ngrok agent, authenticate it to your ngrok account, then run:

```sh
ADMIN_USER=admin ADMIN_PASS='choose-a-password-of-at-least-12-characters' together
ngrok http 8080
```

Share the HTTPS URL printed by ngrok. Keep both processes running; the URL stops working when the tunnel stops. Treat it as public: Together's invite gate and strong admin credentials are still required. The current free ngrok plan has limited transfer and request allowances, so it is unsuitable for uploaded media or sustained use; see [ngrok's tunnel guide](https://ngrok.com/docs/guides/share-localhost/tunnels) and [current pricing](https://ngrok.com/pricing).

## Free hosting assessment

Together needs an always-running Linux process, a writable persistent filesystem for its SQLite database and media, WebSockets, and `ffmpeg`/`ffprobe`. That makes conventional serverless free tiers a poor fit: local files disappear on restart, deploy, or scale-to-zero, and the binary cannot safely use SQLite there.

**Recommended free-tier deployment: Oracle Cloud Infrastructure (OCI) Always Free VM.** This is a small VM rather than serverless, but it is the viable no-cost SaaS-hosted option for the unmodified binary. OCI's Always Free allocation currently includes up to 2 Arm OCPUs and 12 GB RAM for Ampere A1 compute, plus 200 GB of combined boot/block-volume storage. Use the `linux/arm64` release, mount the persistent volume at `/var/lib/together`, and follow this guide with Caddy or Nginx. Verify capacity in the selected home region before committing to it: free Arm capacity can be temporarily unavailable. See the [OCI Free Tier](https://docs.oracle.com/en-us/iaas/Content/FreeTier/freetier.htm) and [Always Free resources](https://docs.oracle.com/en-us/iaas/Content/FreeTier/freetier_topic-Always_Free_Resources.htm).

The conclusion was checked on 2026-07-18:

| Platform | Free compute | Persistent local disk on free tier | Fit |
|---|---|---|---|
| OCI Always Free VM | Yes | Yes, within 200 GB combined volume allowance | Recommended; VM, not serverless |
| Render | Yes | No; free web services lose local files on restart/deploy/spin-down | Not suitable |
| Koyeb | Yes | No; volumes cannot attach to Free instances | Not suitable |

Do not substitute a managed Postgres service for SQLite without an application migration. Similarly, object storage is useful for backups but is not a POSIX volume and cannot safely host SQLite's database/WAL files. Re-check providers and quotas before deployment; free-tier terms change.

## Upload capacity

`TOGETHER_MAX_UPLOAD_BYTES` is a positive byte count enforced for new and resumed uploads, with a default of `21474836480` (20 GiB). It is a per-upload boundary, not a disk quota: reserve space for both the original and processed media. The server also rejects creation JSON larger than 4 KiB, upload chunks over 8 MiB, and subtitle bodies over 10 MiB. Clients must declare the total at creation and send the same `Upload-Length` on each resumable chunk; a `413` means a request body is too large, while `409` means offset, declared total, or final size does not match.

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
- The service will not start on first boot: verify `/etc/together.env` permissions, ownership, `TOGETHER_DATA` write access, and that `ADMIN_USER` plus a 12-code-point-or-longer `ADMIN_PASS` are present. Check `TOGETHER_MAX_UPLOAD_BYTES` is a positive integer if it is set.
- HTTPS does not issue: confirm public DNS and that ports 80/443 reach Caddy or Nginx.
- A room disappears after restart: expected; create a new room after the server returns.
