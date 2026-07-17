# Public release tasks

## Done

- [x] Add reproducible bundled-web Linux release archives, checksums, and GitHub CI/release workflows.
- [x] Add public repository, deployment, reverse-proxy, backup/restore, and hardening documentation.
- [x] R1 — Create `/var/backups/together` as `together:together` (`0700`) before enabling backups, so the unprivileged restic service can initialize its default repository. Source: [REVIEW_1.md](reviews/REVIEW_1.md). Confirmed by the fresh-server path/ownership analysis in that review.
