---
status: ready
---

# Security-hardening implementation tasks

Tasks are ordered for isolated agent sessions. An implementer should complete only one task per session, run its named checks, and record completion evidence in `specs/003-security-hardening/evidence/task-N.txt`. Context packs are predictions; verify them against the working tree before editing. Do not implement a task whose dependency evidence is missing.

## Task 1 — Fail-closed administrator provisioning

Make first boot reject absent or weak administrator credentials without changing startup behavior once a user exists.

```toml
id = 1
type = "fix"
chunk = 1
deps = []
skeleton = true
files = [
  "internal/auth/auth.go",
  "internal/auth/auth_test.go",
  "cmd/server/main.go",
]
consumes = []
produces = [
  "Seed(d *sql.DB, user, pass string) error",
]

[[criteria]]
text = "an empty database plus missing ADMIN_PASS or a password shorter than 12 Unicode code points returns a credential-free Seed error and inserts no user"
layer = "unit"

[[criteria]]
text = "a database with an existing user starts without ADMIN_USER or ADMIN_PASS and preserves that user"
layer = "integration"

[[criteria]]
text = "tests call Seed(d *sql.DB, user, pass string) error with empty, 11-code-point, 12-code-point, and existing-database cases and assert the contract"
layer = "contract"

[[criteria]]
text = "weak seed refuses startup; strong seed starts and existing databases restart without seed variables"
layer = "e2e"
gate = 1
```

**Inputs:** SPEC Kernel 1; PRD FR-1 and AC-1.1–1.3; confirmed review finding 1.

**Outputs:** Validated `Seed` behavior and startup propagation; updated tests that no longer rely on two-character seed passwords.

**Estimated difficulty:** Small.

**Context pack:** `internal/auth/auth.go`, `internal/auth/auth_test.go`, `cmd/server/main.go`, `specs/003-security-hardening/PRD.md` §FR-1/US-1, `ARCHITECTURE.md` §9. Backend-only; obey `CONVENTIONS.md` error/logging rules.

> **DONE** `f6989fe` — Evidence: `specs/003-security-hardening/evidence/task-1.txt`. `./scripts/verify.sh` passes: Go tests under `-race`, vet, formatting, frontend tests, and production web build. Seed rejects absent or weak first-boot credentials without credential disclosure, accepts 12 Unicode code points, and preserves an existing database when seed variables are absent.

## Task 2 — Uniform login work, bounded throttles, and strong account invites

Add a fake-clock token bucket and safe proxy-IP resolution, wire it into login/register, equalize password verification work, and lengthen newly issued codes.

```toml
id = 2
type = "fix"
chunk = 1
deps = [1]
skeleton = true
files = [
  "internal/auth/auth.go",
  "internal/auth/auth_test.go",
  "internal/auth/http.go",
  "internal/auth/http_test.go",
  "internal/auth/throttle.go",
  "internal/auth/throttle_test.go",
]
consumes = []
produces = [
  "POST /api/login -> 200 User | 401 {\"error\":\"invalid username or password\"} | 429 {\"error\":\"too many attempts\"}",
  "POST /api/register -> 200 User | 400 validation error | 429 {\"error\":\"too many attempts\"}",
  "clientIP(remoteAddr string, xForwardedFor string) string",
]

[[criteria]]
text = "unknown-user and known-user wrong-password login attempts each execute exactly one Argon2 verification and return byte-identical JSON 401 bodies"
layer = "unit"

[[criteria]]
text = "a fake-clock test proves login burst 5/refill 12 seconds, registration burst 10/refill 6 seconds, IP isolation, Retry-After, 10000-key cap, and 15-minute idle eviction without sleeping"
layer = "unit"

[[criteria]]
text = "non-loopback peers cannot alter their limiter key with X-Forwarded-For; loopback peers use the first syntactically valid forwarded IP"
layer = "unit"

[[criteria]]
text = "new invite codes match 32 lowercase hexadecimal characters while an existing unused 8-character code remains redeemable once"
layer = "integration"

[[criteria]]
text = "real HTTP tests call the produced login/register endpoints and clientIP signature exactly as specified, asserting JSON status, body, and Retry-After shapes"
layer = "contract"

[[criteria]]
text = "a 32-hex invite registers a member; uniform login failures throttle at the documented boundary while another IP remains available"
layer = "e2e"
gate = 1
```

