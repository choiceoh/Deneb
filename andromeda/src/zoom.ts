// Mouse-first zoom: Ctrl+wheel (and Ctrl +/-/0) scales the whole UI via the
// body `zoom` property, persisted across launches. The webview exposes no
// built-in zoom on our frameless window, so high-DPI monitors had no lever.
import { getString, setString } from "./storage";

const KEY = "andromeda.zoom";
const MIN = 0.7;
const MAX = 1.6;
const STEP = 0.1;

function clamp(z: number): number {
  return Math.min(MAX, Math.max(MIN, Math.round(z * 10) / 10));
}

function apply(z: number) {
  document.body.style.setProperty("zoom", String(z));
}

export function currentZoom(): number {
  const saved = Number.parseFloat(getString(KEY));
  return Number.isFinite(saved) ? clamp(saved) : 1;
}

function set(z: number) {
  const next = clamp(z);
  setString(KEY, String(next));
  apply(next);
}

// Idempotent: returns a cleanup so React StrictMode double-mount stays safe.
export function initZoom(): () => void {
  apply(currentZoom());
  function onWheel(e: WheelEvent) {
    if (!e.ctrlKey) return;
    e.preventDefault();
    set(currentZoom() + (e.deltaY < 0 ? STEP : -STEP));
  }
  function onKey(e: KeyboardEvent) {
    if (!e.ctrlKey && !e.metaKey) return;
    if (e.key === "+" || e.key === "=") {
      e.preventDefault();
      set(currentZoom() + STEP);
    } else if (e.key === "-") {
      e.preventDefault();
      set(currentZoom() - STEP);
    } else if (e.key === "0") {
      e.preventDefault();
      set(1);
    }
  }
  window.addEventListener("wheel", onWheel, { passive: false });
  window.addEventListener("keydown", onKey);
  return () => {
    window.removeEventListener("wheel", onWheel);
    window.removeEventListener("keydown", onKey);
  };
}
