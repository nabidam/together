---
status: gate-passed
---

# PRD.md — Together V2: Ephemeral Rooms + Local Playback

Product requirements for the V2 refactor. Sources: `specs/001-core/SPEC.md`, `UX.md` (screen ids S1–S7, M1–M4, V1 referenced throughout). This document describes behavior only; implementation lives in `ARCHITECTURE.md`.

## 1. Product summary

A private watch/listen-together app for a couple and close friends (≤10 concurrent users). One partner (an account user) hosts a room around a media item from the persistent library; the other joins via a link with no account, plays a **local copy** of the file, and the server keeps everyone in perfect sync while sending zero media bytes during playback. Rooms, chat, and presence are ephemeral — they exist only while the room is open.

## 2. Scope split (binding — do not promote)

- **Kernel:** guest link access, local-file playback, server-authoritative sync, room-lifetime chat, explicit room lifecycle.
- **v1:** kernel + music (audio) rooms + admin upload pipeline (carried over, extended for audio).
- **Backlog (ranked, out of this cycle):** collaborative canvas, kick user, co-host promotion, emoji picker, content-hash file matching, guest link expiry options.

## 3. User roles

| Role | Identity | Can |
|------|----------|-----|
| Admin | Account (invite-registered) | Everything a member can + upload media, delete media, issue invite codes |
| Member | Account (invite-registered) | Sign in, create/host rooms, join any live room, watch, chat |
| Guest | No account; display name only | Join one specific room via its invite link, acquire media, watch, chat, control playback |

## 4. Functional requirements

### 4.1 Guest room access

- **FR-1** A room has exactly one reusable invite link at any time, valid while the room is open.
- **FR-2** Opening a valid invite link shows a join screen (S6) asking only for a display name; submitting it puts the guest in the room (S4/S5) with no account, password, or email.
- **FR-3** Display names: required, ≤32 characters, control characters stripped, not empty after stripping.
- **FR-4** If the chosen name matches a currently-connected participant's name, the newcomer's name gets a numeric suffix — `(2)`, then `(3)`. A departed participant's name becomes available again.
- **FR-5** A guest who disconnects and reopens the invite link (same browser) re-enters as the **same** participant: same name, no new suffix, no duplicate presence entry.
- **FR-6** The host can regenerate the invite link. The old link stops working for **new** joins immediately; guests already in the room stay connected.
- **FR-7** An invite link for a closed room, or a regenerated-away link, leads to a terminal "invite not valid" message (S6 error state) — no name form.
- **FR-8** Guests can never reach account-holder surfaces: home (S3), admin (S7), room creation, or the library list.

### 4.2 Local-file playback

- **FR-9** Until a participant has a valid local file loaded, the player region shows an acquisition panel (UX §3.4) offering: (a) download the room's media from the server, (b) load a copy they already have, and (c) a deliberately quiet "play from server" fallback.
- **FR-10** Download is a normal browser file download saved to disk (resumable/pausable by the browser). The app never buffers the media file in page memory; after downloading, the user loads the saved file via the file picker.
- **FR-11** On file selection, the app compares the selected file's byte size to the room media's byte size. Exact match → the player arms and the participant becomes File Ready. Mismatch → inline warning showing selected vs. expected size, player blocked, participant stays Downloading.
- **FR-12** Playback of a loaded local file starts only after an explicit user gesture on an arm overlay (browser autoplay compliance).
- **FR-13** "Play from server" requires explicit confirmation (M4) each room-entry; it is never the automatic fallback for any failure.
- **FR-14** Subtitles for video media are served by the server and selectable in the transport (CC), for both local-file and streamed playback, for both guests and account users.
- **FR-15** Media can be downloaded while a session is in progress (late joiner) with no special handling; the joiner's status dot shows Downloading while others keep watching.

### 4.3 Server-authoritative sync

- **FR-16** Play, pause, and seek are **intents**: any participant (host or guest) can issue them; the server applies each intent and broadcasts one absolute playback state to everyone; every participant's player follows the broadcast, including the intent's author.
- **FR-17** During playback the server sends only state/chat/presence messages — zero media bytes to any participant playing a local file.
- **FR-18** Drift correction: a client >1s from the projected position hard-seeks; a client >0.15s off nudges playback rate; within 0.15s it plays untouched.
- **FR-19** Readiness is advisory. The server never blocks or delays a play intent because some participant lacks a file; laggards sync from the current projected position once ready.
- **FR-20** A participant who joins or reconnects receives the complete current room state in one message — playback position/paused state, participant list with statuses, and chat history — with no event replay.

### 4.4 Participant presence & status

