---
name: Andromeda
description: Warm Zen — a dense but calm desktop work cockpit floating on a warm gradient
colors:
  panel: "#ffffff"
  panel-sunken: "#f7f7f9"
  ink: "#1f2128"
  ink-2: "#3e434c"
  muted: "#6f747e"
  muted-2: "#9a9ea7"
  faint: "#bcbfc6"
  line: "#ececef"
  line-2: "#e2e3e6"
  accent: "#c17a5b"
  accent-deep: "#a85f43"
  accent-soft: "rgba(193, 122, 91, 0.13)"
  online: "#6b9b80"
  due: "#a85038"
  due-soft: "rgba(168, 80, 56, 0.12)"
  danger: "#c8453f"
typography:
  body:
    fontFamily: "Pretendard Variable, Pretendard, system-ui, Segoe UI, Malgun Gothic, sans-serif"
    fontSize: "14px"
    fontWeight: 400
  panel-title:
    fontFamily: "Pretendard Variable, Pretendard, system-ui, Segoe UI, Malgun Gothic, sans-serif"
    fontSize: "23px"
    fontWeight: 600
    letterSpacing: "-0.02em"
  section-title:
    fontFamily: "Pretendard Variable, Pretendard, system-ui, Segoe UI, Malgun Gothic, sans-serif"
    fontSize: "15px"
    fontWeight: 600
  grid-cell:
    fontFamily: "Pretendard Variable, Pretendard, system-ui, Segoe UI, Malgun Gothic, sans-serif"
    fontSize: "13px"
    fontWeight: 400
  grid-header:
    fontFamily: "Pretendard Variable, Pretendard, system-ui, Segoe UI, Malgun Gothic, sans-serif"
    fontSize: "11px"
    fontWeight: 500
    letterSpacing: "0.04em"
  meta:
    fontFamily: "Pretendard Variable, Pretendard, system-ui, Segoe UI, Malgun Gothic, sans-serif"
    fontSize: "12px"
    fontWeight: 400
  micro-label:
    fontFamily: "Pretendard Variable, Pretendard, system-ui, Segoe UI, Malgun Gothic, sans-serif"
    fontSize: "11px"
    fontWeight: 500
    letterSpacing: "0.14em"
rounded:
  panel: "14px"
  ctl: "9px"
  pill: "8px"
  chip: "999px"
spacing:
  xs: "2px"
  sm: "8px"
  cell: "9px"
  gap: "12px"
  pane: "18px"
  panel-y: "20px"
  panel-x: "22px"
components:
  panel:
    backgroundColor: "{colors.panel}"
    rounded: "{rounded.panel}"
  field:
    backgroundColor: "{colors.panel}"
    textColor: "{colors.ink}"
    rounded: "{rounded.ctl}"
    padding: "8px 11px"
  button:
    backgroundColor: "{colors.panel}"
    textColor: "{colors.ink}"
    rounded: "{rounded.ctl}"
    padding: "8px 13px"
  button-accent:
    backgroundColor: "{colors.accent}"
    textColor: "{colors.panel}"
    rounded: "{rounded.ctl}"
    padding: "8px 13px"
  button-accent-hover:
    backgroundColor: "{colors.accent-deep}"
  chip:
    backgroundColor: "{colors.accent-soft}"
    textColor: "{colors.accent-deep}"
    rounded: "{rounded.chip}"
---

# Andromeda Design System

> Generated design context for AI design tooling (impeccable). This file
> **mirrors** the implementation sources of truth — `src/styles.css` (`:root`
> tokens + classes), `src/theme.ts` (inline mirror) — and the canonical Korean
> design docs: [`docs/UI-UX.md`](docs/UI-UX.md) (system),
> [`docs/DESIGN-PHILOSOPHY.md`](docs/DESIGN-PHILOSOPHY.md) (beliefs),
> [`docs/DESIGN.md`](docs/DESIGN.md) (identity/architecture). If values here
> diverge, the code wins; update this file when tokens change.

## Overview

Andromeda is the desktop command cockpit for the Deneb gateway: mail, calendar,
todos, and wiki handled densely, with an AI co-pilot column alongside. The look
is **"Warm Zen"** — rounded white panels floating on a soft warm-over-cool
gradient (a metaphor borrowed from Zen browser), with a single warm clay accent.
It is a work _environment_, not a tool: the UI must never overpower content and
must stay comfortable through a full day of use. Dense but calm; quiet, thin
hairlines; restrained motion; no decoration.

## Colors

Color is used for meaning only — never decoration. Raw hex exists solely in
`src/styles.css` `:root`; components reference `var(--…)` tokens or the
`theme.ts` mirror.

### Surfaces

- **panel** `#ffffff` — floating panel and field surface.
- **panel-sunken** `#f7f7f9` — sunken surface (row hover, row-button hover).
- Window canvas is the fixed `--grad` gradient: a warm radial glow
  (`#f4e3d6` at top-left) over a cool neutral linear gradient
  (`#e3e5ee → #dcdee8 → #e6e0df`), deepened slightly so white panels read as
  floating.

### Ink tiers

- **ink** `#1f2128` — primary body text.
- **ink-2** `#3e434c` — secondary body (grid cells, AI responses).
- **muted** `#6f747e` — supporting text.
- **muted-2** `#9a9ea7` — labels, meta, placeholders. Low contrast on white —
  labels/meta only, never body text.
- **faint** `#bcbfc6` — faintest hints, inactive dots.

