#!/bin/sh
# Nightly SQLite backup for together, via restic.
#
# Scope: DATABASE ONLY. Media (uploads/, media/) is NOT backed up yet, and the
# restic repo defaults to a LOCAL path on the same VPS — this protects against
# DB corruption / bad migration / accidental delete, NOT VPS or disk loss.
# Offsite destination + media backup are deferred; see docs/debt.md.
#
# Requires: sqlite3 CLI (apt install sqlite3) and restic on PATH.
#
# One-time setup on the box:
#   apt install sqlite3 restic
#   install -m 755 deploy/backup.sh /usr/local/bin/together-backup.sh
#   umask 077; head -c 32 /dev/urandom | base64 > /etc/together-restic.pass
#   chown together /etc/together-restic.pass
#   install -d -o together -g together -m 700 /var/backups/together
#   cp deploy/together-backup.{service,timer} /etc/systemd/system/
#   systemctl enable --now together-backup.timer
#   # verify: systemctl start together-backup.service && journalctl -u together-backup
#   # KEEP /etc/together-restic.pass safe — lose it and the repo is unrecoverable.
#
# Config comes from the environment / systemd EnvironmentFile:
#   RESTIC_REPOSITORY        (default /var/backups/together)
#   RESTIC_PASSWORD_FILE     restic repo password (required)
#   TOGETHER_DATA            (default /var/lib/together)
set -eu

DATA="${TOGETHER_DATA:-/var/lib/together}"
DB="$DATA/together.db"
export RESTIC_REPOSITORY="${RESTIC_REPOSITORY:-/var/backups/together}"

SNAP="$(mktemp)"
trap 'rm -f "$SNAP" "$SNAP-wal" "$SNAP-shm"' EXIT

# Consistent single-file snapshot of the WAL-mode DB (the .db file alone is torn
# under WAL — .backup uses SQLite's backup API to serialize a clean copy).
sqlite3 "$DB" ".backup '$SNAP'"

# Init the repo on first run (idempotent).
restic cat config >/dev/null 2>&1 || restic init

restic backup --stdin --stdin-filename together.db --tag together-db < "$SNAP"
restic forget --tag together-db --keep-daily 7 --keep-weekly 4 --prune