**Inputs:** Task 1; PRD FR-2–5, AC-2.1–2.4, NFR-1; confirmed review findings 3–4.

**Outputs:** `throttle.go`, limiter/IP tests, uniform login verification, 128-bit new invite codes, JSON `429` contracts.

**Estimated difficulty:** Medium.

**Context pack:** `internal/auth/auth.go`, `internal/auth/http.go`, both existing auth test files, `cmd/server/main.go`, PRD authentication sections, `ARCHITECTURE.md` §4.1/§8–9, `deploy/Caddyfile`. Backend-only; no frontend or design docs.

> **DONE** `0cae561` — Evidence: `specs/003-security-hardening/evidence/task-2.txt`. `./scripts/verify.sh` passes: Go tests under `-race`, vet, formatting, frontend tests, dependency audit, and production web build. Login failures perform one Argon2 verification and return uniform JSON; bounded fake-clock throttles protect login/register by trusted client IP; new invites are 128-bit lowercase hexadecimal while legacy unused codes remain redeemable.

## Task 3 — Enforce live WebSocket capacity

Move the 12-participant security boundary to the actual live-connection admission point.

```toml
id = 3
type = "fix"
chunk = 2
deps = [2]
skeleton = true
files = [
  "internal/live/hub.go",
  "internal/live/hub_test.go",
  "internal/live/rooms.go",
  "internal/live/rooms_test.go",
]
consumes = []
produces = [
  "Room live connection capacity = 12; over-cap WebSocket closes with StatusPolicyViolation before presence",
]

[[criteria]]
text = "12 concurrent account/guest WebSockets enter presence and a thirteenth connection is policy-closed without entering Room.clients or any presence frame"
layer = "integration"

[[criteria]]
text = "two simultaneous handshakes racing for the last slot admit exactly one under go test -race"
layer = "integration"

[[criteria]]
text = "existing two-host-tab and reconnecting-guest tests remain valid; disconnected guest sessions are not counted as live connections"
layer = "integration"

[[criteria]]
text = "a contract test drives WebSocket admission and asserts the produced capacity and close-status contract exactly"
layer = "contract"

[[criteria]]
text = "12 room sockets appear in presence and a thirteenth is policy-closed without changing presence"
layer = "e2e"
gate = 1
```

**Inputs:** Task 2; PRD FR-6/AC-3.1/NFR-1; the confirmed 13-connection reproduction.

**Outputs:** Race-safe connection admission and regression coverage.

**Estimated difficulty:** Medium.

**Context pack:** `internal/live/hub.go`, `internal/live/hub_test.go`, `internal/live/rooms.go`, `internal/live/rooms_test.go`, PRD room sections, `ARCHITECTURE.md` §3.2/§4.5/§5. Backend-only.

> **DONE** `adc01ff` — Evidence: `specs/003-security-hardening/evidence/task-3.txt`. `go test -race ./internal/live -run 'TestWSCapacity' -count=3` proves a mixed account/guest set reaches exactly 12 live clients, the thirteenth receives WebSocket `StatusPolicyViolation` before any presence frame, and two concurrent final-slot handshakes admit exactly one. `./scripts/verify.sh` passes.

## Task 4 — Bound room creation, expire rooms from birth, and index join tokens

Prevent unbounded never-connected rooms and replace public linear token scans while preserving token/no-oracle behavior.

