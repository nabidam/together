---
status: gate-passed
---

# Public release preparation

Prepare Together for a public GitHub release under Apache-2.0. Add CI that verifies Go, frontend, formatting, and a reproducible bundled-web release build. Produce release artifacts for Linux amd64 and arm64 with checksums; each artifact must run as one binary alongside documented system dependencies. Replace the maintainer-oriented README with public project, setup, operations, reverse-proxy, backup/restore, and security-hardening documentation. Add the standard contributor and security-reporting documents appropriate to a public repository. Out of scope: a new release-hosting service, automatic production deployment, telemetry, schema/API changes, and Windows/macOS support.

## Acceptance criteria

- GitHub Actions runs the project verification suite and validates release builds on pull requests and main pushes.
- A release script emits Linux amd64/arm64 binaries with the SPA embedded and SHA-256 checksums.
- Fresh-server, systemd, Caddy/TLS, firewall, backup/restore, upgrade, and hardening instructions are complete and internally consistent.
- The repository includes Apache-2.0 licensing, contribution, and responsible-security-reporting guidance.

## Impact analysis

Fits the existing architecture: the Go binary already embeds the Vite build through `go:embed`, and release tooling can wrap that existing composition without changing runtime behavior. No schema, API, module boundary, or UX flow changes are needed. Affected files are build/release scripts, GitHub workflow configuration, deploy examples, and public documentation; `ARCHITECTURE.md`, `UX.md`, and `DESIGN.md` remain true. The documentation change belongs in README plus new operations and community documents, not an architecture-contract patch.
