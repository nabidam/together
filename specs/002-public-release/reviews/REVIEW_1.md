# Review 1 — public release preparation

**Reviewer:** independent reviewer
**Range:** `main...working tree`
**Precondition:** verification suite green before review; shell syntax and `git diff --check` green.

## Confirmed findings

1. **Medium — fresh-server backups cannot initialize their repository.**
   `docs/OPERATIONS.md:82-91` installs and enables the backup timer but does not create the default `RESTIC_REPOSITORY=/var/backups/together`. `deploy/together-backup.service:7,9` runs it as the unprivileged `together` user; `deploy/backup.sh:29,40` then tries to initialize that root-owned, nonexistent path.
   **Confirmation:** the documented fresh-server sequence creates `/var/lib/together` but not `/var/backups/together`; an unprivileged process cannot create the missing directory below root-owned `/var/backups`.
   **Fix task:** `specs/002-public-release/TASKS.md` R1. Implemented by creating `/var/backups/together` as `together:together` with mode `0700` before enabling the timer, and by mirroring the setup step in the backup script comments.

## No other confirmed findings

No other bugs, security issues, races, architecture/convention violations, test-adequacy gaps, or composition/fake-integrity issues were confirmed in the reviewed diff.
