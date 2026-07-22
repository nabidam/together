---
status: passed
review_type: security
review_date: 2026-07-22
---

# Security Review 2

## Scope

Independent Phase 6a review of security-hardening behavior and the current
working tree, including authentication, room authorization, resource bounds,
uploads, WebSocket admission, and release tooling.

## Findings and Resolution

1. `internal/live/rooms.go` could mint a guest session from a join token
   regenerated after `roomByToken` returned. The join path now verifies that
   `h.tokens[in.Token]` still maps to the same room while holding `h.mu` before
   creating the guest session.
2. `internal/live/hub.go` could validate a `start` media ID before a concurrent
   confirmed media change, then write an activity for stale media. The start
   path now rechecks the selected media under the room lock that writes watch
   state and returns the existing recoverable error when it changed.

Focused re-review after both fixes: no findings.

## Verification

- `./scripts/verify.sh`
- `./scripts/release.sh`; both archive checksums verified
- Extracted Linux amd64 release binary started on loopback and passed `/healthz`
- Two consecutive `./scripts/security-e2e.sh` runs
- `TOGETHER_E2E_INJECT_FAILURE=1 ./scripts/security-e2e.sh` failed cleanly
- `go test ./... -race`, `go vet ./...`, `gofmt -l internal cmd`,
  `govulncheck -show verbose ./...`, and `git diff --check`
