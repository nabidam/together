# DESIGN.md — Together V2 (NxCode adoption map)

Living document. Together uses a **pre-built design system**: NxCode, whose single source of truth is `design.md` (token values in its frontmatter + prose ramp), mapped **once** into Tailwind `@theme` variables in `web/src/app.css`. This file maps those tokens onto the screens `UX.md` defines and onto shadcn-svelte's semantic variables. **No color, radius, type, or motion value appears in this file or in any component — everything is referred to by token name.** The only values originating here are the app-specific layout constants in §6, which have no other home.

## 1. Direction

- **Adjectives:** intimate, focused, terminal-native.
- **References:** a private cinema run from a terminal — mpv/VLC's utilitarian transport grammar, Linear's restraint, NxCode's "calm command center" voice.
- **Signature:** the NxCode **green glow** (`glow-green` utility in `app.css`) is reserved for exactly two things: the `● In Sync` status dot and the active play state in the transport. Green means "alive and in sync" — nowhere else. Color is scarce against the graphite; hairline borders (`--color-border`) carry structure.

## 2. Semantic role → token map

Token names are the `@theme` variables in `web/src/app.css`, each mapped 1:1 from `design.md`.

| Role | Token |
|---|---|
| Page background | `--color-surface` |
| Player letterbox / overlay scrim base | `--color-void` |
| Cards, panels, dialogs, side panel | `--color-card` (+ 1px `--color-border`) |
| Input fill | `--color-input` |
| Body text | `--color-fg` |
| Headings, values, strong text | `--color-fg-strong` |
| Hairlines, dividers, input borders | `--color-border` |
| Primary action fill / success / online / In Sync | `--color-primary` (text on it: `--color-on-primary`) |
| Links, focus rings, File Ready dot, data highlights | `--color-secondary` |
| Destructive (end room, delete media) | `--color-error` |
| Size-mismatch warn, processing/failed accents | `--color-warning` / `--color-error` |
| Informational banners (reconnecting) | `--color-info` |
| Type roles | `design.md` typography: `display`, `headline`, `body-md`, `label`, `eyebrow` (mono, the only ALL-CAPS), `code` — via `--font-sans` / `--font-mono` + the `.eyebrow` utility |
| Numerics & timestamps (positions, durations, sizes, chat times) | `--font-mono`, always |
| Radii | `--radius-xs…xl`, `--radius-pill` (map `design.md` `rounded.pill` in chunk 6); default control radius `--radius-md` |
| Motion | `design.md` durations mapped as `--duration-fast/base/slow` (chunk 6) with `--ease-nx`; no bounce, no spring |
| Spacing | Tailwind's 4px-base scale only — the `design.md` 8px rhythm on a 4px base |

**Status dot ladder** (app semantics, NxCode vocabulary — see gap G1): `◌ Downloading` = hollow glyph in `--color-fg`; `◐ File Ready` = `--color-secondary`; `● In Sync` = `--color-primary` + `glow-green`. Dots are the `design.md` mono status-dot glyph language, never emoji.

## 3. shadcn-svelte variable mapping

Defined once in `app.css`, next to `@theme`; shadcn components consume these and are thereby skinned — components themselves stay token-free.

| shadcn variable | ← token |
|---|---|
| `--background` / `--foreground` | `--color-surface` / `--color-fg` |
| `--card` / `--card-foreground` | `--color-card` / `--color-fg` |
| `--popover` / `--popover-foreground` | `--color-card` / `--color-fg` |
| `--primary` / `--primary-foreground` | `--color-primary` / `--color-on-primary` |
| `--secondary` / `--secondary-foreground` | `--color-input` / `--color-fg-strong` |
| `--muted` / `--muted-foreground` | `--color-input` / `--color-fg` |
| `--accent` / `--accent-foreground` | `--color-input` / `--color-fg-strong` |
| `--destructive` / `--destructive-foreground` | `--color-error` / `--color-fg-strong` |
| `--border` / `--input` | `--color-border` / `--color-border` |
| `--ring` | `--color-secondary` |
| `--radius` | `--radius-md` |

## 4. Component inventory (UX element → implementation)

