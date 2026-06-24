// Connection status dot — online (green, optionally pulsing) vs disconnected
// (faint). The .live-dot / .live-dot.pulse classes live in styles.css; this stops
// the AI panel, sidebar, and settings from each re-deciding the color + pulse inline.
export function LiveDot({ connected, pulse = false }: { connected: boolean; pulse?: boolean }) {
  return (
    <span
      className={"live-dot" + (pulse && connected ? " pulse" : "")}
      style={{ background: connected ? "var(--online)" : "var(--faint)" }}
    />
  );
}
