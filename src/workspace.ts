// Work-area context for Deneb's AI — the "well-structured back end" principle.
//
// Each work-area pane is backed by structured data (in Phase 1, a Refine resource:
// mail rows, calendar events, todo items, a wiki page). Because that data is
// already structured at the back end, a pane can describe its current content to
// the AI as plain TEXT — losslessly, with no vision model. The same structured
// source renders to a grid on screen AND serializes to text for the AI: one
// source, two projections.
//
// Vision stays a last resort for true images / scanned PDFs (Deneb's
// capture.image + OCR path), never for reading our own UI.

export interface WorkspaceView {
  // A compact text representation of what is currently in this pane — markdown,
  // a table, a key/value list, etc. Return "" when there is nothing to add.
  serializeForAI(): string;
}

// Join the active views' serializations into the workspaceContext string passed
// to chatStream(). Empty contributions are dropped.
export function collectWorkspaceContext(views: WorkspaceView[]): string {
  return views
    .map((v) => v.serializeForAI().trim())
    .filter((s) => s.length > 0)
    .join("\n\n");
}
