# Hardening Together

Use this checklist before making an instance public. It complements, rather than replaces, operating-system patching and a secure SSH policy.

## Network and TLS

- Run Together only on `127.0.0.1:8080`; put Caddy in front of it for TLS.
- Permit inbound 80/443 and SSH only; do not expose 8080.
- Replace the example Caddy hostname before loading the configuration.
- Keep Caddy and the operating system patched. Verify certificate renewal with `systemctl status caddy` and Caddy logs.

## Process isolation and secrets

- Use the supplied `together` system account; never run the service as root.
- Keep `/etc/together.env` owned by root and readable only by the `together` group (`0640`); use a unique, long first-admin password.
- Keep `/var/lib/together` owned by `together:together` and not world-readable. It contains SQLite sessions and uploaded media.
- Retain the supplied systemd hardening settings (`NoNewPrivileges`, read-only system paths, private `/tmp`, protected home, and explicit writable data path).

## Application behavior

- Treat invite links as credentials. Regenerate a room link if it was shared too widely.
- Accounts are invite-code gated, but this is not a substitute for network security or strong admin passwords.
- Upload processing invokes ffmpeg only at ingest time. Keep ffmpeg patched and restrict server access to trusted administrators.
- Monitor disk space: uploaded and processed media consume local storage. Delete media that is no longer needed.

## Backups and incident response

- Test restores, not just backups. The provided restic unit covers SQLite only; back up media separately.
- Store restic credentials and backups off the application host where possible.
- If compromise is suspected, take the service offline, preserve logs and backups, rotate credentials, revoke room links, and rebuild from a trusted release.

## GitHub repository settings

- Enable private vulnerability reporting and branch protection for `main`.
- Require the CI workflow and review before merging pull requests.
- Keep Actions permissions at read-only by default; the release workflow is the only workflow that needs `contents: write`.
- Review third-party Actions updates. The workflows use current major versions from the official Actions projects; pin to reviewed full commit SHAs if your organization requires immutable action references.
