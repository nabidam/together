# Security policy

## Supported versions

Security fixes are made on `main` and the latest tagged release.

## Reporting a vulnerability

Do not open a public issue for a suspected vulnerability. Use GitHub's private vulnerability-reporting feature for this repository. If it is not enabled, contact the repository owner privately through their GitHub profile and include a minimal reproduction, affected version or commit, impact, and suggested mitigation if known.

Please allow time for triage and a fix before public disclosure. Do not access data that is not yours, disrupt a running service, or use a report to gain persistence.

## Deployment baseline

Together is intended to run behind a TLS-terminating reverse proxy, bound to loopback only, under a dedicated unprivileged account. Follow [docs/HARDENING.md](docs/HARDENING.md) before making an instance Internet-facing.