| UX element | Serves |
|---|---|
| All buttons (S1–S7, dialogs, transport) | shadcn **Button** (`default` = primary fill; `ghost`/`outline` for secondary; `destructive` for M2 confirm + delete) |
| Text fields (S1, S2, S6, M1 name, chat input) | shadcn **Input** |
| M1 Media Picker | shadcn **Dialog** + custom single-select list rows |
| M2 End Room, M3 Regenerate | shadcn **AlertDialog** |
| M4 Play From Server | shadcn **Dialog** |
| Room menu (copy link / regenerate / end) | shadcn **DropdownMenu**, host-only |
| Status-dot explanations | shadcn **Tooltip** (hover + focus) |
| S3 loading | shadcn **Skeleton** rows |
| Reconnecting banner, inline retry/error banners | shadcn **Alert** (`--color-info` accent for reconnect, `--color-error` for failures) |
| S7 library table | shadcn **Table** |
| Scrub bar | shadcn **Slider**, wired to seek intents (echo-driven) |
| S3 room rows, participant list, chat messages | custom compositions on `--color-card` rows |
| Acquisition panel (UX §3.4) | custom composition: Card + two peer Buttons + quiet text link |
| Video/audio transport, arm overlay, now-playing anchor (S5) | **custom** (gap G2/G3) |
| Icons | `lucide-svelte` at `design.md` stroke/sizes: download, folder-open, play, pause, maximize, captions, panel-right, link, refresh-cw, x, plus mono glyphs (`●`, `//`) per `design.md` |

## 5. Gap list — UX needs the system can't serve

Flagged per the workflow; proposed resolutions below are now part of this contract unless the user objects at the gate review.

- **G1 — Status dot ladder.** Neither NxCode nor shadcn defines a three-step readiness indicator. Proposal (adopted in §2): hollow `--color-fg` → `--color-secondary` → `--color-primary`+glow, using NxCode's status-dot glyph language. Rationale: green stays "alive", cyan already means "data/ready", no new colors invented.
- **G2 — Media transport bar.** shadcn has no player chrome. Proposal: custom bar on a `--color-void` scrim, ghost Buttons + Slider + mono time readout; standard video grammar per UX §5.
- **G3 — Arm overlay.** Proposal: full-player `--color-void` scrim at reduced opacity, one primary Button with a play glyph + "Click to enable playback" in `body-md`.
- **G4 — `--radius-pill` and motion-duration variables** exist in `design.md` but not yet in `app.css` `@theme`. Mechanical: add the mappings in chunk 6 (no invention).

## 6. Layout (single source for these constants)

| Constant | Value |
|---|---|
| Side panel width (S4/S5) | 320px |
| Auth/join column max width (S1/S2/S6, centered) | 360px |
| Room strip height | 48px |
| Two-column theater breakpoint | ≥900px |
| Page container cap | `design.md` container cap (1320px class already implied by NxCode; use its prose value, don't restate elsewhere) |

- S4/S5: player region fills remaining width beside the side panel; panel collapsed → player takes 100%. Below the breakpoint the side panel becomes a full-height overlay toggled from the transport.
- S3/S7: single centered column under the container cap; S7 may run denser (tables + two cards, no tabs) per UX §5.
- Density: generous outer padding, tight internal grouping (NxCode instrument-panel rhythm); whole-row click targets ≥44px.

## 7. Component states

Interactive elements (Button, Input, Slider, menu items, room rows, panel toggle, arm overlay):
- **default** per §3 mapping; **hover** = surface one ramp step lighter (`--color-card` → `--color-input`) or Button opacity step, `--duration-fast`; **focus-visible** = 2px `--ring` outline, offset 2px — always visible, never suppressed; **active** = pressed step, no transform bounce; **disabled** = reduced opacity + `cursor-not-allowed`, still ≥4.5:1 for its label. Disconnected state (AC-3.6) uses disabled styling on transport + chat input under the reconnect Alert.

Data views:
- **S3 rooms:** empty ("No rooms right now." + Create room), loading (Skeleton rows), error (Alert + Retry).
- **M1 library:** empty (admin link vs "Ask your admin…"), loading (Skeleton), error (inline Alert).
- **Room join:** loading (full-region spinner until `hello`), error (reconnect Alert; terminal states for invalid/full/closed per UX).
- **Acquisition panel:** default (two peer actions + quiet fallback), mismatch (inline `--color-warning` block with both sizes + re-pick), never a modal.
- **Chat:** empty ("No messages yet."), disconnected (input disabled).
- **S7 rows:** processing (spinner + stage text), ready, failed (`--color-error` + reason + delete affordance).

## 8. Hard rules

- Tokens only in components — no raw hex, no px font sizes, no ad-hoc radii/shadows/durations. The only mapping site is `app.css` (`@theme` + §3 block).
- `design.md` Do's and Don'ts bind: green sparingly; no pure black; no gradients/purple wash; hairlines carry structure; sentence case; ALL-CAPS only in the mono eyebrow; **no emoji** (wireframe glyphs in UX.md stand in for lucide icons); no colored left-border card accents; no bounce/spring.
- WCAG AA: contrast ≥4.5:1, focus visible on every interactive element, touch targets ≥44px, body ≥15px, `prefers-reduced-motion` collapses transitions to instant (already global in `app.css`).
- Dark only. No theme toggle, no light palette.
- No template clichés: no hero gradients, no glassmorphism, no rounded-3xl cards. The app should read as NxCode: machined, quiet, alive at exactly the sync dot.
