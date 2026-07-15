/** Promote the first summary line to a heading; numbered rows stay GFM lists. */
export function erpTextToMarkdown(raw: string): string {
  const lines = raw.replace(/\r\n/g, "\n").trim().split("\n");
  if (!lines.length || (lines.length === 1 && !lines[0])) return "";
  const out: string[] = [];
  let headed = false;
  for (const line of lines) {
    const t = line.trimEnd();
    if (!headed && t.trim()) {
      out.push(`## ${t.trim()}`);
      headed = true;
      continue;
    }
    out.push(t);
  }
  return out.join("\n");
}