```toml
id = 4
type = "fix"
chunk = 2
deps = [3]
skeleton = true
files = [
  "internal/live/hub.go",
  "internal/live/hub_test.go",
  "internal/live/rooms.go",
  "internal/live/rooms_test.go",
]
consumes = []
produces = [
  "POST /api/rooms -> 201 {id,joinToken} | 404 media error | 429 {\"error\":\"room limit reached\"}",
  "Hub room quota = 10 per owner and 100 globally",
  "Hub.roomByToken(token string) (*Room, bool) uses the current-token index",
]

[[criteria]]
text = "a never-connected room created with a 50ms idle setting tears down, frees owner/global capacity, and makes its token indistinguishable from unknown"
layer = "integration"

[[criteria]]
text = "one owner's eleventh room and the process's 101st room return JSON 429 without allocating a room or token-index entry; teardown frees one slot"
layer = "integration"

[[criteria]]
text = "create, regenerate, and teardown keep the current-token index correct; stale and unknown token responses remain byte-identical under go test -race"
layer = "integration"

[[criteria]]
text = "holding unrelated room mutexes cannot block indexed lookup of an unknown token for more than 100ms"
layer = "unit"

[[criteria]]
text = "contract tests call the produced room endpoint and roomByToken signature, asserting quota, response, and current-token behavior exactly"
layer = "contract"

[[criteria]]
text = "a never-connected room expires, room quotas reject excess creation, and regenerated old/new tokens keep no-oracle behavior"
layer = "e2e"
gate = 1
```

**Inputs:** Task 3; PRD FR-7–9, AC-3.2–3.4, NFR-2; confirmed never-expiring-room reproduction.

**Outputs:** Creation quotas, creation-time timer, join-token index, lock-order/race regression tests.

**Estimated difficulty:** Medium–high.

**Context pack:** all `internal/live/{hub,rooms}.go` and tests, PRD FR-7–9, `ARCHITECTURE.md` §3.2/§4.3/§5, `CONVENTIONS.md` concurrency/test rules. Backend-only.

> **DONE** `642dbdc` — Evidence: `specs/003-security-hardening/evidence/task-4.txt`. `./scripts/verify.sh` passes: Go tests under `-race`, vet, formatting, frontend tests, dependency audit, and production web build. Rooms expire from creation when never connected; owner/global quotas reject excess rooms without allocating state; the current-token index is updated on create, regeneration, and teardown while stale/unknown tokens remain indistinguishable.

## Task 5 — Enforce fixed-media WebSocket authorization

Close the room-scope authorization gap in the legacy `start` frame without changing ordinary play/pause/seek permissions.

```toml
id = 5
type = "fix"
chunk = 2
deps = [4]
skeleton = true
files = [
  "internal/live/hub.go",
  "internal/live/hub_test.go",
]
consumes = []
produces = [
  "WS start{mediaId} mutates activity only when mediaId == Room.mediaID; mismatch -> error",
]

[[criteria]]
text = "a guest in a media-1 room sending start for ready media 2 receives an error and leaves WatchState.MediaID, version, and position unchanged"
layer = "integration"

[[criteria]]
text = "start for the room's own media still broadcasts activity; play, pause, seek, end, and status behavior is unchanged"
layer = "integration"

[[criteria]]
text = "a WebSocket contract test sends start frames and asserts the produced authorization/error contract exactly"
layer = "contract"

[[criteria]]
text = "a guest's unrelated-media start returns an error while the room remains on its fixed media"
layer = "e2e"
gate = 1
```

**Inputs:** Task 4; PRD FR-10/AC-3.5; confirmed cross-media reproduction.

**Outputs:** Fixed-media validation and integration regression.

**Estimated difficulty:** Small.

**Context pack:** `internal/live/hub.go`, `internal/live/hub_test.go`, `internal/live/watch.go`, PRD FR-10, `ARCHITECTURE.md` §3.2/§4.5. Backend-only.

> **DONE** `c982dc9` — Evidence: `specs/003-security-hardening/evidence/task-5.txt`. `start` now returns a recoverable error unless its media ID matches the room's immutable media ID; mismatches leave activity untouched while matching start and normal controls continue to broadcast activity.

## Task 6 — Demo Gate 1: authentication and room boundaries

Walk the first two chunks through the production binary before upload work begins.

