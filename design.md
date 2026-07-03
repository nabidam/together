---
name: NxCode
colors:
  primary: "#22D86B"
  secondary: "#1FC7D4"
  surface: "#0A0C0F"
  surface-card: "#1A1F26"
  on-surface: "#B4BECB"
  on-surface-strong: "#EEF2F6"
  border: "#2C343F"
  error: "#FF5C5C"
  warning: "#F5B23D"
  info: "#5B9CFF"
typography:
  display:
    fontFamily: Inter
    fontSize: 68px
    fontWeight: 700
    letterSpacing: -0.03em
  headline:
    fontFamily: Inter
    fontSize: 40px
    fontWeight: 600
    letterSpacing: -0.015em
  body-md:
    fontFamily: Inter
    fontSize: 15px
    fontWeight: 400
    lineHeight: 1.5
  label:
    fontFamily: Inter
    fontSize: 13px
    fontWeight: 500
  eyebrow:
    fontFamily: JetBrains Mono
    fontSize: 11px
    fontWeight: 500
    letterSpacing: 0.12em
    textTransform: uppercase
  code:
    fontFamily: JetBrains Mono
    fontSize: 13px
    fontWeight: 400
rounded:
  xs: 3px
  sm: 5px
  md: 8px
  lg: 12px
  xl: 16px
  pill: 999px
---

# NxCode Design System

## Overview
A terminal-native design language for a research & IT company building at the
edge of technology. The world is a **dark, terminal-grade coding environment**:
deep graphite surfaces, a signal-green primary that reads like a live cursor,
cyan for data and links, and JetBrains Mono wherever a "system" voice is needed.
The brand is **hopeful and technical** — it believes the future is buildable
and speaks in the vocabulary of the people who build it. Calm command center:
the reader should feel like they just SSH'd into something powerful and well-built.

## Colors
Dark-first and terminal-native. Color is used **sparingly against the dark** — a
little green goes a long way.

- **Primary** (#22D86B, signal green): primary actions, success, brand moments,
  and "alive/online" states. Reads as a live terminal cursor.
- **Secondary** (#1FC7D4, cyan): links, focus rings, data highlights.
- **Surface** (#0A0C0F): page background. Never pure black — a deep graphite ramp
  runs from `#060709` (void) → `#1A1F26` (card) → `#222932` (input).
- **Surface card** (#1A1F26): cards and panels, 1px border, no colored left accent.
- **On-surface** (#B4BECB): body text on dark. Strong text steps up to #EEF2F6.
- **Border** (#2C343F): hairlines carry most structure — borders do heavy lifting
  since fills are close in value.
- **Stream accents** (violet #A98BFF, pink #FF7EB6, amber #F5B23D, blue #5B9CFF):
  only in code syntax, charts, and category coding.
- **Semantic**: green = success, amber (#F5B23D) = warning, red (#FF5C5C) = danger,
  blue (#5B9CFF) = info.

## Typography
- **Display / Headlines**: Inter, semibold–bold, tight tracking (-0.015em headings,
  -0.03em display). Scale tops out at 68px hero.
- **Body**: Inter, regular, 15px, line-height 1.5. Open digits (`cv05`), single-story
  `a` (`ss03`).
- **Labels**: Inter, medium, 13px.
- **Eyebrow / kicker**: JetBrains Mono, 11px, uppercase, +0.12em tracking — the only
  ALL-CAPS in the system (e.g. `// SYSTEM STATUS`).
- **Code / data / numerics**: JetBrains Mono. Monospace all numbers in data contexts.
  Mono is used liberally for labels to reinforce the "system" voice.
- **Casing**: sentence case for everything — headings, buttons, labels.

## Spacing & Layout
- 8px rhythm on a 4px base. Dense, instrument-panel feel — generous outer padding,
  tight internal grouping.
- Containers cap at 1320px. App shell: 264px sidebar + 56px topbar.
- Prefer flex/grid with `gap` over margins.

## Components
- **Buttons**: rounded (8px), primary uses signal-green fill with #04210F text.
- **Inputs**: 1px border, `#222932` surface, cyan focus ring.
- **Cards**: `#1A1F26` fill + 1px border + soft shadow. No elevation tricks —
  elevation is communicated by surface lightness first, shadow second. No colored
  left-border accent.
- **Corners**: small, machined radii (3–12px for most UI, 8px default; up to 22px
  for large feature panels).
- **Shadows & glow**: dark, low, diffuse drop shadows. The signature elevation is the
  **green glow** (1px green ring + soft bloom) on active/primary/online elements.
  Focus uses a cyan ring.
- **Icons**: Lucide line icons, 1.75px stroke, 16/20/24px. Plus monospace glyphs as
  semantic marks — `>_` (prompt/brand), `//` (eyebrow), `~` (home), `↗` (external),
  `●` (status dot).

## Motion
- Fast and precise. Durations 120 / 200 / 360ms; default easing `cubic-bezier(.16,1,.3,1)`.
- Opacity + transform fades and short slides; occasional cursor blink or typing reveal.
- **No bounce, no spring overshoot** — engineered, not playful. Respect `prefers-reduced-motion`.

## Do's and Don'ts
- **Do** use green sparingly — only for the most important action, success, or "online" state.
- **Don't** use busy gradients or a full purple wash; backgrounds are flat graphite.
  A faint dotted matrix or low-opacity green/cyan hero glow is the only permitted texture.
- **Do** lean on 1px hairline borders for structure since fills are close in value.
- **Don't** use pure black (#000) — start from the graphite ramp.
- **Do** keep everything sentence case; reserve ALL-CAPS for the mono eyebrow only.
- **Don't** use emoji. Meaning is carried by Lucide icons, mono glyphs, and the status dot.
- **Do** monospace all numerics and data; use terminal/command metaphors as flavor that means something.
- **Don't** add a colored left-border accent to cards or use spring/bounce motion.
