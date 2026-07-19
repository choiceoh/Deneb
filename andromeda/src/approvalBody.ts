// Split Amaranth approval reader text into meta / 결재선 / 본문 / 첨부.
// formatDocRead (scripts/dev/groupware-reader) concatenates these with bare
// section headers — the detail UI folds line + attachments so the body leads.

export type ApprovalDocSections = {
  // Structured header fields (문서번호/id are agent plumbing and dropped).
  title: string;
  form: string;
  drafter: string;
  draftedAt: string;
  line: string;
  lineCount: number;
  body: string;
  attachments: string;
  attachmentHeader: string;
  attachmentCount: number;
};

const EMPTY: ApprovalDocSections = {
  title: "",
  form: "",
  drafter: "",
  draftedAt: "",
  line: "",
  lineCount: 0,
  body: "",
  attachments: "",
  attachmentHeader: "",
  attachmentCount: 0,
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

export type ApprovalAttachmentRow = {
  /** 1-based index as printed by the reader (also a valid RPC selector). */
  index: number;
  name: string;
  meta: string;
  raw: string;
};

/** Parse "1. 영수증.pdf · 12KB" rows from the 첨부 block. */
export function parseAttachmentRows(block: string): ApprovalAttachmentRow[] {
  const rows: ApprovalAttachmentRow[] = [];
  for (const line of (block ?? "").split("\n")) {
    const m = /^\s*(\d+)\.\s+(.+)$/.exec(line);
    if (!m) continue;
    const rest = m[2].trim();
    const parts = rest.split(/\s+·\s+/);
    const name = (parts[0] ?? rest).trim();
    const meta = parts.slice(1).join(" · ").trim();
    if (!name) continue;
    rows.push({ index: Number(m[1]), name, meta, raw: line.trim() });
  }
  return rows;
}

/** Parse reader blob; unknown shapes fall through with body = full text. */
export function parseApprovalDocBody(raw: string): ApprovalDocSections {
  const text = (raw ?? "").replace(/\r\n/g, "\n");
  if (!text.trim()) {
    return { ...EMPTY };
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
    return { ...EMPTY, body: text.trim() };
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

  // Lift 양식·기안·기안일·제목 into structured header fields; 문서번호/id stay out.
  let title = "";
  let form = "";
  let drafter = "";
  let draftedAt = "";
  for (const rawLine of lines.slice(0, metaEnd)) {
    const t = rawLine.trim();
    if (t.startsWith("제목:")) title = t.slice("제목:".length).trim();
    else if (t.startsWith("양식:")) form = t.slice("양식:".length).trim();
    else if (t.startsWith("기안:")) drafter = t.slice("기안:".length).trim();
    else if (t.startsWith("기안일:")) draftedAt = t.slice("기안일:".length).trim();
  }

  const line = lineStart >= 0 ? sliceBlock(lineStart, endOf(lineStart, bodyStart, attachStart)) : "";
  const body =
    bodyStart >= 0
      ? sliceBlock(bodyStart, endOf(bodyStart, attachStart))
      : lineStart < 0 && attachStart < 0
        ? text.trim()
        : "";
  const attachmentHeader = attachStart >= 0 ? lines[attachStart].trim() : "";
  const attachments = attachStart >= 0 ? sliceBlock(attachStart, lines.length) : "";

  return {
    title,
    form,
    drafter,
    draftedAt,
    line,
    lineCount: countNumberedRows(line),
    body,
    attachments,
    attachmentHeader,
    attachmentCount: countAttachRows(attachments),
  };
}

/** Parse Amaranth date stamps (2026-07-16 / 2026.07.16 / 20260716) → local midnight ms. */
export function approvalDayMs(date?: string): number | null {
  const s = (date ?? "").trim();
  const m = /^(\d{4})[-./]?(\d{2})[-./]?(\d{2})/.exec(s);
  if (!m) return null;
  return new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3])).getTime();
}