```toml
id = 6
type = "gate"
chunk = 2
deps = [1, 2, 3, 4, 5]
skeleton = false
files = [
  "specs/003-security-hardening/evidence/task-6.txt",
  "specs/003-security-hardening/screenshots/",
]
consumes = [
  "Seed(d *sql.DB, user, pass string) error",
  "POST /api/login -> 200 User | 401 {\"error\":\"invalid username or password\"} | 429 {\"error\":\"too many attempts\"}",
  "POST /api/register -> 200 User | 400 validation error | 429 {\"error\":\"too many attempts\"}",
  "Room live connection capacity = 12; over-cap WebSocket closes with StatusPolicyViolation before presence",
  "POST /api/rooms -> 201 {id,joinToken} | 404 media error | 429 {\"error\":\"room limit reached\"}",
  "WS start{mediaId} mutates activity only when mediaId == Room.mediaID; mismatch -> error",
]
produces = []

[[criteria]]
text = "weak seed refuses startup; strong seed starts and existing databases restart without seed variables"
layer = "e2e"
gate = 1

[gate]
n = 1
release = false
launch = "./build.sh; run ./together on 127.0.0.1:18080 with TOGETHER_DATA from mktemp -d, TOGETHER_ROOM_IDLE=2s, and a 12+ code-point ADMIN_PASS"
seed = "upload a 1-second ffmpeg-generated media fixture through the real admin API inside the disposable data directory"
unglamorous = "send malformed proxy headers and repeated invalid credentials; observe bounded JSON errors and a live process"

[[gate.journey]]
step = "weak seed refuses startup; strong seed starts and existing databases restart without seed variables"
task = 1

[[gate.journey]]
step = "a 32-hex invite registers a member; uniform login failures throttle at the documented boundary while another IP remains available"
task = 2

[[gate.journey]]
step = "12 room sockets appear in presence and a thirteenth is policy-closed without changing presence"
task = 3

[[gate.journey]]
step = "a never-connected room expires, room quotas reject excess creation, and regenerated old/new tokens keep no-oracle behavior"
task = 4

[[gate.journey]]
step = "a guest's unrelated-media start returns an error while the room remains on its fixed media"
task = 5

[[gate.journey]]
step = "malformed proxy headers and repeated invalid credentials return bounded JSON errors while the process remains live"
task = 2
```

**Completion artifact:** Human walkthrough result in task-6 evidence; screenshots optional. Any failed observation becomes a head-of-queue fix task before Task 7.

**Estimated difficulty:** Medium manual gate.

**Context pack:** PLAN Demo Gate 1, SPEC kernel journey, PRD US-1–3/error cases, built binary, `docs/OPERATIONS.md`. No code changes.

> **DONE** — Evidence: `specs/003-security-hardening/evidence/task-6.txt`. Human walkthrough passed; no findings were reported.

## Task 7 — Crystallize Gate 1 as a disposable production journey

Encode the witnessed authentication/room journey so later changes cannot regress it.

```toml
id = 7
type = "crystallization"
chunk = 2
deps = [6]
gate = 1
skeleton = false
files = [
  "scripts/security-e2e.sh",
  "scripts/verify.sh",
]
consumes = [
  "Seed(d *sql.DB, user, pass string) error",
  "POST /api/login -> 200 User | 401 {\"error\":\"invalid username or password\"} | 429 {\"error\":\"too many attempts\"}",
  "POST /api/register -> 200 User | 400 validation error | 429 {\"error\":\"too many attempts\"}",
  "Room live connection capacity = 12; over-cap WebSocket closes with StatusPolicyViolation before presence",
  "POST /api/rooms -> 201 {id,joinToken} | 404 media error | 429 {\"error\":\"room limit reached\"}",
  "WS start{mediaId} mutates activity only when mediaId == Room.mediaID; mismatch -> error",
]
produces = [
  "scripts/security-e2e.sh launches the production entry point only with self-created disposable state",
]

[[criteria]]
text = "the script reproduces every Gate 1 journey step, including malformed proxy input, and exits non-zero on any missing observation"
layer = "e2e"
gate = 1

[[criteria]]
text = "success, assertion failure, and interrupt paths terminate the child server and remove only the script-created temporary directory"
layer = "integration"

[[criteria]]
text = "a contract test invokes scripts/security-e2e.sh and asserts it launches the production entry point only with self-created disposable state"
layer = "contract"
```

