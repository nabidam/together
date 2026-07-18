---
status: confirmed
reviewed_commit: 4faff8cb8019978b472ac5b93de8cff9e860ad3e
reviewed_range: repository-wide snapshot at 4faff8c
review_type: security
review_date: 2026-07-18
---

# Security review 1

## Scope

Repository-wide source review of Together `v0.1.0` at commit `4faff8c` on `main`. The user explicitly requested a whole-repository security audit, so the review covered the complete tracked snapshot rather than a feature diff.

Reviewed trust boundaries:

- Account authentication, registration, sessions, roles, and seed administration.
- Public guest invite, room session, and WebSocket surfaces.
- Room lifecycle, concurrency, authorization, and resource limits.
- Administrative media upload, ffmpeg pipeline, file serving, and deletion.
- Svelte rendering and browser storage/navigation boundaries.
- Caddy/systemd deployment examples, backup script, dependencies, and committed-secret patterns.

Runtime versions and configuration of the deployed operating system, Caddy, ffmpeg, firewall, and host were not available and were not assessed.

## Baseline verification

The following completed successfully against the reviewed commit:

- `./scripts/verify.sh`
- `go test -race ./...`
- `go vet ./...`
- `npm audit --json` — zero known vulnerabilities across 144 dependencies.
- `go run golang.org/x/vuln/cmd/govulncheck@latest -show verbose ./...` — zero reachable vulnerabilities.
- Committed secret/private-key pattern scan — no findings.

`govulncheck` reported `GO-2026-5932` for the unmaintained `golang.org/x/crypto/openpgp` package at module level, but Together does not import or call that package.

## Confirmed findings

### S1 — Initial administrator accepts an empty or weak password

- **Severity:** high, configuration-dependent
- **Location:** `internal/auth/auth.go:46-56`, `cmd/server/main.go:57`, `internal/auth/auth_test.go:20-39`
- **Category:** authentication / insecure configuration
- **Confirmation:** confirmed

`Seed` checks only whether the user table is non-empty or the username is empty. It passes `ADMIN_PASS` directly to Argon2 without rejecting an empty or weak value. The existing `TestSessionRoundtrip` seeds and successfully authenticates an administrator with the two-character password `pw`, confirming the behavior.

**Impact:** A first boot with `ADMIN_USER` set and a missing or weak `ADMIN_PASS` exposes full remote administrator access, including media upload, account invitations, and all library content.

**One-line fix:** Refuse first-user provisioning unless both seed values are present and the password meets a strong minimum; stop startup before listening.

**Fix task:** `specs/003-security-hardening/TASKS.md` Task 1.

### S2 — Guest WebSocket connections bypass the 12-participant cap

- **Severity:** medium
- **Location:** `internal/live/rooms.go:260-279`, `internal/live/hub.go:249-270`
- **Category:** resource exhaustion / missing authorization boundary
- **Confirmation:** confirmed by failing isolated regression

The participant cap is evaluated while minting a guest session, but `Handle` adds every authenticated WebSocket to `Room.clients` without checking capacity. One guest cookie or account session can therefore open unbounded live connections.

The isolated regression opened 13 WebSockets with one guest cookie and observed:

```text
participant cap bypassed: 13 live connections exceed cap 12
```

**Impact:** An invite-holding guest or ordinary account can consume unbounded sockets, goroutines, send channels, presence entries, and broadcast work until the process is degraded or killed.

**One-line fix:** Enforce the cap atomically under `Room.mu` at WebSocket admission and policy-close over-cap connections before presence.

**Fix task:** `specs/003-security-hardening/TASKS.md` Task 3.

### S3 — Account invite codes contain only 32 bits of entropy

- **Severity:** medium
- **Location:** `internal/auth/http.go:104-109`
- **Category:** weak credential / unauthorized account creation
- **Confirmation:** confirmed by code and endpoint contract

The invite endpoint allocates four random bytes and hex-encodes them, producing an eight-character code with only 32 bits of entropy. Registration has no attempt throttle, and unused codes do not expire.

Concrete reproduction:

1. Authenticate as an administrator and call `POST /api/admin/invites`.
2. Observe an eight-character hexadecimal credential.
3. Send candidate codes to the public `POST /api/register` endpoint; invalid guesses are rejected cheaply before Argon2 and may be attempted without an application or proxy limit.

**Impact:** Guessing one active unused code creates a permanent member account with library-wide access to every ready media item and all account-visible rooms.

**One-line fix:** Generate at least 16 random bytes for new codes and throttle public registration attempts while continuing to accept already-issued codes during upgrade.

**Fix task:** `specs/003-security-hardening/TASKS.md` Task 2.

### S4 — Login permits unbounded guessing and leaks username existence through work-factor timing

- **Severity:** medium
- **Location:** `internal/auth/auth.go:59-67`, `internal/auth/auth.go:22-29`, `internal/auth/http.go:59-72`, `deploy/Caddyfile:1-10`
- **Category:** authentication brute force / username enumeration / denial of service
- **Confirmation:** confirmed by control flow; concrete timing reproduction documented

`Login` uses `err != nil || !Verify(...)`. For an unknown username the left side is true and Go short-circuits, skipping Argon2; a known username with the wrong password performs Argon2. Neither the application routes nor the supplied Caddy configuration rate-limit login attempts. The two-slot hash semaphore caps simultaneous Argon2 memory but does not bound request queues or sustained CPU use.

