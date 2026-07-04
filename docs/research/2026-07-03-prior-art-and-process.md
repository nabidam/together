# Together — Research & Process Log

Purpose: reproducible record of the research, decisions, and tooling that produced the design spec. Re-running the steps below should land at the same conclusions or expose what changed.

## 1. Process timeline

| Step | What happened | Output |
|------|--------------|--------|
| 1 | Brainstorming session (superpowers:brainstorming skill) — requirements captured from user brief | Requirements list below |
| 2 | Web research on prior art (queries in §3) | Findings in §4 |
| 3 | Clarifying question: build vs assemble — user away, proceeded with "fully custom" (matches stated goal), flagged as assumption A1 | Spec §2 assumptions |
| 4 | Architecture approaches compared (custom monolith / +Jellyfin / fork OpenTogetherTube) | Spec §4 |
| 5 | Design spec drafted, self-reviewed, committed (`0ed2c26`) | `docs/superpowers/specs/2026-07-03-together-app-design.md` |
| 6 | User reviewed, approved, added: document process, decide stack, use ponytail for implementation, hyper-futuristic minimal UI on `design.md` tokens | This doc + spec updates |
| 7 | ui-ux-pro-max design-system query run (§6) — validated dark/terminal direction; its font/palette suggestions overridden by `design.md` | Spec §10 (UI) |

## 2. Original requirements (user brief, 2026-07-03)

- Private app for a long-distance relationship: watch movies, listen to music, draw together, more activities later.
- VPS: 2 cores, 2 GB RAM. Must be optimized for it. ≤10 users early on.
- Auth + roles: Admin (manages platform, uploads media), Members (use platform).
- Rooms/groups: users create, others join. Inside a room, a user starts an activity; others join and experience it together.
- V1 media: manual upload by Admin (.mp4/.mkv + optional subtitle). Same for music.
- Any participant controls playback (play/pause/seek); actions sync to all.
- Optional: download media before watching for better UX.
- Research similar products, their pros/cons; list fun features.

## 3. Research queries (reproduce with any web search)

1. `watch together apps 2026 self-hosted sync video playback Watch2Gether Kosmi alternatives comparison`
2. `Jellyfin SyncPlay OpenTogetherTube self-hosted watch party open source pros cons`

## 4. Prior-art findings

| Product | Type | Pros | Cons | Lesson taken |
|---------|------|------|------|--------------|
| Watch2Gether / Teleparty / Hyperbeam | Hosted | Polished, zero setup | No own uploads, privacy, not customizable | UX bar to aim for |
| Kosmi | Hosted | Mixes games + video in rooms | Same hosted limitations | Activity variety ideas |
| Jellyfin + SyncPlay | Self-hosted | Mature sync, subtitles, buffering-aware group pause | Live transcoding too heavy for 2-core VPS; no rooms/chat/activities | Group-pause-on-buffering pattern |
| OpenTogetherTube | Self-hosted OSS | Rooms, vote queue, permissions, Docker | URL-centric (YouTube), not uploads; no roles/music/drawing | Closest rooms model; confirms WebSocket state sync |
| Syncplay (desktop) | OSS desktop | Everyone plays local file; server relays tiny messages — near-zero load | Desktop only, no web UI | "Download first, sync locally" mode |
| OpenWatchParty | OSS plugin | Jellyfin plugin + separate Rust WebSocket sync server | Tied to Jellyfin | Standard architecture: sync server ≠ media serving |

Sources: alternativeto.net/software/watch2gether, syncup.tv/blog/best-watch-party-apps-2026, jellywatch.app/blog/jellyfin-watch-party-group-watch-guide-2026, github.com/mhbxyz/OpenWatchParty, saashub.com/compare-syncplay-vs-jellyfin, jellyfin.org

