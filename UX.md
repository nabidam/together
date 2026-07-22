# UX.md — Together V2 (Ephemeral Rooms + Local Playback)

Living document. Screens, wireframes, and flows for the V2 refactor per `specs/001-core/SPEC.md`. No visual styling here — all tokens come from `design.md` (NxCode); this file defines structure, hierarchy, and behavior only.

Two user classes: **account users** (sign in; any of them can host; admins additionally upload/manage) and **guests** (no account; enter via invite link, exist only for the life of one room).

---

## 1. Screen inventory

| Id | Screen | Purpose | Entry points |
|----|--------|---------|--------------|
| S1 | Login | Account user signs in | `#/` unauthenticated; logout; expired session |
| S2 | Register | New account via invite code | Link from S1; `#/register` |
| S3 | Home (Rooms) | See live rooms, create a room, reach Admin | Successful login; `#/` authenticated; leaving a room |
| S4 | Room — Video | The theater: synced video, chat, participants, media acquisition | Create room (S3); join live room (S3); guest join (S6); reconnect via room URL |
| S5 | Room — Audio | Now-playing variant of S4 for `kind: audio` media | Same as S4 when the room's media is audio |
| S6 | Guest Join | Guest opens invite link, picks a display name | `#/join/{token}` — the only pre-auth route besides S1/S2 |
| S7 | Admin | Upload media, manage library, manage invites | Admin-only link in S3 header; `#/admin` |
| M1 | Media Picker (modal) | Host picks or proposes a ready library item from inside a room | "Choose media" / "Change media" in S4/S5 |
| M2 | End Room (confirm modal) | Host confirms explicit teardown | "End room" in S4/S5 host controls |
| M3 | Regenerate Link (confirm modal) | Host confirms revoking the current invite link | "Regenerate" next to invite link in S4/S5 |
| M4 | Play From Server (confirm modal) | Explicit opt-in to streaming fallback | "Play from server instead" in acquisition panel |
| V1 | Room Closed (terminal view) | Tells participants the room ended | `room_closed` broadcast while in S4/S5; opening a dead room/invite link |

Size-mismatch warning is **not** a modal — it is an inline error state of the acquisition panel in S4/S5 (see §3.4). Kicking, co-host, canvas: backlog, not drawn.

---

## 2. Navigation map

```
                       #/join/{token}
                             │
                             ▼
        ┌─────────────── S6 Guest Join ──(name ok)──────┐
        │                                                ▼
S1 Login ──(auth ok)──► S3 Home ──(create)──► S4/S5 Room ──(host: M1 choose media)──► S4/S5 Room
   ▲  │                    │  ▲        (join live room)     │                              │
   │  └──► S2 Register ────┘  │                             │ (leave / guest closes tab)   │ (guest: dead end;
   │        (invite code)     └─────────────────────────────┘                              │  account: → S3)
   │                          │
   └── (logout) ◄─────────────┤
                              └──(admin only)──► S7 Admin ──(back)──► S3
```

- Account users always land on S3 after auth; room entry is S3 → S4/S5.
- Guests never see S1/S2/S3/S7. Their world is S6 → S4/S5 → V1.
- `#/join/{token}` renders **before** the auth gate (anonymous visitors short-circuit ahead of `/api/me`).
- Reconnect (both classes): reopening the room URL / invite link while the room lives re-enters S4/S5 directly with state restored; no intermediate screen.

---

## 3. Wireframes

Glyphs inside wireframes (⬇ 📂 🔍 ▸ ●) are ASCII/emoji stand-ins for lucide icons and NxCode mono glyphs per `design.md` — the shipped UI uses no emoji.

### S1 — Login

```
┌────────────────────────────────────────┐
│                                        │
│              [ wordmark ]              │
│                                        │
│   Username  [____________________]    │
│   Password  [____________________]    │
│                                        │
│           (  Sign in  )  ← primary     │
│                                        │
│      Have an invite code? Register     │
│                                        │
└────────────────────────────────────────┘
```

- Eye goes first to the username field (auto-focused).
- **Error:** inline message above the button ("Wrong username or password."); fields keep their values.
- **Loading:** button shows busy state, form disabled.
- **Empty:** n/a (form is the empty state).

### S2 — Register

Same centered single-column layout as S1: Invite code, Username, Password, (Create account), link back to Sign in. Errors inline: "Invite code invalid or already used." / "Username taken." A failed attempt must visibly not consume the code (the same code can be retried).

### S3 — Home (Rooms)

