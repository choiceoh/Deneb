// Separator-less bordered pipe-table recovery (native PipeTableNormalizer parity).

const FENCE = /^(\s{0,3})(`{3,}|~{3,})\s*(.*?)\s*$/;
const SEP = /^\s*\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)+\|?\s*$/;

function pipePrefix(line: string): string {
  let n = 0;
  while (n < line.length && (line[n] === " " || line[n] === "\t" || line[n] === ">")) n++;
  return line.slice(0, n);
}

function cellCount(content: string): number {
  let s = content.trim();
  if (s.startsWith("|")) s = s.slice(1);
  if (s.endsWith("|") && !s.endsWith("\\|")) s = s.slice(0, -1);
  let cells = 1;
  for (let i = 0; i < s.length; i++) {
    if (s[i] === "\\" && i + 1 < s.length && s[i + 1] === "|") {
      i++;
      continue;
    }
    if (s[i] === "|") cells++;
  }
  return cells;
}

function isBorderedPipeRow(content: string): boolean {
  const t = content.trim();
  if (t.length < 3 || t[0] !== "|" || t[t.length - 1] !== "|" || t.endsWith("\\|")) return false;
  return cellCount(content) >= 2;
}

function convertFencedPipeTable(
  lines: string[],
  openIdx: number,
  prefix: string,
  openRun: string,
): { rows: string[]; nextIndex: number } | null {
  const ch = openRun[0];
  const len = openRun.length;
  const rows: string[] = [];
  let closeIdx = -1;
  for (let j = openIdx + 1; j < lines.length; j++) {
    const l = lines[j];
    if (pipePrefix(l) !== prefix) return null;
    const c = l.slice(prefix.length);
    const f = FENCE.exec(c);
    if (f) {
      if (f[2][0] === ch && f[2].length >= len && f[3].trim() === "") {
        closeIdx = j;
        break;
      }
      return null;
    }
    rows.push(l);
  }
  if (closeIdx < 0) return null;
  let cols = -1;
  let hasSeparator = false;
  for (const l of rows) {
    const c = l.slice(prefix.length);
    if (!isBorderedPipeRow(c)) return null;
    if (SEP.test(c)) hasSeparator = true;
    const n = cellCount(c);
    if (cols === -1) cols = n;
    else if (n !== cols) return null;
  }
  if (rows.length < 2 || cols < 2 || !hasSeparator) return null;
  return { rows, nextIndex: closeIdx + 1 };
}

/** Insert missing `| --- |` into bordered pipe tables; unwrap bare-fenced tables. */
export function normalizePipeTables(text: string): string {
  if (!text.includes("|")) return text;
  const lines = text.split("\n");
  const result: string[] = [];
  let i = 0;
  let inFence = false;
  let fenceCh = " ";
  let fenceLen = 0;

  while (i < lines.length) {
    const line = lines[i];
    const prefix = pipePrefix(line);
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
        const converted = convertFencedPipeTable(lines, i, prefix, fence[2]);
        if (converted) {
          result.push(...converted.rows);
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

    if (isBorderedPipeRow(content)) {
      const cols = cellCount(content);
      let j = i;
      let hasSeparator = false;
      let consistent = true;
      const run: string[] = [];
      while (j < lines.length && pipePrefix(lines[j]) === prefix) {
        const c = lines[j].slice(prefix.length);
        if (!isBorderedPipeRow(c)) break;
        if (SEP.test(c)) hasSeparator = true;
        if (cellCount(c) !== cols) consistent = false;
        run.push(lines[j]);
        j++;
      }
      if (run.length >= 2 && cols >= 2 && consistent && !hasSeparator) {
        result.push(run[0]);
        result.push(`${prefix}| ${Array(cols).fill("---").join(" | ")} |`);
        for (let k = 1; k < run.length; k++) result.push(run[k]);
        i = j;
        continue;
      }
      result.push(...run);
      i = j;
      continue;
    }

    result.push(line);
    i++;
  }
  return result.join("\n");
}
