---
status: gate-passed
---

# PLAN.md — Security hardening

Sources: `specs/003-security-hardening/SPEC.md`, `specs/003-security-hardening/PRD.md`, the confirmed 2026-07-18 review findings, and the living contracts in `ARCHITECTURE.md`, `UX.md`, `CONVENTIONS.md`, and `DESIGN.md`.

The first two chunks are the walking skeleton: they close remotely reachable authentication and room-control paths with real HTTP/WebSocket integration tests. Each implementation task stays near 50–300 changed lines, preserves the existing production composition, and ends with `./scripts/verify.sh` green.

## Chunk 1 — Authentication boundary

**Files:** `internal/auth/auth.go`, `internal/auth/auth_test.go`, `internal/auth/http.go`, `internal/auth/http_test.go`, `internal/auth/throttle.go` (new), `internal/auth/throttle_test.go` (new), `cmd/server/main.go`.

**Requirements:**

- Make first-user provisioning fail closed: empty database requires `ADMIN_USER` and a password of at least 12 Unicode code points; existing databases ignore seed variables.
- Preserve `Seed(*sql.DB,string,string) error`, returning a credential-free configuration error that stops `main` before listen.
- Generate new account invite codes from 16 random bytes while accepting existing 8-character codes.
- Make unknown-user and known-user/wrong-password login paths perform one Argon2 verification and return identical JSON `401` responses.
- Add a stdlib-only, fake-clock-testable token bucket: login 5 burst/5 per minute; register 10 burst/10 per minute; bounded to 10,000 keys with 15-minute idle eviction.
- Resolve client IP from the socket peer; honor the first valid `X-Forwarded-For` IP only for a loopback peer.
- Emit JSON `429` plus integer `Retry-After`; never burn an invite on a throttled request.

**Acceptance:**

- PRD AC-1.1–1.3 and AC-2.1–2.4 pass at unit/integration layer without sleep-based tests.
- Existing login, registration rollback, admin authorization, session, and invite tests remain green under `-race`.

**Do not:** add CAPTCHA/account lockout, persist limiter state, trust proxy headers from a non-loopback peer, log credentials/usernames, or add dependencies.

## Chunk 2 — Room resource and media authorization boundary

**Files:** `internal/live/hub.go`, `internal/live/hub_test.go`, `internal/live/rooms.go`, `internal/live/rooms_test.go`.

**Requirements:**

- Enforce 12 live WebSocket clients under `Room.mu`; a rejected connection is policy-closed before it joins presence.
- Arm a room's idle timer at creation. Preserve stop-on-first-connect and reset-on-last-disconnect semantics.
- Enforce 10 rooms per owner and 100 rooms globally; return JSON `429` without allocating room/token state.
- Add a hub join-token index. Create, regenerate, and teardown update it; lookup copies the pointer under `Hub.mu`, releases it, then validates current token/closed state under `Room.mu` to preserve lock order.
- `start` accepts only the room's immutable media ID. A mismatch sends an error and does not mutate `watch`.
- Add regression tests reproducing the review's 13-connection, never-connected room, and cross-media start failures; run all under `-race`.

**Acceptance:**

- PRD AC-3.1–3.5 and NFR-1–2 pass.
- Existing reconnect, two-host-tab, token no-oracle, teardown, panic isolation, presence, and chat tests remain green.

**Do not:** reduce legitimate two-tab host behavior, count disconnected guest cookies as live sockets, change room IDs/join-token entropy, gate play/pause/seek by host, or introduce a lock-order inversion.

## DEMO GATE 1 — Authentication and room boundaries

**Journey:** Launch the production binary with disposable storage. Observe weak initial-admin startup refusal, then strong-admin startup. Generate a 32-hex invite, register a member, and exercise login throttling. Create/upload a tiny media fixture, create a room, join with one guest identity, open 12 connections, and verify the thirteenth never enters presence. Create a never-connected short-idle room and observe teardown. Attempt cross-media `start` and observe a recoverable error with unchanged activity. Regenerate the room token and confirm old/new no-oracle behavior.

**Unglamorous step:** send malformed proxy headers and repeated invalid credentials; the process stays live, returns JSON errors, and stores no unbounded limiter state.

**Runnability preconditions:**

