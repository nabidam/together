# Hardening Together

Use this checklist before making an instance public. It complements, rather than replaces, operating-system patching and a secure SSH policy.

## Network and TLS

- Run Together only on `127.0.0.1:8080`; put Caddy or Nginx in front of it for TLS.
- Permit inbound 80/443 and SSH only; do not expose 8080.
- Replace the example Caddy or Nginx hostname before loading the configuration.
- Keep the selected proxy and the operating system patched. Verify certificate renewal with `systemctl status caddy` or `systemctl status nginx` and the corresponding logs.
- Together trusts `X-Forwarded-For` only from a loopback peer. Keep the app bound to loopback and do not put an untrusted local proxy in front of it; direct non-loopback clients cannot choose their rate-limit identity.

## Process isolation and secrets

- Use the supplied `together` system account; never run the service as root.
- Keep `/etc/together.env` owned by root and readable only by the `together` group (`0640`). On a new database, `ADMIN_USER` and an `ADMIN_PASS` of at least 12 Unicode code points are required; after the first user exists, these seed values are ignored.
- Keep `/var/lib/together` owned by `together:together` and not world-readable. It contains SQLite sessions and uploaded media.
- Retain the supplied systemd hardening settings (`NoNewPrivileges`, read-only system paths, private `/tmp`, protected home, and explicit writable data path).

## Application behavior

- Treat invite links as credentials. Regenerate a room link if it was shared too widely.
- Accounts are invite-code gated, but this is not a substitute for network security or strong admin passwords.
- Upload processing invokes ffmpeg only at ingest time. Keep ffmpeg patched and restrict server access to trusted administrators.
- `TOGETHER_MAX_UPLOAD_BYTES` limits each new or resumed upload's declared total; it defaults to 20 GiB. Creation JSON, individual upload chunks, and subtitle bodies are also capped at 4 KiB, 8 MiB, and 10 MiB respectively. Set a lower limit in `/etc/together.env` when disk capacity requires it.
- Monitor disk space: uploaded and processed media consume local storage. The per-upload limit is not a filesystem-wide quota, so reserve headroom and delete media that is no longer needed.
- Login failures are rate-limited per trusted client IP (five immediate failures, then one every 12 seconds). Repeated registration failures are limited separately (ten immediate failures, then one every six seconds). A `429` response includes `Retry-After`.
- Rooms are ephemeral and bounded: 12 live WebSocket clients per room, 10 live rooms per owner, and 100 live rooms process-wide. Treat a room link as a credential and regenerate it if exposed.

## Backups and incident response

- Test restores, not just backups. The provided restic unit covers SQLite only; back up media separately.
- Store restic credentials and backups off the application host where possible.
- If compromise is suspected, take the service offline, preserve logs and backups, rotate credentials, revoke room links, and rebuild from a trusted release.

## GitHub repository settings

- Enable private vulnerability reporting and branch protection for `main`.
- Require the CI workflow and review before merging pull requests.
- Keep Actions permissions at read-only by default; the release workflow is the only workflow that needs `contents: write`.
- Review third-party Actions updates. The workflows use current major versions from the official Actions projects; pin to reviewed full commit SHAs if your organization requires immutable action references.