- **FR-21** Every participant appears in the participant list with name, host badge (if host), and a status dot: Downloading → File Ready → In Sync.
- **FR-22** Status is self-reported by each client as its local state changes and is broadcast to all participants.
- **FR-23** The host badge is computed from identity (room creator's account), not stored per connection; the host reclaims it automatically on rejoin. Two simultaneous host connections may coexist.

### 4.5 Chat

- **FR-24** Room-lifetime text chat in the side panel; messages carry display name, guest marker, and timestamp.
- **FR-25** Chat history is capped at the most recent 200 messages per room (oldest dropped); joiners/reconnecters receive whatever the cap holds.
- **FR-26** Chat dies with the room. No chat persists anywhere after room close.
- **FR-27** Emoji entered as text render in messages; there is no picker (backlog).

### 4.6 Room lifecycle

- **FR-28** Any account user can create a room (S3 → M1): pick one **ready** library item, optionally name the room (defaults to the media title).
- **FR-29** The home screen (S3) lists live rooms only — name, media title, kind, participant count. No room history exists.
- **FR-30** The host can end the room explicitly (M2 confirm) at any time regardless of who is connected; all participants immediately see the Room Closed view (V1) and lose chat/playback.
- **FR-31** A room with zero open connections for 30 continuous minutes closes itself identically to FR-30. Any join/reconnect during those 30 minutes cancels the countdown. A disconnected guest whose session could still resume does **not** keep the room alive.
- **FR-32** Host disconnect does not end, pause, or transfer the room; playback and guest control continue.
- **FR-33** Room close destroys: chat, presence, guest sessions, the invite link, playback state. It never touches: the media library, accounts, account sessions, invite codes.
- **FR-34** Per-room participant cap of 12; joins beyond it are rejected with a terminal "room is full" message (S6 error state).

### 4.7 Music (v1)

- **FR-35** Audio items in the library create audio rooms (S5): now-playing panel (title, duration) instead of video, transport without CC/fullscreen, side panel open by default.
- **FR-36** Acquisition, size check, arming, sync, drift correction, status dots, and chat behave identically to video rooms.
- **FR-37** The library and media picker (M1, S7) label every item `video` or `audio`.

### 4.8 Accounts & admin (carried over from V1)

- **FR-38** Account registration requires a single-use invite code; a failed registration attempt does not consume the code.
- **FR-39** Admins upload media through a resumable chunked upload that survives page reload (continuation resumes where it left off).
- **FR-40** Uploaded video is processed into a web-playable format; uploaded audio likewise via an audio path. Processing runs in the background, one job at a time; the library row shows processing/ready/failed.
- **FR-41** Admins can upload `.srt` subtitles per video item (converted for web display) and delete any library item (with confirm).
- **FR-42** Admins can generate and list unused invite codes.

## 5. Non-functional requirements

- **NFR-1** Runs on a 2 vCPU / 2 GB RAM VPS serving ≤10 concurrent users without degradation.
- **NFR-2** No live transcoding, ever; media processing happens only at ingest, one job at a time, at low CPU priority.
- **NFR-3** During synced playback of local files, per-participant server traffic is state-only (order of bytes/sec, not KB/s).
- **NFR-4** Loading a multi-GB media item must not grow browser tab memory by the media's size (no in-memory buffering).
- **NFR-5** Server crash/restart may lose all live rooms and chat (accepted); it must never lose library, accounts, sessions, or invite codes; the app must come back serving S1/S3 normally.
- **NFR-6** Invite links are unguessable (≥128-bit random); besides login/register, the join surface (join submit + token peek) is the only one reachable without a session.
- **NFR-7** One misbehaving room must not crash the whole service.
- **NFR-8** A11y floors (from `design.md`): text contrast ≥ 4.5:1, touch targets ≥ 44px, body text ≥ 15px, visible focus rings, `prefers-reduced-motion` respected. Dark theme only.
- **NFR-9** Sync quality: two clients on residential connections stay within the FR-18 bands during steady playback; a seek lands both within ~1s of each other.

## 6. User stories with acceptance criteria

All criteria are falsifiable in a running deployment with two browsers (or browser + incognito).

### US-1 Guest joins with a link (kernel)

*As the partner without an account, I open the link, type my name, and I'm watching.*

- **AC-1.1** Open a room's invite link in a browser with no session: a join form (S6) with a single name field appears — no login form, no password field.
- **AC-1.2** Submit name "Ali": the room screen appears; both browsers' participant lists show "Ali" within 2 seconds.
- **AC-1.3** Submit an empty or 33+ character name: an inline error appears and the guest is not in the room.
- **AC-1.4** With "Ali" connected, join from a third browser as "Ali": the newcomer appears as "Ali (2)" in every participant list.
- **AC-1.5** Kill the guest's network for ~10s, restore, reopen the invite link in the same browser: no name form appears; the room screen returns; the participant list still shows exactly one "Ali".
- **AC-1.6** Host regenerates the link; open the **old** link in a fresh browser: terminal "invite not valid" message, no name form. The already-joined guest is still connected and can still chat.
- **AC-1.7** Open the **new** link in a fresh browser: join works.
- **AC-1.8** As a guest, navigate to `#/` or `#/admin` by URL: no room list or admin UI renders and no library/account data is visible.

### US-2 Local-file playback (kernel)

*As a participant, I play my own copy of the file so the server never streams to me.*

- **AC-2.1** Enter a room without a loaded file: the acquisition panel is visible where the player would be, offering Download and Load-your-copy.
- **AC-2.2** Click Download: the browser starts a normal file download of the full media file; the page remains responsive; tab memory (browser task manager) does not grow with the download.
- **AC-2.3** Select the downloaded file via Load your copy: the panel is replaced by the player with an arm overlay; this participant's dot turns File Ready in **both** browsers.
- **AC-2.4** Select a file of the wrong size: an inline warning shows the selected and expected sizes; no player appears; the dot stays Downloading.
- **AC-2.5** After the mismatch, select the correct file: warning clears, player arms.
- **AC-2.6** Click the arm overlay, then play: video/audio plays. During 60s of steady playback, the browser dev-tools network tab shows no media/byte-range requests — only WebSocket frames.
- **AC-2.7** Click "Play from server": a confirmation dialog appears; cancel → nothing changes; confirm → playback works without any local file, and network shows ranged media requests (fallback is real but opt-in).
- **AC-2.8** In a video room, enable CC in the transport: uploaded subtitles display over local-file playback, in the guest's browser too.

### US-3 Synced control (kernel)

*As either participant, my play/pause/seek moves everyone.*

- **AC-3.1** Host presses play: both players are playing within 2s, positions within 1s of each other.
- **AC-3.2** **Guest** presses pause: both players pause. Guest seeks to an arbitrary position: both players end up within 1s of that position.
- **AC-3.3** Host presses play while the guest's dot is still Downloading: playback starts for the host anyway (readiness never gates).
- **AC-3.4** The guest then loads their file and arms mid-scene: their player starts at the current mid-scene position (within the 1s band), not at 0:00.
- **AC-3.5** During playback, briefly cut one client's connection: on reconnect and without any user action beyond reopening the room, its player is back within 1s of the other's position, and prior chat messages are visible.
- **AC-3.6** While disconnected (reconnect banner showing), the client's transport and chat input do not silently swallow input — they are visibly disabled or queued-and-refused.

### US-4 Room-lifetime chat (kernel)

- **AC-4.1** Send a message from each browser: both appear in both side panels, in order, with sender names; guest-sent messages are distinguishable as guest.
- **AC-4.2** Reload the guest's browser and rejoin: previous messages are visible again (up to the cap).
- **AC-4.3** Send 210 messages: only the most recent 200 survive for a fresh joiner.
- **AC-4.4** End the room, create a new room with the same media, rejoin: zero old messages anywhere.

### US-5 Room lifecycle (kernel)

- **AC-5.1** Create a room from home: it appears in the room list (S3) of another signed-in account within one refresh, with media title and participant count.
- **AC-5.2** Host ends the room (with the guest connected): the guest's screen switches to Room Closed (V1) within 2s; the host returns to home; the room is gone from the room list; the library still lists the media.
- **AC-5.3** All participants close their tabs; check back after 30+ minutes: the room is gone from the room list and its invite link is dead. (Time-shortened equivalent acceptable in a test build, but the shipped default must be 30 min.)
- **AC-5.4** Close all tabs, rejoin after 5 minutes: the room is still alive with playback state intact (the countdown reset on rejoin).
- **AC-5.5** Kill the host's tab mid-playback: the guest's playback continues uninterrupted and the guest can still pause/seek/chat. Host reopens the room from home: host badge and Room menu (copy link / regenerate / end) are back.
- **AC-5.6** Restart the server process with a room live: after restart, sign-in works, the library is intact, invite codes are intact — and the room and its chat are gone.

### US-6 Music room (v1)

- **AC-6.1** Upload an audio file as admin: it appears in the library labeled `audio` and becomes ready.
- **AC-6.2** Create a room with it: the room screen is the now-playing layout (S5) — no video surface, no CC/fullscreen controls, side panel open.
- **AC-6.3** Both participants acquire the audio file exactly as in US-2 (same panel, same size check, same dots).
- **AC-6.4** Play/pause/seek from either side syncs both, same bands as AC-3.1/3.2.

### US-7 Admin & accounts (v1, carried over)

- **AC-7.1** Register with a valid invite code → account works; try the same code again → rejected as used.
- **AC-7.2** Submit registration with a valid code but an already-taken username → error; the **same code** then succeeds with a different username (failed attempt didn't burn it).
- **AC-7.3** Upload a large video, reload the page mid-upload, re-drop the same file: upload resumes (visibly does not restart from 0%) and completes; the item becomes ready.
- **AC-7.4** While a video is processing, its library row shows a processing state; when done, ready; a deliberately broken file ends failed with an error shown.
- **AC-7.5** Delete a library item: confirm dialog appears; after confirm the item is gone from library and media picker.

## 7. Validation rules

| Field | Rule | On violation |
|-------|------|--------------|
| Guest display name | Required; strip control chars; 1–32 chars after strip | Inline error on S6; join refused |
| Room name (M1) | Optional; ≤64 chars; defaults to media title | Inline error; create blocked |
| Media selection (M1) | Exactly one **ready** library item | Create button disabled |
| Local file (S4/S5) | `file.size` == room media size, exact bytes | Inline warn with both sizes; player blocked |
| Invite token (join) | Must match a live room's current token | Terminal invalid-link state |
| Room capacity | ≤12 participants | Terminal room-full state |
| Chat message | Non-empty; ≤2000 chars | Send refused, input kept |
| Invite code (register) | Exists, unused | Inline error on S2; code not consumed |
| Username (register) | Non-empty, unique | Inline error on S2; code not consumed |

## 8. Error cases

- **Dead/regenerated invite link** → S6 terminal state, no form, no retry (FR-7).
- **Room ends while participant inside** → V1 Room Closed within 2s; account users get "Back to rooms", guests get a dead end (deliberate).
- **WebSocket drop** → reconnect banner over the room; intents/chat disabled while down; silent full-state restore on reconnect (FR-20); guests resume identity (FR-5).
- **Size mismatch** → inline warn + block, re-pick affordance (FR-11). Never a modal, never auto-falls-back to streaming (FR-13).
- **Server unreachable at join** → S6/S1 show a plain "can't reach server" inline error with retry.
- **Login failure** → inline error, fields preserved (UX S1).
- **Upload/processing failure** → failed state on the library row with the reason; delete is the recovery path.
- **Room list fetch failure** → inline retry banner on S3.
- **Server crash** → rooms/chat lost (NFR-5); users re-land on S1/S3; no corrupted library/accounts.

## 9. Edge cases

- **Mid-movie joiner:** enters with playback running; sees armed-empty player/acquisition panel + own Downloading dot; others unaffected; syncs to projected position when ready (FR-15, AC-3.4).
- **Guest downloads mid-session:** plain download during playback; allowed; no throttling UI (SPEC §4).
- **Host-only room, host leaves:** empty-room countdown arms; host rejoin within 30 min finds it alive (AC-5.4).
- **Two host tabs:** both show host powers; harmless; last-writer intents win like any other intent.
- **Backward clock jumps** on a client must not fling playback (position projection clamps).
- **Name freed by departure:** guest "Ali" leaves (session gone, not mere disconnect); a new "Ali" joins un-suffixed.
- **Empty states:** empty library (M1/S7 per UX), no live rooms (S3), no chat messages, room with only its host.
- **Offline app:** this is an online-only product; there is no offline mode — the reconnect banner and terminal states above are the full story.
- **Reduced motion:** with `prefers-reduced-motion`, panel/overlay transitions are instant; no functional difference.

## 10. Constraints

- 2 vCPU / 2 GB VPS; ≤10 concurrent users; one deployable unit behind a TLS-terminating proxy.
- Backend dependency budget and stack per SPEC §5–6 (binding; details in ARCHITECTURE.md).
- No live transcoding under any circumstance (NFR-2).
- Design tokens exclusively from `design.md` (NxCode); dark only; no emoji in UI chrome; a11y floors NFR-8.
- V1 persistent rooms are **not** migrated; cutover to ephemeral rooms is clean (SPEC §9.11).

## 11. Out of scope

Real-time streaming as the primary path, live transcoding, host auto-transfer, guest accounts, collaborative canvas (backlog #1), kick/co-host (backlog #2/#3), emoji picker (backlog #4), content-hash matching (backlog #5), link expiry options (backlog #6), E2E encryption, mobile apps, >10 users, room history/persistence.

## 12. Future improvements (unranked notes, beyond backlog)

Watch-history "recently watched together", per-room subtitle offset adjustment, download progress reported into the status dot (browser API permitting), soft-pause hint when a participant's dot regresses, multiple audio tracks.