**Inputs:** Completed Gate 1; Tasks 1–6.

**Outputs:** Initial `scripts/security-e2e.sh`; optional verify integration when runtime is under 60 seconds.

**Estimated difficulty:** Medium.

**Context pack:** `build.sh`, `scripts/verify.sh`, `cmd/server/main.go`, relevant API contracts in `ARCHITECTURE.md` §4, PLAN Gate 1, task-6 evidence. Backend/journey work; no UI design context.

## Task 8 — Enforce upload request and declared-size boundaries

Restore the documented upload size contract and make every upload body bounded without buffering media in memory.

```toml
id = 8
type = "fix"
chunk = 3
deps = [7]
skeleton = false
files = [
  "internal/media/upload.go",
  "internal/media/upload_test.go",
  "internal/media/pipeline.go",
  "web/src/lib/upload.js",
  "cmd/server/main.go",
]
consumes = []
produces = [
  "POST /api/admin/media {title,origName,sizeBytes} -> 200 {id} | 400 validation error | 413 body too large",
  "PATCH /api/admin/media/{id}/blob?offset=N with Upload-Length -> 200 {size} | 409 offset/total mismatch | 413 chunk too large",
  "TOGETHER_MAX_UPLOAD_BYTES default = 21474836480",
]

[[criteria]]
text = "new 16MiB uploads declare size at creation, accept two 8MiB chunks, report exact resume offsets, and queue only after exact completion"
layer = "integration"

[[criteria]]
text = "4KiB+1 JSON, 8MiB+1 chunk, 10MiB+1 subtitle, writes beyond total, and premature finish return the documented error without committing excess bytes"
layer = "integration"

[[criteria]]
text = "a legacy uploading row with absent size establishes Upload-Length once and rejects a conflicting later value"
layer = "integration"

[[criteria]]
text = "the browser client sends sizeBytes and Upload-Length; pipeline completion still replaces input size with final output size"
layer = "contract"

[[criteria]]
text = "HTTP contract tests drive both produced upload endpoints and the configured maximum, asserting request/response/error shapes exactly"
layer = "contract"

[[criteria]]
text = "bounded resumable upload accepts exact chunks, rejects oversized or overrun input, and survives restart with durable media intact"
layer = "e2e"
gate = 2
```

**Inputs:** Task 7; PRD FR-11–14, AC-4.1–4.4, NFR-3; confirmed unbounded-upload finding.

**Outputs:** Server-enforced upload limits, legacy resume path, client header/body contract, tests.

**Estimated difficulty:** Medium–high.

**Context pack:** `internal/media/upload.go`, `internal/media/upload_test.go`, `internal/media/pipeline.go`, `web/src/lib/upload.js`, `cmd/server/main.go`, PRD upload sections, `ARCHITECTURE.md` §4.2/§8–9, `CONVENTIONS.md`. UI logic only; no visual components or DESIGN.md.

## Task 9 — Extend the production proof and synchronize living contracts

Extend the journey through upload/restart behavior and make operator/architecture documentation describe only implemented controls.

```toml
id = 9
type = "proof"
chunk = 4
deps = [8]
skeleton = false
production_composition_proof = true
files = [
  "scripts/security-e2e.sh",
  "scripts/verify.sh",
  "ARCHITECTURE.md",
  "CONVENTIONS.md",
  "docs/HARDENING.md",
  "docs/OPERATIONS.md",
  "README.md",
]
consumes = [
  "scripts/security-e2e.sh launches the production entry point only with self-created disposable state",
  "POST /api/admin/media {title,origName,sizeBytes} -> 200 {id} | 400 validation error | 413 body too large",
  "PATCH /api/admin/media/{id}/blob?offset=N with Upload-Length -> 200 {size} | 409 offset/total mismatch | 413 chunk too large",
  "TOGETHER_MAX_UPLOAD_BYTES default = 21474836480",
]
produces = [
  "scripts/security-e2e.sh covers the complete security kernel journey through the production composition",
]

[[criteria]]
text = "the production journey covers seed, invite, rate, connection, room quota/expiry, token, media scope, bounded upload, restart, and durable-state observations twice consecutively"
layer = "integration"

[[criteria]]
text = "injected assertion failure and SIGINT leave no server process, listener, or disposable data directory behind"
layer = "integration"

[[criteria]]
text = "ARCHITECTURE, CONVENTIONS, HARDENING, OPERATIONS, and README state the implemented limits, proxy trust, env configuration, errors, and test layers without contradicting SPEC/PRD"
layer = "contract"

[[criteria]]
text = "a contract invocation asserts scripts/security-e2e.sh covers the complete security kernel journey through the production composition"
layer = "contract"

[[criteria]]
text = "the complete security journey passes through the production binary and living documentation matches each observed control"
layer = "e2e"
gate = 2
```

