---
status: gate-passed
---

# PRD — Security hardening

## 1. Goal

Close the confirmed authentication, resource-exhaustion, room-authorization, and upload-boundary findings without changing Together's core user journey or adding runtime dependencies.

## 2. Functional requirements

### Authentication and provisioning

- **FR-1 — Fail-closed seed:** When the database has no users, startup requires non-empty `ADMIN_USER` and an `ADMIN_PASS` of at least 12 Unicode code points. Missing or weak values stop startup with a credential-free error. Once a user exists, seed environment variables are ignored as today.
- **FR-2 — Strong account invites:** New account invite codes contain 16 cryptographically random bytes encoded as 32 lowercase hexadecimal characters. Existing unused 8-character codes remain redeemable so upgrades do not invalidate operator-issued credentials.
- **FR-3 — Uniform login work:** A login attempt for an unknown username and a wrong password for a known username each perform exactly one Argon2 verification and return the same `401` error shape.
- **FR-4 — Authentication throttles:** Failed login attempts are limited per client IP to a burst of 5 and a sustained rate of 5/minute. Failed registration attempts are limited per client IP to a burst of 10 and 10/minute. Rejection is JSON `429`, includes integer-seconds `Retry-After`, and does not consume an invite.
- **FR-5 — Safe client identity:** Rate limiting uses the socket peer IP. `X-Forwarded-For` is honored only when the socket peer is loopback, taking the first syntactically valid IP. Limiter storage is capped at 10,000 entries; entries idle for 15 minutes are evicted.

### Room resource and authorization boundaries

- **FR-6 — Live capacity:** A room admits at most 12 live WebSocket connections, including account and guest tabs. Admission is decided under `Room.mu`; an over-cap connection is closed with WebSocket policy-violation status and never appears in presence.
- **FR-7 — Room quotas:** One owner may hold at most 10 live rooms and the process at most 100. Exceeding either returns JSON `429` without creating state.
- **FR-8 — Empty-from-birth expiry:** Room creation arms the existing idle timer immediately. First connection stops it; subsequent last-disconnect/reconnect behavior remains unchanged.
- **FR-9 — Indexed join credentials:** Current join tokens are indexed by the hub. Create, regeneration, and teardown update the index; stale and unknown tokens retain byte-identical `404` responses.
- **FR-10 — Fixed-media control:** `start` succeeds only when `mediaId` equals the room's immutable `mediaID`. A mismatch returns a recoverable WebSocket `error` and leaves activity unchanged.

### Upload boundaries

- **FR-11 — Declared size:** `POST /api/admin/media` accepts `{title,origName,sizeBytes}`. `sizeBytes` must be positive and no greater than `TOGETHER_MAX_UPLOAD_BYTES`, default `21474836480` (20 GiB).
- **FR-12 — Bounded bodies:** Media-creation JSON is limited to 4 KiB, upload PATCH bodies to 8 MiB, and subtitle bodies to 10 MiB. An oversized body returns JSON `413`.
- **FR-13 — Exact upload accounting:** A PATCH may not write beyond declared `sizeBytes`; `finish` succeeds only when the upload blob's exact size equals the declared size. Offset mismatch or incomplete/overrun upload returns JSON `409`.
- **FR-14 — Upgrade compatibility:** Rows with `status='uploading'` and null/non-positive `size_bytes` may establish their declared size on their first post-upgrade PATCH from its explicit `Upload-Length` header. Later requests must match it. New clients always declare size at creation.

### Documentation and evidence

- **FR-15 — Contract sync:** `ARCHITECTURE.md`, `CONVENTIONS.md`, `docs/HARDENING.md`, `docs/OPERATIONS.md`, and configuration tables describe the implemented limits, trust model, response codes, and operator setting.
- **FR-16 — Regression suite:** Every confirmed finding has a test that fails against `v0.1.0` behavior and passes after its fix. The security journey launches the production entry point with disposable storage.

## 3. User stories and acceptance criteria

### US-1 Safe first boot

- **AC-1.1** Start with an empty data directory and `ADMIN_USER=admin` but no password: the process exits non-zero before listening and logs only that strong seed credentials are required.
- **AC-1.2** Repeat with an 11-character password: same result. Repeat with 12 characters: startup succeeds and login works.
- **AC-1.3** Restart a database that already has users with seed variables absent: startup succeeds and existing login still works.

### US-2 Brute-force-resistant entry

