---
status: gate-passed
profile: full
---

# Security hardening

## Core promise

A compromised guest, ordinary member, malformed client, or deployment mistake must not cross its authorization boundary or exhaust the small server through an unbounded operation.

## Kernel

1. Fail-closed initial administrator provisioning and brute-force-resistant account entry.
2. Enforced, race-safe limits for authentication attempts, live room connections, and room creation.
3. Room-scoped WebSocket authorization: a participant can control only the room's fixed media.
4. Bounded media-upload requests and declared total upload size.
5. Regression evidence that drives the production composition and preserves existing login, invite, room, sync, and upload journeys.

### Kernel journey

Start the production binary with disposable storage → verify weak or missing initial-admin credentials stop startup → start with a strong administrator credential → create a long random account invite and register a member → verify repeated bad authentication is throttled without distinguishing unknown users → create a room and join as a guest → verify the twelfth live connection is the last admitted connection → verify the guest cannot start unrelated media → create rooms without connecting and observe idle cleanup and room quotas → upload a declared-size media file in bounded chunks and reject oversized or overrun requests → restart and verify accounts, sessions, invites, and media remain available while ephemeral rooms disappear.

## v1 scope

- Resolve all seven findings from the 2026-07-18 security review.
- Keep the existing Go/Svelte monolith, SQLite data model, session-cookie model, public guest-link flow, and local-file-first playback.
- Preserve two host tabs, reconnecting guest identity, the 12-live-connection room capacity, multi-GB resumable uploads, and current public API paths.
- Add no runtime dependency.

## Acceptance summary

- First boot cannot create an administrator with a missing or shorter-than-12-character password.
- Account invites contain at least 128 random bits.
- Login and registration throttles are deterministic, bounded in memory, proxy-aware only from loopback, and return JSON `429` with `Retry-After`.
- Unknown-username and wrong-password login paths both perform one Argon2 verification.
- A thirteenth live WebSocket never enters room presence; never-connected rooms expire; one owner may hold at most 10 rooms and the process at most 100.
- Join-token lookup is indexed rather than a public linear scan.
- A room participant cannot start a ready media ID other than the room's fixed media.
- Upload JSON, chunks, subtitle bodies, declared size, and final byte count are server-enforced.
- Existing verification, race tests, production build, and the new security journey are green.

## Backlog

1. Expiring and explicitly revocable account invites.
2. Distributed rate limiting for multi-instance deployments.
3. Per-account session management and forced session revocation.
4. Filesystem-wide media quota with reserved-space accounting across concurrent uploads.

## Edge cases and constraints

- Existing accounts, sessions, invite codes, media rows, and room links retain their current wire shapes.
- Rate-limit state is process-local and may reset on restart; deployments remain single-instance.
- Existing uploads created before this change may resume; the migration path must not strand them.
- Security responses never reveal filesystem paths, credentials, password validity, or whether a probed username exists.
- Design system: existing `design.md`/`DESIGN.md`; no visual redesign. Any new error state uses existing Alert/Input/Button patterns and WCAG AA behavior.

## Out of scope

CAPTCHA, email verification, password reset, account lockout requiring administrator intervention, multi-node coordination, malware scanning, ffmpeg sandbox redesign, and operating-system/Caddy patch management.
