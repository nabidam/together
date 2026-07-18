# Together

Together is a private, self-hosted watch-together server for small groups. It is a single Go binary with an embedded Svelte web app, SQLite persistence, in-memory rooms, and synchronized local-file-first video or audio playback.

Rooms, guest sessions, presence, chat, and playback state are intentionally ephemeral. Accounts, invite codes, and media metadata persist in SQLite. Media is processed once at upload time—Together never live-transcodes.

## Quick start

Download the release archive for your Linux architecture, verify its checksum, and install its single `together` binary. The server needs `ffmpeg` and `ffprobe` on `PATH` to process uploads.

```sh
sha256sum --check SHA256SUMS
tar -xzf together_linux_amd64.tar.gz
sudo install -m 0755 together /usr/local/bin/together

ADMIN_USER=admin ADMIN_PASS='choose-a-password-of-at-least-12-characters' together
```

Open `http://127.0.0.1:8080` only for local testing. For an Internet-facing server, follow the [operations guide](docs/OPERATIONS.md) to run Together behind Caddy with HTTPS.

## Supported release targets

Release artifacts are static, bundled-web Linux binaries for:

- `linux/amd64`
- `linux/arm64`

They require no Node.js, Go toolchain, or frontend files at runtime. They do require host-installed `ffmpeg`/`ffprobe` for media ingestion and a writable data directory. See [requirements and upgrades](docs/OPERATIONS.md#requirements) for the complete list.

## Development

```sh
# Backend API and embedded SPA on :8080.
ADMIN_USER=admin ADMIN_PASS='development-password-12' go run ./cmd/server

# Vite development server on :5173; it proxies API, WebSocket, and media routes.
cd web && npm run dev
```

## Verification and release builds

```sh
./scripts/verify.sh

# Build static Linux amd64 and arm64 archives plus SHA256SUMS in dist/.
./scripts/release.sh

# Build a single binary for the host architecture at ./together.
./build.sh
```

`scripts/release.sh` builds the frontend once, embeds it through `go:embed`, cross-compiles the supported targets, and restores the tracked web placeholder before exiting. The release archive contains exactly one executable.

`scripts/verify.sh` runs the production security journey when `ffmpeg` and `ffprobe` are installed; otherwise it prints a skip and the release gate must run that journey. The journey uses only a temporary data directory and covers seed credentials, throttling, room limits, media scope, bounded resumable uploads, and restart durability.

## Operations and security

- [Server setup, Caddy reverse proxy, systemd, backups, restore, and upgrades](docs/OPERATIONS.md)
- [Security model and hardening checklist](docs/HARDENING.md)
- [Responsible vulnerability reporting](SECURITY.md)

The supplied systemd unit binds the app to `127.0.0.1:8080`; Caddy is the public HTTPS endpoint. Do not expose the application port directly to the Internet.

On first boot, Together requires a non-empty `ADMIN_USER` and an `ADMIN_PASS` of at least 12 Unicode code points. `TOGETHER_MAX_UPLOAD_BYTES` sets a positive per-upload limit and defaults to 20 GiB; see the [operations guide](docs/OPERATIONS.md#upload-capacity) for sizing and proxy-trust guidance.

## Contributing

Read [CONTRIBUTING.md](CONTRIBUTING.md) before opening an issue or pull request. By contributing, you agree that your contributions are licensed under [Apache-2.0](LICENSE).

## Project documentation

| File | Purpose |
|---|---|
| `ARCHITECTURE.md` | Runtime contracts, API, WebSocket protocol, and configuration |
| `UX.md` / `DESIGN.md` / `design.md` | Product flow and visual-system contracts |
| `CONVENTIONS.md` | Code, test, commit, and UI rules |
| `CLAUDE.md` | Maintainer-oriented repository guide |
| `specs/` | Product, implementation, evidence, and review history |

## License

Copyright 2026 Together contributors. Licensed under the [Apache License 2.0](LICENSE).