**Inputs:** Tasks 7–8; PLAN Chunk 4; PRD FR-15–16/NFR-4–5.

**Outputs:** Complete production proof, verify integration/skip rule, patched living contracts, Decision log entry.

**Estimated difficulty:** Medium.

**Context pack:** Task 7 script, Task 8 diff/evidence, all files listed above, SPEC/PRD/PLAN, `ARCHITECTURE.md` §2/§4/§5/§8–10, current operations/hardening docs. No UI changes.

## Task 10 — Release Gate: hardened production composition

Run the release build and security exit bar. Do not implement fixes inside this task; route failures back to new fix tasks.

```toml
id = 10
type = "gate"
chunk = 4
deps = [1, 2, 3, 4, 5, 7, 8, 9]
skeleton = false
files = [
  "specs/003-security-hardening/evidence/task-10.txt",
  "specs/003-security-hardening/reviews/",
  "specs/003-security-hardening/screenshots/",
]
consumes = [
  "scripts/security-e2e.sh covers the complete security kernel journey through the production composition",
]
produces = []

[[criteria]]
text = "the complete security journey passes through the production binary and living documentation matches each observed control"
layer = "e2e"
gate = 2

[gate]
n = 2
release = true
launch = "./scripts/release.sh; extract the host-architecture archive and run its together binary on loopback with TOGETHER_DATA from mktemp -d"
seed = "scripts/security-e2e.sh creates the tiny ffmpeg fixture, disposable credentials, account, media, and room state"
unglamorous = "interrupt an oversized upload and terminate mid-journey; rerun and verify no overrun bytes, stuck listener, leaked child, or corrupted durable state"

[[gate.journey]]
step = "weak seed refuses startup; strong seed starts and existing databases restart without seed variables"
task = 1

[[gate.journey]]
step = "a 32-hex invite registers a member; uniform login failures throttle at the documented boundary while another IP remains available"
task = 2

[[gate.journey]]
step = "12 room sockets appear in presence and a thirteenth is policy-closed without changing presence"
task = 3

[[gate.journey]]
step = "a never-connected room expires, room quotas reject excess creation, and regenerated old/new tokens keep no-oracle behavior"
task = 4

[[gate.journey]]
step = "a guest's unrelated-media start returns an error while the room remains on its fixed media"
task = 5

[[gate.journey]]
step = "bounded resumable upload accepts exact chunks, rejects oversized or overrun input, and survives restart with durable media intact"
task = 8

[[gate.journey]]
step = "the complete security journey passes through the production binary and living documentation matches each observed control"
task = 9

[[gate.journey]]
step = "interrupt an oversized upload and terminate mid-journey; rerun with no overrun bytes, stuck listener, leaked child, or corrupted durable state"
task = 9
```

**Completion artifact:** Release-gate evidence includes `./scripts/verify.sh`, `go test -race ./...`, frontend tests/build, `npm audit`, official verbose `govulncheck`, `git diff --check`, the security journey, and the independent Phase 6a security review. Any unresolved `GATE BLOCKED` fails completion.

**Estimated difficulty:** Medium release gate.

**Context pack:** release script/build artifacts, complete security journey, all task evidence, SPEC kernel journey, PRD NFR measurements, living architecture/operations docs. No implementation edits.