### Lines

- **line** `#ececef` — hairlines (grid rows, section dividers).
- **line-2** `#e2e3e6` — control borders (fields, buttons).

### Accent (the single point color)

- **accent** `#c17a5b` — warm clay. Emphasis, active state, primary actions.
- **accent-deep** `#a85f43` — accent hover/press.
- **accent-soft** `rgba(193,122,91,.13)` — active backgrounds, chips, selection.

### Semantic

- **online** `#6b9b80` — connected/healthy (calm sage green).
- **due** `#a85038` — danger, overdue, delete, error.
- **due-soft** `rgba(168,80,56,.12)` — danger-tinted backgrounds.
- **danger** `#c8453f` — failure states (e.g. code-session status dots).

## Typography

One typeface everywhere: **Pretendard Variable** (Korean-optimized variable
font), stack `"Pretendard Variable", Pretendard, system-ui, "Segoe UI",
"Malgun Gothic", sans-serif`.

- **body** 14px/400 — default.
- **panel-title (h2)** 23px/600, letter-spacing −0.02em — titles are set tight.
- **section-title (h3)** 15px/600.
- **grid-cell** 13px/400 in ink-2; unread rows only go to 600.
- **grid-header** 11px/500, tracked 0.04em, muted-2.
- **meta/snippet** 12px/400, muted-2.
- **micro-label** 11px, tracked 0.14em, uppercase — Latin labels only
  (`DENEB AI`); Korean nav labels are never uppercased/tracked.

Numbers use `font-feature-settings: "tnum" 1` for even-width grid alignment.
Hard floor: 11px for micro labels and grid headers; body information never
below 12px.

## Elevation

Floating is the core illusion, built from three things working together: the
layered panel shadow, the slightly sunken gradient canvas, and the 12px gap
between panels. Shadows are reserved for the floating metaphor — never for
emphasis or decoration.

- **shadow-panel** (floating panels): three layers — a crisp contact layer
  `0 1px 2px rgba(33,38,66,.05)`, a mid lift `0 10px 24px -10px rgba(33,38,66,.2)`,
  and a deep ambient `0 30px 64px -22px rgba(33,38,66,.34)`. The ambient layer
  does most of the floating.
- **shadow-pill** (lifted active nav tab): a contact layer
  `0 1px 2px rgba(40,45,70,.1)` and a lift `0 8px 18px -5px rgba(40,45,70,.24)`.

Motion is an accent, not a show: `fadeUp` entrance (6px rise + fade, 0.45s,
`cubic-bezier(.2,.7,.2,1)`, staggered 26–60ms), `pulse` breathing on status
dots (2.4s), 0.12–0.15s control transitions, `scale(.98)` press feedback.
Everything respects `prefers-reduced-motion`.

## Components

Canonical catalog lives in [`docs/UI-UX.md`](docs/UI-UX.md) §5; structural and
stateful styling lives in `styles.css` classes, inline styling in `theme.ts`
tokens.

- **panel** (`.panel`) — floating rounded white surface: panel color, 14px
  radius, shadow-panel. The shared vessel for work area, AI panel, popovers.
- **field** (`.field`) — white, line-2 border, 9px radius, 13px text; focus =
  accent border + `0 0 0 3px accent-soft` ring.
- **button** (`.btn`) — white, line-2 border; hover `#f5f5f7`, active
  `scale(.98)`. **button-accent** (`.btn-accent`) — accent bg, white text, one
  per primary action; hover accent-deep.
- **data grid** (`.dgrid` + `Grid`/`GridNotice`/`RowBtn`) — dense table: 11px
  tracked headers, 13px ink-2 cells at 9×10 padding, hairline rows, warm
  `#faf9f8` row hover. `GridNotice` renders disconnected → error → loading →
  empty states in that fixed order.
- **chip** (`.chip`) — accent-soft bg, accent-deep text, fully-round pill.
- **status dot** (`.live-dot`) — 6px circle; connected = online + pulse,
  disconnected = faint, static.
- **nav tab** (`.nav-item`) — transparent by default; active tab **lifts**:
  white bg + shadow-pill, accent icon.
- **icons** (`Icon.tsx`) — dependency-free outline set: 24 viewBox, 1.85
  stroke, round caps, `currentColor`. No fill icons, no external icon libs.

## Do's and Don'ts

- **Do** reference tokens only: `var(--…)` / `theme.ts` in components. **Don't**
  hardcode hex anywhere outside `styles.css` `:root`.
- **Do** keep clay as the only point color. **Don't** introduce a second accent;
  semantic colors (online/due/danger) are the only exceptions.
- **Do** keep shadows for floating surfaces. **Don't** add emphasis/decoration
  shadows or animated gradients, parallax, bouncy motion.
- **Do** keep data dense (9×10 cells, thin hairlines). **Don't** go below 11px
  (labels) / 12px (body information).
- **Do** serialize every pane's visible content to text for the AI panel
  (`useRegisterPane`). What the user sees == what the AI reads.
- **Do** handle disconnected/error/loading/empty via `GridNotice` in that
  order. **Don't** let a failed RPC masquerade as "no data".
- **Do** give each screen exactly one `.btn-accent` primary action; danger
  actions use due/danger. **Don't** uppercase or track Korean text — micro
  labels are Latin-only.
- **Do** reuse `fadeUp`/existing transitions. **Don't** invent new large
  motions; respect `prefers-reduced-motion`.
