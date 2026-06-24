// Build the text projection a pane pushes to the AI panel: a counted header plus
// one line per row. Centralized so every list pane presents to Deneb in the same
// shape (and the shape can change in one place). See useRegisterPane.
//
// `serializeList` covers the common case (header = `[label count건]`). Panes that
// need a bespoke header (e.g. SearchPane quotes the query) use `projectList` with
// a custom header and keep the same one-line-per-row body.
export function projectList<T>(header: string, rows: T[], line: (row: T) => string): string {
  if (rows.length === 0) return "";
  return `${header}\n` + rows.map(line).join("\n");
}

export function serializeList<T>(label: string, rows: T[], line: (row: T) => string, unit = "건"): string {
  return projectList(rows.length === 0 ? "" : `[${label} ${rows.length}${unit}]`, rows, line);
}