Concrete reproduction:

1. Send repeated login requests for the documented administrator username with a wrong password and measure response time.
2. Repeat with a random nonexistent username.
3. The nonexistent path returns without Argon2 while the known-user path performs it; once a username is identified, sustained requests continuously occupy both hash slots.

**Impact:** Remote attackers can enumerate account names, make unlimited online password guesses, saturate authentication CPU, and queue blocked request goroutines.

**One-line fix:** Apply bounded per-client throttles before hashing, perform one dummy Argon2 verification for unknown users, and return indistinguishable JSON failures.

**Fix task:** `specs/003-security-hardening/TASKS.md` Task 2.

### S5 — Newly created rooms can remain forever and room creation is unbounded

- **Severity:** medium
- **Location:** `internal/live/rooms.go:376-416`, `internal/live/hub.go:272-284`, `internal/live/rooms.go:184-203`
- **Category:** authenticated resource exhaustion / algorithmic complexity
- **Confirmation:** confirmed by failing isolated regression

Room creation inserts a room with no `emptyTimer`. The timer is created only after the last WebSocket disconnects, so a room that is never connected never expires. There is no per-owner or global room quota. Public token lookup also linearly scans and locks every room, amplifying the impact of a large room set.

The isolated regression created a room with a 20 ms idle setting, never connected a socket, waited four idle periods, and observed:

```text
empty room created without any websocket never received an idle timer
```

**Impact:** Any invited member can accumulate unbounded room/token/chat structures and make public join-token probes increasingly expensive.

**One-line fix:** Arm idle expiry at creation, add per-owner/global room quotas, and index current join tokens with teardown/regeneration-safe updates.

**Fix task:** `specs/003-security-hardening/TASKS.md` Task 4.

### S6 — A room guest can start unrelated ready media

- **Severity:** low
- **Location:** `internal/live/hub.go:341-353`, `internal/live/rooms.go:146-164`
- **Category:** authorization scope violation
- **Confirmation:** confirmed by failing isolated regression

The `start` handler verifies only that the supplied media ID is globally ready. It does not require the ID to equal the room's immutable `mediaID`, even though guest media-byte authorization is explicitly scoped to that fixed media.

The isolated regression joined a media-1 room as a guest, inserted ready media 2, sent `start{mediaId:2}`, and observed:

```text
room-scoped guest changed activity from room media 1 to unrelated media 2
```

**Impact:** A guest can probe whether arbitrary media IDs are ready and disrupt its room by switching authoritative activity to media it is not authorized to download.

**One-line fix:** Reject `start` unless the requested ID equals `Room.mediaID`, send a recoverable error, and leave activity unchanged.

**Fix task:** `specs/003-security-hardening/TASKS.md` Task 5.

### S7 — Administrative upload request and total sizes are not server-enforced

- **Severity:** low
- **Location:** `internal/media/upload.go:33-50`, `internal/media/upload.go:62-88`, `internal/media/upload.go:90-112`
- **Category:** resource exhaustion / input-boundary failure
- **Confirmation:** confirmed by code; concrete HTTP reproduction documented

Media creation decodes an unrestricted JSON body. Upload PATCH streams the entire request with raw `io.Copy` despite the client convention of 8 MiB chunks. The server stores no declared input total and allows `finish` without checking the blob against one. Subtitle upload uses `LimitReader`, silently accepting a truncated body rather than rejecting an oversized one.

Concrete reproduction:

1. Authenticate as administrator and create an uploading media row.
2. Send one PATCH body larger than 8 MiB at offset zero.
3. Observe the server write the complete body and return its full size.
4. Call finish without any declared-total verification.

**Impact:** A compromised administrator session or defective client can bypass intended chunk boundaries, consume arbitrary disk, and take the service down.

**One-line fix:** Bound every request body, require a declared upload total, reject overrun/incomplete finish, and enforce a configurable per-media maximum.

**Fix task:** `specs/003-security-hardening/TASKS.md` Task 8.

## Compound-rule follow-up

The same missing-bound pattern appears at multiple trust boundaries: authentication attempts, live connections/rooms, and upload bodies. The remediation cycle therefore includes updates to `CONVENTIONS.md` and regression layers in Task 9 so future externally controlled collections, queues, request bodies, and expensive operations require an explicit server-side budget.

## Negative findings

- No evident SQL injection: dynamic media-kind SQL is restricted to the exact `video|audio` enumeration; other values use parameters.
- No evident frontend XSS sink: Svelte escapes account, guest, room, media, subtitle, and chat strings; no `{@html}`, `innerHTML`, `eval`, or `javascript:` use was found.
- WebSocket origin verification remains enabled through `websocket.Accept` defaults.
- Session and room credentials use cryptographic randomness with sufficient entropy; only account invite codes were undersized.
- Media and subtitle serving paths derive from server-owned database values; reviewed request path parameters do not directly select filesystem paths.
- No committed private keys, environment secrets, or API credentials were found.

## Required re-review

After Tasks 1–9 are complete and verification is green, run a fresh Phase 6a review over the full security-hardening diff. Each reproduction above must exist as a permanent regression test or production-journey assertion before the release gate can pass.
