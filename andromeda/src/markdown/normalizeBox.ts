// Box-drawing table → GFM (native BoxTableNormalizer parity).

const VERTICALS = "│┃║┆┇┊┋╎╏";
const VERTICAL_SPLIT = /[│┃║┆┇┊┋╎╏]/;
const FENCE = /^(\s{0,3})(`{3,}|~{3,})\s*(.*?)\s*$/;

function isBoxBorderChar(c: string): boolean {
  const code = c.codePointAt(0) ?? 0;
  return code >= 0x2500 && code <= 0x257f && !VERTICALS.includes(c);
}

function blockPrefix(line: string): string {
  let n = 0;
  while (n < line.length && (line[n] === " " || line[n] === "\t" || line[n] === ">")) n++;
  return line.slice(0, n);
}

function isBorderLine(line: string): boolean {
  const t = line.trim();
  if (!t) return false;
  let hasBorder = false;
  for (const c of t) {
    if (isBoxBorderChar(c)) hasBorder = true;
    else if (!VERTICALS.includes(c) && c !== " ") return false;
  }
  return hasBorder;
}

function isDataLine(line: string): boolean {
  const t = line.trim();
  if (!t || !VERTICALS.includes(t[0])) return false;
  let n = 0;
  for (const c of t) if (VERTICALS.includes(c)) n++;
  return n >= 2;
}

function splitDataCells(line: string): string[] {
  const cells = line
    .trim()
    .split(VERTICAL_SPLIT)
    .map((c) => c.trim());
  if (cells.length && cells[0] === "") cells.shift();
  if (cells.length && cells[cells.length - 1] === "") cells.pop();
  return cells;
}

function convertBlock(block: string[]): string[] {
  const rows: string[][] = [];
  let borderSinceRow = false;
  for (const line of block) {
    if (isBorderLine(line)) {
      borderSinceRow = true;
      continue;
    }
    const cells = splitDataCells(line);
    if (!cells.length) continue;
    const continuation = rows.length > 0 && !borderSinceRow && cells[0] === "";
    if (continuation) {
      const prev = rows[rows.length - 1];
      for (let k = 0; k < cells.length; k++) {
        const v = cells[k];
        if (!v) continue;
        while (prev.length <= k) prev.push("");
        prev[k] = prev[k] ? `${prev[k]} ${v}` : v;
      }
    } else {
      rows.push([...cells]);
    }
    borderSinceRow = false;
  }
  if (!rows.length) return block;
  const numCols = Math.max(...rows.map((r) => r.length));
  for (const r of rows) while (r.length < numCols) r.push("");
  const esc = (c: string) => c.replace(/\\/g, "\\\\").replace(/\|/g, "\\|");
  const md: string[] = [];
  md.push(`| ${rows[0].map(esc).join(" | ")} |`);
  md.push(`| ${Array(numCols).fill("---").join(" | ")} |`);
  for (let r = 1; r < rows.length; r++) md.push(`| ${rows[r].map(esc).join(" | ")} |`);
  return md;
}

function convertFencedBoxTable(
  lines: string[],
  openIdx: number,
  prefix: string,
  openRun: string,
): { mdLines: string[]; nextIndex: number } | null {
  const ch = openRun[0];
  const len = openRun.length;
  const body: string[] = [];
  let closeIdx = -1;
  for (let j = openIdx + 1; j < lines.length; j++) {
    const l = lines[j];
    if (blockPrefix(l) !== prefix) return null;
    const c = l.slice(prefix.length);
    const f = FENCE.exec(c);
    if (f) {
      if (f[2][0] === ch && f[2].length >= len && f[3].trim() === "") {
        closeIdx = j;
        break;
      }
      return null;
    }
    body.push(c);
  }
  if (closeIdx < 0) return null;
  let dataCount = 0;
  let borderCount = 0;
  let maxCols = 0;
  for (const c of body) {
    if (isDataLine(c)) {
      dataCount++;
      maxCols = Math.max(maxCols, splitDataCells(c).length);
    } else if (isBorderLine(c)) borderCount++;
    else if (c.trim()) return null;
  }
  if (dataCount < 1 || borderCount < 1 || maxCols < 2) return null;
  return { mdLines: convertBlock(body.filter((l) => l.trim())), nextIndex: closeIdx + 1 };
}

function isDataAfter(line: string, prefix: string): boolean {
  return blockPrefix(line) === prefix && isDataLine(line.slice(prefix.length));
}

/** Rewrite box-drawing tables as GFM. Untouched when no box verticals. */
export function normalizeBoxTables(text: string): string {
  if (![...text].some((c) => VERTICALS.includes(c))) return text;
  const lines = text.split("\n");
  const result: string[] = [];
  let i = 0;
  let fenceCh = " ";
  let fenceLen = 0;
  let inFence = false;

  while (i < lines.length) {
    const line = lines[i];
    const prefix = blockPrefix(line);
    const content = line.slice(prefix.length);
    const fence = FENCE.exec(content);

    if (inFence) {
      result.push(line);
      if (fence) {
        const run = fence[2];
        if (run[0] === fenceCh && run.length >= fenceLen && fence[3].trim() === "") inFence = false;
      }
      i++;
      continue;
    }
    if (fence) {
      if (fence[3].trim() === "") {
        const converted = convertFencedBoxTable(lines, i, prefix, fence[2]);
        if (converted) {
          for (const md of converted.mdLines) result.push(prefix + md);
          i = converted.nextIndex;
          continue;
        }
      }
      inFence = true;
      fenceCh = fence[2][0];
      fenceLen = fence[2].length;
      result.push(line);
      i++;
      continue;
    }

    const startsBlock =
      isDataLine(content) || (isBorderLine(content) && i + 1 < lines.length && isDataAfter(lines[i + 1], prefix));
    if (startsBlock) {
      let j = i;
      let dataCount = 0;
      let borderCount = 0;
      let maxCols = 0;
      const blockContents: string[] = [];
      while (j < lines.length && blockPrefix(lines[j]) === prefix) {
        const c = lines[j].slice(prefix.length);
        if (isDataLine(c)) {
          dataCount++;
          maxCols = Math.max(maxCols, splitDataCells(c).length);
        } else if (isBorderLine(c)) borderCount++;
        else break;
        blockContents.push(c);
        j++;
      }
      if (dataCount >= 1 && borderCount >= 1 && maxCols >= 2) {
        for (const md of convertBlock(blockContents)) result.push(prefix + md);
        i = j;
        continue;
      }
    }
    result.push(line);
    i++;
  }
  return result.join("\n");
}
