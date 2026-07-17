# Contributing to Together

Thanks for helping improve Together. Please use GitHub issues for reproducible bugs and feature proposals; do not post security vulnerabilities publicly—see [SECURITY.md](SECURITY.md).

## Development workflow

1. Fork the repository and create a focused branch from `main`.
2. Keep behavior changes covered by an automated test at the appropriate layer.
3. Run `./scripts/verify.sh` before opening a pull request.
4. Describe user-visible behavior, testing performed, and any operational impact in the pull request.

The repository uses Go, Svelte 5, and plain Node tests. Read [CONVENTIONS.md](CONVENTIONS.md) and [ARCHITECTURE.md](ARCHITECTURE.md) before changing runtime behavior. Keep dependencies within the documented budgets; do not add a framework or package for a small convenience.

## Pull requests

Keep pull requests narrow and use Conventional Commit subjects such as `fix: repair upload resume`. Do not commit generated `cmd/server/webdist/` assets, local data, credentials, media files, or release archives. UI changes must preserve the UX and token contracts in `UX.md`, `DESIGN.md`, and `design.md`.

## License

By submitting a contribution, you license it under Apache-2.0, the same license as this project.