- **AC-2.1** Generate an invite: the returned code matches `^[0-9a-f]{32}$`; a previously issued 8-character unused code still registers once.
- **AC-2.2** Instrument the password verifier: unknown-user and known-user/wrong-password attempts each call it exactly once and return indistinguishable `401` bodies.
- **AC-2.3** With a fake clock, the sixth immediate failed login from one IP returns `429` and `Retry-After`; another IP is unaffected; advancing 12 seconds admits one attempt.
- **AC-2.4** A spoofed `X-Forwarded-For` from a non-loopback peer does not change the limiter key; the same header from loopback uses its first valid address.

### US-3 Bounded rooms

- **AC-3.1** Open 12 live sockets in one room: all appear in presence. The thirteenth is policy-closed and room presence remains 12.
- **AC-3.2** Create a room with a 50 ms test idle and never connect: it disappears and its join token returns the same `404` body as an unknown token.
- **AC-3.3** One member's eleventh live room and the process's 101st live room return `429`; deleting/expiring one room frees capacity.
- **AC-3.4** Regenerate a token: old token fails, new token succeeds, connected guests persist, and indexed lookup remains correct under the race detector.
- **AC-3.5** A guest in a media-1 room sends `start{mediaId:2}` where media 2 is ready: the guest receives an error and the room remains on media 1.

### US-4 Bounded resumable upload

- **AC-4.1** Create a 16 MiB upload and send two 8 MiB chunks: resume offsets advance exactly and finish queues processing.
- **AC-4.2** Send an 8 MiB + 1 byte chunk, a write beyond declared size, or a subtitle over 10 MiB: each is rejected with `413` or `409` and no excess byte is committed.
- **AC-4.3** Finish before declared bytes arrive: returns `409`; append remaining bytes and finish succeeds.
- **AC-4.4** Resume a pre-upgrade uploading row using `Upload-Length`: the declared total is established once; a conflicting later total is rejected.

## 4. Validation and error contract

| Boundary | Validation | Error |
|---|---|---|
| Seed admin | username present; password ≥12 code points | startup error, no listener |
| Login/register rate | token bucket by trusted client IP | `429 {"error":"too many attempts"}` + `Retry-After` |
| Invite generation | 16 random bytes | `500` on storage failure |
| Live room connections | `len(clients) < 12` under room lock | WS policy close; no presence entry |
| Room creation | owner <10 and global <100 | `429 {"error":"room limit reached"}` |
| WS `start` | requested ID equals fixed room media | WS `error`; state unchanged |
| Media creation | 4 KiB JSON; positive size ≤configured max | `400` or `413` |
| Upload PATCH | body ≤8 MiB; offset and total in bounds | `409` or `413` |
| Subtitle | body ≤10 MiB | `413` |

## 5. Non-functional requirements

- **NFR-1 — Memory bound:** limiter state never exceeds 10,000 keys and a full room never exceeds 12 live client entries. Measurement: deterministic unit tests inspect counts after 20,000 unique IPs and 13 socket attempts.
- **NFR-2 — Public token lookup:** unknown-token lookup with 100 rooms completes without scanning room mutexes. Measurement: a unit test installs lock-held unrelated rooms and confirms lookup completes within 100 ms; `go test -race ./internal/live` remains green.
- **NFR-3 — Compatibility:** existing account/session/invite/media data survives upgrade. Measurement: database integration fixture containing an 8-character invite and legacy uploading row passes registration/resume tests.
- **NFR-4 — Dependency ceiling:** no new Go or JavaScript runtime dependency. Measurement: `go.mod` direct requirements and `web/package.json` dependency names are diffed by `scripts/verify.sh`.
- **NFR-5 — Verification:** `./scripts/verify.sh`, the production security journey, `npm audit`, and official `govulncheck` report no reachable vulnerability. Measurement: release-gate evidence records commands and outputs.

## 6. Error and edge cases

- Proxy headers containing malformed, private, multiple, or empty values must not panic or create unbounded keys.
- A rate-limited request with malformed JSON is still rejected before expensive hashing.
- Simultaneous 12th/13th WebSocket handshakes admit only one when one slot remains.
- Room teardown racing token regeneration leaves neither an old nor orphaned live token.
- An accepted upload chunk whose client disconnects early commits only bytes actually read and reports the resulting offset.
- Lowering the configured maximum does not corrupt existing ready media; it only constrains new/resumed uploads.
- A process restart intentionally clears rate-limit and room state.

## 7. Constraints and out of scope

The existing single-process Go/Svelte architecture, SQLite schema shape, session cookies, URL paths, WebSocket frame names, ffmpeg pipeline, and visual design remain. No CAPTCHA, distributed limiter, account lockout, invite expiry/revocation UI, malware scan, multi-instance coordination, or filesystem-wide reservation system is included.
