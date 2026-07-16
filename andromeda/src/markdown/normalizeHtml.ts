// Block-level HTML → markdown (native HtmlBlockNormalizer parity).
// Line-anchored only; fenced code passes through. Skips <table>/<pre>/<div>.

const FENCE = /^(\s{0,3})(`{3,}|~{3,})\s*(.*?)\s*$/;
const HR = /^\s*<hr\s*\/?>\s*$/i;
const HEADING = /^\s*<h([1-6])(?:\s[^>]*)?>([\s\S]*?)<\/h\1>\s*$/i;
const PARA_INLINE = /^\s*<p(?:\s[^>]*)?>([\s\S]*?)<\/p>\s*$/i;
const PARA_TAG = /^\s*<\/?p(?:\s[^>]*)?>\s*$/i;
const UL_OPEN = /^\s*<ul(?:\s[^>]*)?>\s*$/i;
const OL_OPEN = /^\s*<ol(?:\s[^>]*)?>\s*$/i;
const LIST_CLOSE = /^\s*<\/(?:ul|ol)>\s*$/i;
const LI = /^\s*<li(?:\s[^>]*)?>([\s\S]*?)<\/li>\s*$/i;
const OL_START = /start\s*=\s*["']?(\d+)/i;
const BQ_INLINE = /^\s*<blockquote(?:\s[^>]*)?>([\s\S]*?)<\/blockquote>\s*$/i;
const BQ_OPEN = /^\s*<blockquote(?:\s[^>]*)?>\s*$/i;
const BQ_CLOSE = /^\s*<\/blockquote>\s*$/i;

export function normalizeHtmlBlocks(text: string): string {
  if (!text.includes("<")) return text;
  const lines = text.split("\n");
  const out: string[] = [];
  let inFence = false;
  let fenceCh = " ";
  let fenceLen = 0;
  const listStack: (number | null)[] = [];
  let bqDepth = 0;

  for (const line of lines) {
    const fence = FENCE.exec(line);
    if (inFence) {
      out.push(line);
      if (fence) {
        const run = fence[2];
        if (run[0] === fenceCh && run.length >= fenceLen && fence[3].trim() === "") inFence = false;
      }
      continue;
    }
    if (fence) {
      inFence = true;
      fenceCh = fence[2][0];
      fenceLen = fence[2].length;
      out.push(line);
      continue;
    }

    const q = "> ".repeat(bqDepth);

    if (BQ_OPEN.test(line)) {
      bqDepth++;
      continue;
    }
    if (BQ_CLOSE.test(line)) {
      if (bqDepth > 0) bqDepth--;
      continue;
    }
    const bqInline = BQ_INLINE.exec(line);
    if (bqInline) {
      out.push(`${q}> ${bqInline[1].trim()}`);
      continue;
    }

    if (HR.test(line)) {
      out.push(`${q}---`);
      continue;
    }

    const heading = HEADING.exec(line);
    if (heading) {
      out.push(`${q}${"#".repeat(Number(heading[1]))} ${heading[2].trim()}`);
      continue;
    }

    if (UL_OPEN.test(line)) {
      listStack.push(null);
      continue;
    }
    if (OL_OPEN.test(line)) {
      const m = OL_START.exec(line);
      listStack.push(m ? Number(m[1]) : 1);
      continue;
    }
    if (LIST_CLOSE.test(line)) {
      listStack.pop();
      continue;
    }
    const li = LI.exec(line);
    if (li) {
      const indent = "  ".repeat(Math.max(0, listStack.length - 1));
      const frame = listStack[listStack.length - 1];
      let marker = "- ";
      if (frame != null) {
        listStack[listStack.length - 1] = frame + 1;
        marker = `${frame}. `;
      }
      out.push(`${q}${indent}${marker}${li[1].trim()}`);
      continue;
    }

    const para = PARA_INLINE.exec(line);
    if (para) {
      out.push(q + para[1].trim());
      if (bqDepth === 0) out.push("");
      continue;
    }
    if (PARA_TAG.test(line)) continue;

    out.push(line);
  }
  return out.join("\n");
}
