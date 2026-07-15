// Split Amaranth approval reader text into meta / 결재선 / 본문 / 첨부.
// formatDocRead (scripts/dev/groupware-reader) concatenates these with bare
// section headers — the detail UI folds line + attachments so the body leads.

export type ApprovalDocSections = {
  meta: string;
  line: string;
  lineCount: number;
  body: string;
  attachments: string;
  attachmentHeader: string;
  attachmentCount: number;
};

const LINE_HEADER = /^결재선\s*$/;
const BODY_HEADER = /^본문\s*$/;
const ATTACH_HEADER = /^첨부\s*(\(.*\))?\s*$/;

function countNumberedRows(block: string): number {
  return block.split("\n").filter((l) => /^\s*\d+\.\s/.test(l)).length;
}

function countAttachRows(block: string): number {
  return block.split("\n").filter((l) => /^\s*\d+\.\s/.test(l)).length;
}

/** Parse reader blob; unknown shapes fall through with body = full text. */
export function parseApprovalDocBody(raw: string): ApprovalDocSections {
  const text = (raw ?? "").replace(/\r\n/g, "\n");
  if (!text.trim()) {
    return { meta: "", line: "", lineCount: 0, body: "", attachments: "", attachmentHeader: "", attachmentCount: 0 };
  }

  const lines = text.split("\n");
  let lineStart = -1;
  let bodyStart = -1;
  let attachStart = -1;
  for (let i = 0; i < lines.length; i++) {
    const t = lines[i].trimEnd();
    if (lineStart < 0 && LINE_HEADER.test(t)) lineStart = i;
    else if (bodyStart < 0 && BODY_HEADER.test(t)) bodyStart = i;
    else if (attachStart < 0 && ATTACH_HEADER.test(t)) attachStart = i;
  }

  // No known markers → treat entire blob as body (legacy / chat paste).
  if (lineStart < 0 && bodyStart < 0 && attachStart < 0) {
    return {
      meta: "",
      line: "",
      lineCount: 0,
      body: text.trim(),
      attachments: "",
      attachmentHeader: "",
      attachmentCount: 0,
    };
  }

  const endOf = (start: number, ...stops: number[]) => {
    const next = stops.filter((s) => s > start).sort((a, b) => a - b)[0];
    return next ?? lines.length;
  };

  const sliceBlock = (headerIdx: number, end: number) =>
    lines
      .slice(headerIdx + 1, end)
      .join("\n")
      .trim();

  let metaEnd = lines.length;
  if (lineStart >= 0) metaEnd = Math.min(metaEnd, lineStart);
  if (bodyStart >= 0) metaEnd = Math.min(metaEnd, bodyStart);
  if (attachStart >= 0) metaEnd = Math.min(metaEnd, attachStart);

  const metaRaw = lines.slice(0, metaEnd).join("\n").trim();
  // Drop reader banner + query line — the detail chrome already shows the title.
  const meta = metaRaw
    .split("\n")
    .filter((l) => {
      const t = l.trim();
      if (!t) return false;
      if (t.startsWith("[그룹웨어")) return false;
      if (t.startsWith("조회:")) return false;
      return true;
    })
    .join("\n");

  const line =
    lineStart >= 0 ? sliceBlock(lineStart, endOf(lineStart, bodyStart, attachStart)) : "";
  const body =
    bodyStart >= 0
      ? sliceBlock(bodyStart, endOf(bodyStart, attachStart))
      : lineStart < 0 && attachStart < 0
        ? text.trim()
        : "";
  const attachmentHeader = attachStart >= 0 ? lines[attachStart].trim() : "";
  const attachments = attachStart >= 0 ? sliceBlock(attachStart, lines.length) : "";

  return {
    meta,
    line,
    lineCount: countNumberedRows(line),
    body,
    attachments,
    attachmentHeader,
    attachmentCount: countAttachRows(attachments),
  };
}