Cross-cutting lessons:
1. Sync = WebSocket, server-authoritative state (position, paused, server timestamp). Small, solved problem.
2. Never live-transcode on this VPS. Process media once at upload (remux when codecs allow, transcode otherwise).
3. Browsers don't play .mkv → one-time conversion to .mp4; .srt/.ass → .vtt.
4. Download-before-watch (Syncplay model) removes streaming load entirely.

## 5. Key decisions & rationale

| Decision | Choice | Why | Rejected |
|----------|--------|-----|----------|
| Build vs assemble | Fully custom | Rooms + activities + couple features don't exist elsewhere; sync is small code | Jellyfin backend (RAM, transcoder risk), OTT fork (URL-centric architecture) |
| Shape | Single monolith + reverse proxy + SQLite | Every extra process costs RAM; 10 users need nothing more | Microservices, Redis, Postgres |
| Media strategy | Process once at upload; proxy serves ranges | 2 cores can't live-transcode; `sendfile` makes streaming ~free | Live/on-demand transcoding |
| Backend language | **Go** (see spec §5) | ~30–80 MB RSS, single static binary embedding the SPA, first-class WebSockets/concurrency, `os/exec` for ffmpeg, one-file deploy | Node/TS, Python (2–5× RAM, heavier ops) |
| Frontend | **Svelte 5 + Vite + Tailwind CSS v4** | Smallest runtime of the major frameworks; design.md tokens map directly to Tailwind `@theme` CSS variables; native `<video>`/`<audio>`/canvas — no player or drawing libs | React (bigger runtime, no benefit at this scope), htmx (insufficient for player sync + canvas) |
| UI direction | Hyper-futuristic minimal on **NxCode** system (`design.md`) | User-provided token file is source of truth; ui-ux-pro-max query independently recommended "Dark Mode (OLED) / terminal dark + green" — converges | ui-ux-pro-max font suggestion (Righteous/Poppins) — overridden by design.md (Inter + JetBrains Mono) |

## 6. Tooling used (reproducibility)

- **superpowers:brainstorming** — requirements → approaches → spec flow.
- **ui-ux-pro-max** — design-system query:
  `python3 ~/.claude/skills/ui-ux-pro-max/scripts/search.py "futuristic minimal dark terminal entertainment watch-together couples app" --design-system -p "Together"`
  Result: Dark Mode (OLED) style, terminal-dark + success-green palette, minimal glow effects, dark-only. Fonts/palette overridden by `design.md`.
- **design.md** (repo root) — NxCode design system; canonical tokens for all UI work.
- **ponytail (full)** — governs all implementation: lazy-senior-dev ladder (YAGNI → reuse → stdlib → native platform → existing dep → one line → minimal code), deliberate shortcuts marked with `ponytail:` comments, one runnable check per non-trivial unit. All implementation sessions should load `ponytail:ponytail` first.
- **superpowers:writing-plans → executing-plans** — next: implementation plan from the spec.

## 7. Execution record (updated 2026-07-04)

1. ~~Implementation plan~~ → written: `docs/superpowers/plans/2026-07-03-together-v1.md` (14 TDD tasks, complete code per step).
2. ~~Implement~~ → executed 2026-07-03/04 via superpowers:subagent-driven-development: fresh implementer subagent per task (haiku for transcription tasks, sonnet for integration), spec+quality reviewer per task, fix→re-review loops, final whole-branch review on the most capable model. Ledger: `.superpowers/sdd/progress.md`.
3. Outcome: 31 commits merged to main (`9e45542`), 5,374 lines, all suites green. 19 review findings fixed during execution — several were defects in the plan's own reference code (invite-code burn, zombie WS reconnect, non-reactive player progress, stranded transcode jobs, SIGTERM hang), validating the per-task review gate.
4. Debt tracked in `docs/debt.md`; `ponytail:` comments in code are the live ledger (`/ponytail-debt` to harvest).
5. Fresh-context onboarding: read `CLAUDE.md` → spec §12 (as-built) → `docs/debt.md`.