- Build/launch: `./build.sh`; then run the resulting production binary with `TOGETHER_DATA` set to a new `mktemp -d`, `TOGETHER_ADDR=127.0.0.1:18080`, `TOGETHER_ROOM_IDLE=2s`, and a strong seed credential.
- Seed: upload a 1-second ffmpeg-generated media fixture through the real admin API; all state remains under the disposable data directory.
- Serving chunks: Chunk 1 serves provisioning/invites/throttling; Chunk 2 serves WebSocket capacity, room lifetime/token index, and media scoping.

**Crystallization:** Immediately encode this journey as `scripts/security-e2e.sh`. The first version covers the authentication and room-boundary steps through the production binary; Chunk 4 extends the same script with upload and restart coverage.

## Chunk 3 — Upload request and total-size boundaries

**Files:** `internal/media/upload.go`, `internal/media/upload_test.go`, `internal/media/pipeline.go`, `web/src/lib/upload.js`, `cmd/server/main.go`.

**Requirements:**

- Restore the documented create contract `{title,origName,sizeBytes}` and cap its JSON at 4 KiB.
- Add `TOGETHER_MAX_UPLOAD_BYTES`, integer bytes, default `21474836480`; validate it once in `main` and pass the value into upload route construction.
- Store declared input size while status is `uploading`; allow a legacy uploading row with absent size to establish it once through `Upload-Length`.
- Limit each PATCH to 8 MiB, prevent writes beyond declared total, and require exact total before finish.
- Enforce 10 MiB subtitles without silently truncating an oversized body.
- Update the browser upload client to send `sizeBytes` and `Upload-Length`.
- Preserve final processed `size_bytes`: the pipeline overwrites the declared input size with the ready output file size as today.

**Acceptance:**

- PRD AC-4.1–4.4 pass, including upgrade/resume fixtures.
- Oversized requests leave the file at its prior valid size; existing resumable upload and pipeline tests remain green.

**Do not:** buffer chunks in memory, read an entire media body before writing, add a new durable column, invalidate ready media when the configured maximum changes, or alter ffmpeg planning.

## Chunk 4 — Production security journey and contract sync

**Files:** `scripts/security-e2e.sh` (new), `scripts/verify.sh`, `ARCHITECTURE.md`, `CONVENTIONS.md`, `docs/HARDENING.md`, `docs/OPERATIONS.md`, `README.md`, `specs/003-security-hardening/evidence/`.

**Requirements:**

- Add a fail-closed shell journey that creates its own disposable directory, builds/launches the production entry point, waits for health, exercises seed/login/invite/room/upload security behavior, terminates the process, and always removes temporary state.
- The script must refuse a caller-supplied non-temporary data path and must not require Internet access.
- Add the journey to `scripts/verify.sh` only if its runtime stays below 60 seconds and ffmpeg is available; otherwise make verify print a clear skip and keep the release gate mandatory.
- Patch living contracts to state the implemented seed rule, authentication throttles/proxy trust, room quotas/token index/live capacity, fixed-media start, upload request limits, new env var, JSON error codes, and regression-test layers.
- Update hardening/operations with reverse-proxy trust and upload-limit guidance. Append one `ARCHITECTURE.md` Decision log entry.

**Acceptance:**

- The production journey passes twice consecutively and cleans up its process/data on both success and injected failure.
- `./scripts/verify.sh`, `go test -race ./...`, frontend tests/build, `git diff --check`, and doc consistency checks pass.

**Do not:** describe unimplemented controls, require production credentials/data, add network calls, change UI styling, or turn operator guidance into a second source of exact protocol truth.

## RELEASE GATE — Hardened production composition

**Journey:** Run a release build with disposable storage and execute the kernel journey from `SPEC.md`: weak seed refusal → strong seed → 128-bit invite/register → uniform/throttled login → capped room connections → fixed-media authorization → idle/quota cleanup → bounded resumable upload → restart and durable-data verification.

**Unglamorous step:** interrupt an oversized upload and terminate the process mid-journey; rerun against the disposable database and verify no overrun bytes, stuck listener, leaked child process, or corrupted durable state.

**Runnability preconditions:**

- Build/launch: `./scripts/release.sh` and the extracted release binary, configured only with a fresh `mktemp -d` data directory and loopback address.
- Seed: `scripts/security-e2e.sh` creates the tiny local fixture and all disposable credentials/state.
- Serving chunks: Chunks 1–4 serve every journey step through the production composition.
- Evidence: `./scripts/verify.sh`, `npm audit`, and `go run golang.org/x/vuln/cmd/govulncheck@latest -show verbose ./...` outputs are recorded under this cycle's `evidence/`.
