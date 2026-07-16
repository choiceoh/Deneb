// Tiled-workspace layout logic (pure). The work area can host 1–3 panes side by
// side; these helpers compute the next tile set for open/split/close so the
// provider stays a thin state shell and the semantics are unit-testable.
//
// Invariants: tiles is a non-empty, deduped list of TILEABLE views, capped at
// MAX_TILES. "Focused" is the tile that owns the AI context and receives
// pane-switch replacements.
import type { View } from "./types";

export const MAX_TILES = 3;

// Panes that can live in a split. chat/files have dedicated always-mounted
// surfaces outside the generic pane render (Workstation), and settings is a
// full-screen utility — none of them belong in a tile row.
const NON_TILEABLE: ReadonlySet<View> = new Set(["chat", "files", "settings"] as View[]);

export function isTileable(v: View): boolean {
  return !NON_TILEABLE.has(v);
}

export interface TileState {
  tiles: View[];
  focused: View;
}

// The focused tile: the current view when it's in the tile set, else the first
// tile (the view may be parked on a non-tile surface like settings/chat).
export function focusedTile(tiles: View[], view: View): View {
  return tiles.includes(view) ? view : tiles[0];
}

// Open v as the primary destination: focus it if already tiled, otherwise it
// replaces the focused slot (preserving the split shape). Non-tileable views
// leave the tile set alone — the caller just switches the view surface.
export function openInTiles(tiles: View[], focused: View, v: View): TileState {
  if (!isTileable(v) || tiles.includes(v)) return { tiles, focused: tiles.includes(v) ? v : focused };
  const next = tiles.map((t) => (t === focused ? v : t));
  return { tiles: next.includes(v) ? next : [v], focused: v };
}

// Split: add v as a new tile (focus it). At capacity, the last non-focused tile
// gives up its slot. No-op (just focus) when v is already tiled.
export function splitInTiles(tiles: View[], focused: View, v: View): TileState {
  if (!isTileable(v)) return { tiles, focused };
  if (tiles.includes(v)) return { tiles, focused: v };
  if (tiles.length < MAX_TILES) return { tiles: [...tiles, v], focused: v };
  let replaced = false;
  const next = [...tiles];
  for (let i = next.length - 1; i >= 0; i--) {
    if (next[i] !== focused) {
      next[i] = v;
      replaced = true;
      break;
    }
  }
  return { tiles: replaced ? next : [v], focused: v };
}

// Close a tile (defaults to the focused one). The last tile can't be closed —
// a workstation always shows something.
export function closeInTiles(tiles: View[], focused: View, v?: View): TileState {
  const target = v ?? focused;
  if (!tiles.includes(target) || tiles.length <= 1) return { tiles, focused };
  const idx = tiles.indexOf(target);
  const next = tiles.filter((t) => t !== target);
  const nextFocused = focused === target ? next[Math.min(idx, next.length - 1)] : focused;
  return { tiles: next, focused: nextFocused };
}

// Validate a tile list coming from persistence or a gateway command: known,
// tileable, deduped, capped, never empty.
export function sanitizeTiles(raw: unknown, valid: ReadonlySet<View>, fallback: View = "today"): View[] {
  const out: View[] = [];
  if (Array.isArray(raw)) {
    for (const item of raw) {
      if (typeof item !== "string") continue;
      const v = item as View;
      if (!valid.has(v) || !isTileable(v) || out.includes(v)) continue;
      out.push(v);
      if (out.length >= MAX_TILES) break;
    }
  }
  return out.length > 0 ? out : [fallback];
}