```
┌──────────────────────────────────────────────────────┐
│ [wordmark]                    [Admin*] [username ▾]  │  ← header; * admin only
├──────────────────────────────────────────────────────┤
│  Live rooms                        ( + Create room ) │  ← primary action
│  ┌────────────────────────────────────────────────┐  │
│  │ ▸ "Movie Night"  ·  Alien (1979)  ·  3 watching│  │  ← click row → S4/S5
│  ├────────────────────────────────────────────────┤  │
│  │ ▸ "listening"    ·  OK Computer  ·  1 listening│  │
│  └────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────┘
```

- Eye goes first to the room list; primary action is **Create room** (top right of the list region).
- Each row: room name, media title or "No media selected", participant count, media-kind icon. Whole row is the click target (≥44px tall).
- **Empty:** "No rooms right now." + centered Create room button — creating a room *is* the empty state's call to action.
- **Loading:** skeleton rows.
- **Error:** inline retry banner ("Couldn't load rooms. Retry").
- Rooms are ephemeral: this list shows only live rooms. There is no history, deliberately.

### M1 — Media Picker (modal in S4/S5)

```
┌─ Create room ────────────────────────────┐
│ Room name  [___________________]         │
│                                          │
│ Pick media                    [filter 🔍]│
│ ┌──────────────────────────────────────┐ │
│ │ ◉ Alien (1979)        video · 8.2 GB │ │
│ │ ○ OK Computer         audio · 96 MB  │ │
│ │ ○ …                                  │ │
│ └──────────────────────────────────────┘ │
│                    (Cancel) ( Create )   │
└──────────────────────────────────────────┘
```

- Single-select list of **ready** library items only (in-flight pipeline jobs are excluded). Shows title, kind, exact human-readable size (size matters to the user — they will download it).
- Create room asks only for optional room name; it enters S4/S5 without media.
- Host opens this picker with **Choose media** or **Change media**. Selecting first media applies immediately if alone. Changing existing media sends every other participant a confirmation dialog; switch occurs only after every required participant confirms and resets playback.
- **Empty:** "Library is empty." + for admins, a link to S7; for non-admin members, "Ask your admin to upload something."

### S4 — Room (Video)

The theater. Player dominates; everything else is subordinate or collapsible.

```
┌───────────────────────────────────────────────┬──────────────┐
│ ‹ Leave   "Movie Night" · Alien (1979)   HOST │  Side panel  │
│           strip (thin, auto-hides in play)    │ ┌──────────┐ │
│                                               │ │Participants│
├───────────────────────────────────────────────┤ │ ● Sam HOST │
│                                               │ │ ◐ Ali (2) │ │
│                                               │ │  dots: see │ │
│              VIDEO PLAYER                     │ │  ladder ↓  │ │
│        (or acquisition panel until            │ ├──────────┤ │
│         a local file is loaded — §3.4)        │ │   Chat    │ │
│                                               │ │  msg      │ │
│                                               │ │  msg      │ │
│                                               │ │  msg      │ │
├───────────────────────────────────────────────┤ │ [type…] ⏎│ │
│ ▶/⏸  ──────●───────────  47:12 / 1:57:03  CC ⛶│ └──────────┘ │
└───────────────────────────────────────────────┴──────[⇥]────┘
```

Regions, top to bottom / start to end:

1. **Room strip** (top, thin): Leave is available to account users and guests; a confirmation explains that a final-tab leave pauses playback, then returns account users to S3 and guests to entry. Room name + media title, host badge. A host sees Resume while playback is paused. **Host controls live here behind a single "Room" disclosure menu:** copy invite link, regenerate link (→ M3), end room (→ M2). Guests see only the room name.
2. **Player region** (dominant, ~80% width when panel open, 100% when collapsed): the `<video>` element once a local file is loaded and armed; the **acquisition panel** (§3.4) before that. Until first user gesture, an **arm overlay** sits on the player: big play glyph + "Click to enable playback" (browser autoplay policy). Subtitle track selectable via CC in transport.
3. **Transport bar**: play/pause, scrub bar, position/duration, CC, fullscreen, panel toggle. All actions send intents — the UI reflects only the echoed server state (a click that doesn't round-trip visibly does nothing; that is correct, not a bug).
4. **Side panel** (end, collapsible, remembers state): Participants block on top (short, fixed), Chat fills the rest. Collapse control on the panel edge.

**Participant status dot ladder** (advisory, per SPEC §9.4): `◌ Downloading` → `◐ File Ready` → `● In Sync`. Dot + name (+ `(2)` suffix if collided) + HOST badge. A tooltip on the dot spells out the state name. Host sees no gate — the ladder is information, not a lock.

- Eye goes first to the player region (or the acquisition panel occupying it).
- **Loading (join):** brief full-region spinner until `hello` arrives.
- **Error (WS lost):** thin reconnecting banner over the room strip ("Reconnecting…"); inputs stay visible but intents/chat are disabled until reconnected. A participant whose final socket drops shows Reconnecting in the list for 10 seconds; playback continues. On return, one "Partner rejoined." toast appears and the host can Resume.
- **Participant leaves:** an explicit final-tab Leave pauses active playback for remaining participants; one bottom-end toast reads "User Left.".
- **Playback feedback:** server-confirmed play, pause, and seek actions show a single toast naming actor and action. If host starts playback while anyone is Downloading, confirmation explains that partner will sync when ready.
- **Empty (chat):** plain "No messages yet." (no emoji in chrome).
- **Mid-movie join:** identical layout; player armed-empty + own Downloading dot while the acquisition panel is up. No special casing.

### §3.4 — Acquisition panel (state of S4/S5 player region, not a screen)

Occupies the player region until a size-matched local file is loaded.

```
┌────────────────────────────────────────────────┐
│        Alien (1979) · 8.2 GB · video           │
│                                                │
│   ( ⬇ Download from server )  ← saves to disk │
│                                                │
│   ( 📂 Load your copy )       ← file picker    │
│                                                │
│   After downloading, load the saved file here. │
│                                                │
│   Play from server instead ›  ← quiet, last    │
└────────────────────────────────────────────────┘
```

- Two peer primary paths, load-own-copy listed second but equally weighted (SPEC makes load-own-copy primary in spirit: the download button's helper text funnels users back to the picker).
- Download = plain authed GET (`Content-Disposition: attachment`); the browser owns progress/resume UI. The panel stays up; user re-selects the saved file via **Load your copy**.
- **File selected, size matches:** panel swaps to the armed player (arm overlay). Status dot → File Ready.
- **File selected, size mismatch — the inline error state:**

```
│  ⚠ That file doesn't match.                    │
│  Selected: alien-dc.mkv · 9.1 GB               │
│  Expected: 8.2 GB                               │
│  ( Choose a different file )                    │
```

  Warn + block: no player, dot stays Downloading. No modal; the error lives where the fix is.
- **Play from server instead** (deliberately quiet, bottom): opens M4.

### M4 — Play From Server (confirm modal)

"Stream from the server? Playback quality depends on the server's small connection. Local files are always smoother." — (Cancel) / (Stream anyway). On confirm: player uses the streaming URL, dot ladder proceeds as normal. Never automatic; this modal is the only door.

### S5 — Room (Audio)

Same skeleton as S4; the player region becomes a now-playing panel instead of video.

```
┌───────────────────────────────────────────────┬──────────────┐
│ ‹ Leave   "listening" · OK Computer     HOST  │  Side panel  │
├───────────────────────────────────────────────┤  (identical  │
│                                               │   to S4)     │
│              ♪  OK Computer                   │              │
│                 Radiohead — 53:21             │              │
│         (acquisition panel here first,        │              │
│          same states as §3.4)                 │              │
│                                               │              │
├───────────────────────────────────────────────┤              │
│ ▶/⏸  ──────●───────────  12:04 / 53:21      ⇥│              │
└───────────────────────────────────────────────┴──────────────┘
```

- Transport identical minus CC/fullscreen. Title + duration are the visual anchor. Chat/participants unchanged — for audio rooms the side panel defaults **open** (the conversation is half the point).

### S6 — Guest Join

```
┌────────────────────────────────────────┐
│              [ wordmark ]              │
│                                        │
│   You're invited to "Movie Night"      │
│                                        │
│   Your name  [__________________]      │
│                                        │
│            (  Join room  )             │
│                                        │
└────────────────────────────────────────┘
```

- Eye: the name field (auto-focused). One field, one button — nothing else.
- Room name shown if the token resolves; validates on submit.
- **Error (dead/regenerated link):** the form is replaced: "This invite link isn't valid anymore. Ask your host for a new one." No retry button (retry can't help).
- **Error (name):** inline — "Name can't be empty." / "Name too long (max 32)." Collision is silent: server suffixes `(2)` and the guest sees their suffixed name in the participant list.
- **Error (room full):** "This room is full." — terminal, same treatment as dead link.
- On success → S4/S5. A guest with a live guest cookie reopening the link skips the form entirely and re-enters the room as the same participant.

### S7 — Admin

Carried over from V1 with one addition (kind column). Regions: header with back-to-Home; **Upload** card (file drop + resumable progress — continuation survives reload); **Library** table (title, kind `video|audio`, size, status `processing|ready|failed`, subtitles count, delete with confirm); **Invites** card (generate code, list unused codes). 

- **Empty library:** "Nothing uploaded yet." under the upload card — the upload card is the empty state's action.
- **Job states:** processing rows show a spinner + stage text; failed rows show the error and a delete affordance.
- **Error:** per-card inline banners, never full-page.

### V1 — Room Closed

Full-viewport terminal state replacing S4/S5:

```
│        This room has ended.            │
│   Chat and playback are gone — that's  │
│   by design.                           │
│        ( Back to rooms )  ← account    │
│        (none for guests — dead end)    │
```

Guests keep their downloaded file; nothing else persists for them.

---

## 4. Key flows

### F1 — Kernel journey (host + guest, first watch)

1. Host sees S1, signs in → lands on S3.
2. Host clicks **Create room** → sees M1, picks *Alien (1979)*, clicks Create → system creates the room and drops the host into S4; player region shows the acquisition panel; participant list shows `● Sam HOST` (host loads own copy → In Sync at their own pace).
3. Host opens the **Room** menu in the room strip, clicks **Copy invite link** → system confirms ("Link copied"); host sends it to partner out-of-band.
4. Partner opens the link → sees S6 with "You're invited to 'Movie Night'", types `Ali`, clicks **Join room** → system mints a guest session and shows S4; participant list on both screens now shows Ali with a `◌ Downloading` dot.
5. Ali clicks **Download from server** → browser downloads the file to disk (panel remains, browser shows progress). Download finishes; Ali clicks **Load your copy**, selects the saved file → system checks byte size, it matches → panel swaps to the armed player with the arm overlay; Ali's dot flips to `◐ File Ready` on everyone's participant list.
6. Ali clicks the arm overlay (user gesture) → player is live; dot may flip to `● In Sync` once tracking.
7. Host presses **play** → intent goes to server → server broadcasts state → both `<video>` elements start within the drift bands. Play/pause/scrub by *either* side behaves the same (echo-driven).
8. Ali scrubs to 47:12 → both players jump to 47:12.
9. Ali's connection drops → Ali's S4 shows the reconnecting banner → Ali reopens the invite link → guest cookie recognized, S6 skipped, S4 restores; `hello` carries full state; player seeks to the projected mid-scene position; same name, no `(2)`.
10. They chat in the side panel about the ending.
11. Host opens Room menu → **End room** → M2 confirm → server broadcasts `room_closed` → both see V1. Host clicks **Back to rooms** → S3. Library and accounts remain.

### F2 — Load own copy (no download)

1. Guest (or host) in S4 sees the acquisition panel, already owns the file.
2. Clicks **Load your copy**, picks their local file → size matches → armed player, dot → File Ready. (Mismatch → inline warn/block per §3.4, chooses again.)
3. Clicks arm overlay → In Sync. Total: two clicks + a file dialog.

### F3 — Music room

1. Host on S3 → Create room → M1, picks *OK Computer* (audio) → S5.
2. Both acquire the file exactly as in F1/F2 (same acquisition panel, same size check, same dots).
3. Host presses play → synced audio; side panel is open by default; they chat while listening.

### F4 — Host disconnect + reclaim

1. Mid-playback, host's laptop dies. Nothing visibly changes for the guest: playback continues (server owns state), host's entry drops from presence.
2. Guest can still pause/scrub/chat (intents are not host-gated).
3. Host reopens the room from S3 → re-enters S4, HOST badge back, Room menu available again. No transfer ever happened; if all connections had dropped instead, the room would have quietly closed 30 min later and both would find it gone (V1 state / S3 without the room).

---

## 5. Density & hierarchy notes

- **S3:** one click from anything that matters — enter a room (row), create a room (button), Admin (header, admins only). Logout is buried one level in the username menu. No settings screen exists.
- **M1:** picking media is the whole job; room name is present but skippable (smart default). Filter input only pulls its weight past ~8 library items — render it always, it's one row.
- **S4/S5:** the player owns the viewport. One click away: play/pause, scrub, panel toggle, chat input, arm overlay. **Deliberately buried one disclosure deep (Room menu):** copy link, regenerate (→ M3), end room (→ M2) — host-only, used once or twice per session, and two of the three are destructive-ish; they must not sit next to play/pause. CC and fullscreen live in the transport (standard video grammar).
- **Acquisition panel:** two big peer actions; the streaming fallback is intentionally the quietest element on the panel — visible enough to find when needed, quiet enough to never be the default path.
- **Participant dots:** glanceable, never interactive in v1 (kick/promote are backlog). Meaning is one hover away (tooltip), never required reading.
- **S6:** zero density — a name field and a button. Everything a guest needs to learn about the app they learn inside the room.
- **S7:** admin is a workbench, allowed to be denser: table + two cards on one screen, no tabs. Destructive delete is behind a confirm.
- **V1 (closed):** dead ends are honest — guests get no navigation because there is nowhere for them to go.
