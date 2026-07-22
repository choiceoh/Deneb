import { spawnSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { apiPost, apiPostMaybeBinary, loadSession } from "./client.mjs";

/** Amaranth boxList menuNos (from /gw/gw999A03 under 전자결재). */
export const BOX = {
  pending: "1001000", // 미결문서
  done: "1001100", // 기결문서
  cc: "1001200", // 수신참조문서
  total: "1001500", // 전체결재문서 (list via eap126A04)
};

export const FOLDER_TITLE = {
  pending: "미결문서",
  done: "기결문서",
  cc: "수신참조문서",
  total: "전체결재문서",
};

const MAX_ATTACH_DOWNLOAD = 3;
const MAX_ATTACH_BYTES = 8 * 1024 * 1024;
const MAX_EXTRACT_CHARS = 4_000;
const MAX_APPROVAL_LIST_ENTRIES = 50;

export function normalizeFolder(raw) {
  const f = String(raw || "").trim().toLowerCase();
  if (!f || f === "all" || f === "전체함" || f === "순회") return "all";
  if (["pending", "미결", "미결문서", "미결함", "미결문서함"].includes(f)) return "pending";
  if (["done", "기결", "기결문서", "완료", "종결"].includes(f)) return "done";
  if (["cc", "수신참조", "수신참조문서", "참조", "수신"].includes(f)) return "cc";
  if (["total", "전체결재", "전체결재문서", "전체문서", "결재문서전체", "전체"].includes(f)) {
    return "total";
  }
  return f;
}

/** Decode the subset of HTML entities Amaranth forms commonly emit.
 * Decode `&amp;` last so `&amp;lt;` cannot become a raw `<` via double-unescape. */
function decodeHtmlEntities(s) {
  return String(s)
    .replace(/&nbsp;/gi, " ")
    .replace(/&lt;/gi, "<")
    .replace(/&gt;/gi, ">")
    .replace(/&quot;/gi, '"')
    .replace(/&#39;|&apos;/gi, "'")
    .replace(/&#x([0-9a-f]+);/gi, (_, hex) => {
      const n = Number.parseInt(hex, 16);
      return Number.isFinite(n) && n <= 0x10ffff ? String.fromCodePoint(n) : "";
    })
    .replace(/&#(\d+);/g, (_, dec) => {
      const n = Number.parseInt(dec, 10);
      return Number.isFinite(n) && n <= 0x10ffff ? String.fromCodePoint(n) : "";
    })
    .replace(/&amp;/gi, "&");
}

function stripDangerousBlocks(s) {
  // Loop until stable — nested/partial replacements like `<scr<script>ipt>` need
  // more than one pass. Allow whitespace before `>` in end tags (`</script >`).
  let prev;
  do {
    prev = s;
    s = s.replace(/<script\b[^>]*>[\s\S]*?<\/script\s*>/gi, "");
    s = s.replace(/<style\b[^>]*>[\s\S]*?<\/style\s*>/gi, "");
    // Drop unclosed script/style openings so later tag stripping cannot revive them.
    s = s.replace(/<script\b[^>]*>[\s\S]*$/gi, "");
    s = s.replace(/<style\b[^>]*>[\s\S]*$/gi, "");
  } while (s !== prev);
  return s;
}

function stripHtmlFragment(html, { preserveBreaks = false } = {}) {
  let s = stripDangerousBlocks(String(html || ""));
  s = s.replace(/<(br|hr)\s*\/?>/gi, preserveBreaks ? " DENEBHTMLBREAK " : "\n");
  s = s.replace(/<\/(p|div|li|h[1-6])>/gi, preserveBreaks ? " DENEBHTMLBREAK " : "\n");
  s = s.replace(/<[^>]+>/g, "");
  s = decodeHtmlEntities(s);
  if (!preserveBreaks) return s.replace(/[ \t]+/g, " ").trim();
  return s
    .replace(/\s*DENEBHTMLBREAK\s*/g, "<br>")
    .replace(/(?:<br>){2,}/g, "<br>")
    .replace(/^(?:<br>)+|(?:<br>)+$/g, "")
    .replace(/[ \t]+/g, " ")
    .trim();
}

function attrInt(attrs, name) {
  const m = String(attrs || "").match(new RegExp(`${name}\\s*=\\s*["']?(\\d+)`, "i"));
  return Math.max(1, Number.parseInt(m?.[1] || "1", 10) || 1);
}

/** Expand one HTML table to a rectangular matrix, including colspan/rowspan.
 * Markdown cannot express merged cells, so span continuations become blanks. */
export function htmlTableMatrix(tableHtml) {
  const rowHtml = [...String(tableHtml).matchAll(/<tr\b[^>]*>([\s\S]*?)<\/tr>/gi)].map((m) => m[1]);
  const rows = [];
  const pending = new Map(); // col -> remaining rows occupied by rowspan

  for (const rawRow of rowHtml) {
    const row = [];
    for (const [col, remaining] of [...pending.entries()]) {
      row[col] = "";
      if (remaining <= 1) pending.delete(col);
      else pending.set(col, remaining - 1);
    }

    const cells = [...rawRow.matchAll(/<t[dh]\b([^>]*)>([\s\S]*?)<\/t[dh]>/gi)];
    let col = 0;
    for (const cell of cells) {
      while (row[col] !== undefined) col += 1;
      const colspan = attrInt(cell[1], "colspan");
      const rowspan = attrInt(cell[1], "rowspan");
      const value = stripHtmlFragment(cell[2], { preserveBreaks: true })
        .replace(/\\/g, "\\\\")
        .replace(/\|/g, "\\|");
      row[col] = value;
      for (let i = 1; i < colspan; i += 1) row[col + i] = "";
      if (rowspan > 1) {
        for (let i = 0; i < colspan; i += 1) pending.set(col + i, rowspan - 1);
      }
      col += colspan;
    }
    if (cells.length) rows.push(row);
  }

  const width = Math.max(0, ...rows.map((r) => r.length));
  return rows.map((r) => Array.from({ length: width }, (_, i) => r[i] ?? ""));
}

/** Convert data-shaped HTML tables to Markdown. One-row layout tables (e.g.
 * 금액 labels split across seven cells) stay a readable sentence instead. */
export function htmlTableToMarkdown(tableHtml) {
  const rows = htmlTableMatrix(tableHtml);
  if (!rows.length) return "";
  if (rows.length === 1) return rows[0].filter(Boolean).join(" ").replace(/<br>/g, " ");
  const width = rows[0].length;
  if (width < 2) return rows.map((r) => r.filter(Boolean).join(" ")).filter(Boolean).join("\n");
  const render = (row) => `| ${row.map((v) => String(v).replace(/\n/g, "<br>")).join(" | ")} |`;
  return [render(rows[0]), `| ${rows[0].map(() => "---").join(" | ")} |`, ...rows.slice(1).map(render)].join("\n");
}

/** Extract outermost table blocks without a DOM dependency. Handles nested
 * layout tables by tracking <table> depth instead of a non-greedy regex. */
function extractTableBlocks(html) {
  const s = String(html || "");
  const tags = [...s.matchAll(/<\/?table\b[^>]*>/gi)];
  const blocks = [];
  let depth = 0;
  let start = -1;
  for (const tag of tags) {
    const closing = /^<\/table/i.test(tag[0]);
    if (!closing) {
      if (depth === 0) start = tag.index;
      depth += 1;
    } else if (depth > 0) {
      depth -= 1;
      if (depth === 0 && start >= 0) {
        blocks.push({ start, end: tag.index + tag[0].length, html: s.slice(start, tag.index + tag[0].length) });
        start = -1;
      }
    }
  }
  return blocks;
}

/** Collapse Amaranth form HTML into readable text while preserving true row/column
 * tables as Markdown. Metadata, approval line, and attachments remain separate. */
export function htmlToText(html) {
  let s = String(html || "");
  const blocks = extractTableBlocks(s);
  const rendered = [];
  for (let i = blocks.length - 1; i >= 0; i -= 1) {
    const marker = `DENEBTABLEBLOCK${String(i).padStart(4, "0")}END`;
    rendered[i] = htmlTableToMarkdown(blocks[i].html);
    s = s.slice(0, blocks[i].start) + `\n${marker}\n` + s.slice(blocks[i].end);
  }

  s = stripHtmlFragment(s);
  const lines = s
    .split(/\n+/)
    .map((l) => l.replace(/[ \t]+/g, " ").trim())
    .filter(Boolean);
  const merged = [];
  for (const line of lines) {
    const marker = line.match(/^DENEBTABLEBLOCK(\d{4})END$/);
    if (marker) {
      const table = rendered[Number.parseInt(marker[1], 10)];
      if (table) merged.push(table);
      continue;
    }
    if (
      merged.length &&
      !merged[merged.length - 1].startsWith("|") &&
      (merged[merged.length - 1].length < 8 || line.length < 8) &&
      !/^[0-9]+\./.test(line)
    ) {
      merged[merged.length - 1] = `${merged[merged.length - 1]} ${line}`.trim();
    } else {
      merged.push(line);
    }
  }
  return merged.join("\n").slice(0, 16_000);
}

function firstApprovalValue(...values) {
  for (const value of values) {
    const normalized = String(value ?? "").trim();
    if (normalized) return normalized;
  }
  return "";
}

/** Normalize the two Amaranth approval-list payload shapes into one stable
 * machine-readable record. `folder` is supplied by the caller because neither
 * list endpoint reliably echoes the source box in each document. */
export function normalizeApprovalDoc(d, folder = "") {
  const doc = d || {};
  const folderValue = firstApprovalValue(folder, doc.folder);
  return {
    docId: firstApprovalValue(doc.DOC_ID, doc.doc_id, doc.docId),
    title: firstApprovalValue(doc.DOC_TITLE_ORIGIN, doc.doc_title, doc.DOC_TITLE, doc.title),
    docNo: firstApprovalValue(doc.DOC_NO, doc.doc_no, doc.docNo),
    drafter: firstApprovalValue(
      doc.userNm,
      doc.user_name,
      doc.DISP_TITLE_HEAD,
      doc.displayNm,
      doc.drafter,
    ),
    date: firstApprovalValue(doc.REP_DT, doc.req_dt, doc.draft_dt, doc.rep_dt, doc.date),
    status: firstApprovalValue(doc.RET_ITEM_NM, doc.box_nm, doc.status),
    folder: folderValue ? normalizeFolder(folderValue) : "",
  };
}

function formatApprovalMeta(d, folder = "") {
  const doc = normalizeApprovalDoc(d, folder);
  const bits = [
    doc.title,
    doc.docNo && `문서번호 ${doc.docNo}`,
    doc.drafter && `기안 ${doc.drafter}`,
    doc.date && `일자 ${doc.date}`,
    doc.status && `상태 ${doc.status}`,
    doc.docId && `id=${doc.docId}`,
  ].filter(Boolean);
  return bits.join(" · ");
}

function queryFrom(query, matchText) {
  const q = String(query || "").trim();
  if (q) return q;
  const m = String(matchText || "").match(/제목:\s*(.+)/);
  if (m) return m[1].trim();
  for (const line of String(matchText || "").split(/\n+/)) {
    const s = line.trim();
    if (!s || /^(종류|상태|기안|부서|본문):/.test(s)) continue;
    if (s.length >= 4) return s.slice(0, 80);
  }
  return "";
}

async function listBoxPortlet(folder, limit) {
  const code = BOX[folder];
  const r = await apiPost("/eap/eap106A03", {
    boxList: `${code},`,
    listCount: String(limit),
  });
  if (r.json?.resultCode !== 0 && r.json?.resultCode !== 200) {
    throw new Error(r.json?.resultMsg || `eap106A03 failed (${r.status})`);
  }
  return r.json?.resultData?.EaPortletDocList || [];
}

async function listTotal(limit) {
  const r = await apiPost("/eap/eap126A04", {
    boxCodes: ["10", "20", "30", "40", "50", "60"],
    pageCode: "UBA",
    upperMenuNo: "1000900",
    menuNo: BOX.total,
  });
  if (r.json?.resultCode !== 0 && r.json?.resultCode !== 200) {
    throw new Error(r.json?.resultMsg || `eap126A04 failed (${r.status})`);
  }
  const docs = r.json?.resultData?.docList || [];
  return docs.slice(0, limit);
}

async function fetchDocDetail(docId) {
  const id = String(docId);
  const r = await apiPost("/eap/eap111A23", { docId: id });
  if (r.json?.resultCode !== 0 && r.json?.resultCode !== 200) {
    throw new Error(r.json?.resultMsg || `eap111A23 failed (${r.status})`);
  }
  return r.json?.resultData || {};
}

async function fetchDocLine(docId) {
  const r = await apiPost("/eap/eap126A05", { docId: String(docId) });
  if (r.json?.resultCode !== 0 && r.json?.resultCode !== 200) return [];
  const data = r.json?.resultData;
  return Array.isArray(data) ? data : [];
}

async function fetchAttachList(docId) {
  const r = await apiPost("/eap/eap110A90", { docId: String(docId) });
  if (r.json?.resultCode !== 0 && r.json?.resultCode !== 200) return [];
  const list = r.json?.resultData?.list;
  return Array.isArray(list) ? list : [];
}

function authKeyMap(docId) {
  const sess = loadSession() || {};
  return JSON.stringify({
    compSeq: String(sess.compSeq || "1000"),
    empSeq: String(sess.empSeq || ""),
    docId: String(docId),
    migYn: "0",
  });
}

const OCR_VL_URL = (process.env.DENEB_OCR_VL_URL || "").replace(/\/$/, "");
const OCR_VL_MODEL = process.env.DENEB_OCR_VL_MODEL || "paddleocr-vl";
const OCR_TIMEOUT_MS = 60_000;
const MAX_OCR_PDF_PAGES = 2;

// OCR is on when the fleet PaddleOCR-VL server is wired (gateway sets
// DENEB_OCR_VL_URL) or explicitly forced with DENEB_GROUPWARE_OCR=1; set
// DENEB_GROUPWARE_OCR=0 to force it off (e.g. latency-sensitive smoke).
function ocrEnabled() {
  if (process.env.DENEB_GROUPWARE_OCR === "0") return false;
  return process.env.DENEB_GROUPWARE_OCR === "1" || Boolean(OCR_VL_URL);
}

const IMAGE_EXTS = ["jpg", "jpeg", "png", "tif", "tiff", "bmp", "webp"];

// PaddleOCR-VL near the token ceiling can loop — the same line, or two lines
// alternating, echoed dozens of times. Cap how many times any one normalized
// line may appear (catches both consecutive and alternating loops) and bound the
// total line count so a runaway page can't dominate the card.
const MAX_OCR_LINES = 80;
export function dedupeOcrText(s) {
  const counts = new Map();
  const out = [];
  for (const raw of String(s).split("\n")) {
    const key = raw.replace(/\s+/g, " ").trim();
    if (key) {
      const n = (counts.get(key) || 0) + 1;
      counts.set(key, n);
      if (n > 3) continue; // keep at most 3 of any repeated line
    }
    out.push(raw);
    if (out.length >= MAX_OCR_LINES) {
      out.push("…(생략)");
      break;
    }
  }
  return out.join("\n").trim();
}

// A "real" token is a run that looks like actual content: 2+ Hangul, a 3+ digit
// number, or a 4+ letter Latin word. Symbol-soup OCR of a photo/stamp is mostly
// isolated 1–2 char fragments between punctuation, so it has almost none.
const REAL_TOKEN = /[가-힣]{2,}|\d{3,}|[A-Za-z]{4,}/;

// A line is noise when it carries no real token (kept short lines like "끝." pass
// only if they contain Hangul/among the doc, judged at the block level below).
function hasRealToken(line) {
  return REAL_TOKEN.test(line);
}

// Blank out symbol-soup OCR (photo collages, stamps) and trim to budget: if few
// lines carry a real token there is no text worth surfacing, so return "" and let
// the caller show a short note instead of pages of garbage.
export function cleanOcr(s) {
  const out = String(s || "").trim();
  if (!out) return "";
  const lines = out.split("\n").filter((l) => l.trim());
  if (!lines.length) return "";
  const real = lines.filter(hasRealToken).length / lines.length;
  if (real < 0.5) return "";
  return out.slice(0, MAX_EXTRACT_CHARS);
}

/** PaddleOCR-VL (fleet vLLM, OpenAI-compatible). Far better than tesseract on
 *  Korean business docs — tables, stamps, mixed numbers. Falls back to tesseract
 *  when the server is unset/unreachable so extraction degrades gracefully. */
async function ocrViaVL(buffer, mime) {
  if (!OCR_VL_URL) return "";
  const dataURI = `data:${mime || "image/png"};base64,${buffer.toString("base64")}`;
  const ctrl = new AbortController();
  const timer = setTimeout(() => ctrl.abort(), OCR_TIMEOUT_MS);
  try {
    const res = await fetch(`${OCR_VL_URL}/v1/chat/completions`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      signal: ctrl.signal,
      body: JSON.stringify({
        model: OCR_VL_MODEL,
        temperature: 0,
        max_tokens: 1536,
        messages: [{
          role: "user",
          content: [
            { type: "image_url", image_url: { url: dataURI } },
            { type: "text", text: "OCR:" },
          ],
        }],
      }),
    });
    if (!res.ok) return "";
    const j = await res.json();
    return dedupeOcrText(String(j?.choices?.[0]?.message?.content || "").trim());
  } catch {
    return "";
  } finally {
    clearTimeout(timer);
  }
}

function tesseractFile(filePath) {
  const r = spawnSync("tesseract", [filePath, "stdout", "-l", "kor+eng", "--psm", "6"], {
    encoding: "utf8",
    maxBuffer: 4 * 1024 * 1024,
    timeout: 60_000,
  });
  return r.status === 0 ? String(r.stdout || "").trim() : "";
}

function mimeForExt(ext) {
  const e = String(ext || "").toLowerCase();
  if (e === "jpg" || e === "jpeg") return "image/jpeg";
  if (e === "png") return "image/png";
  if (e === "webp") return "image/webp";
  if (e === "bmp") return "image/bmp";
  if (e === "tif" || e === "tiff") return "image/tiff";
  return "image/png";
}

// Rasterize a (likely scanned) PDF to PNG pages so OCR-VL can read them. Bounded
// to the first MAX_OCR_PDF_PAGES pages to keep latency predictable on the phone
// enrich path. Returns absolute page-image paths under dir.
function rasterizePdf(pdfPath, dir) {
  const prefix = path.join(dir, "page");
  const r = spawnSync(
    "pdftoppm",
    ["-png", "-r", "150", "-f", "1", "-l", String(MAX_OCR_PDF_PAGES), pdfPath, prefix],
    { timeout: 60_000 },
  );
  if (r.status !== 0) return [];
  return fs
    .readdirSync(dir)
    .filter((f) => f.startsWith("page") && f.endsWith(".png"))
    .sort()
    .map((f) => path.join(dir, f));
}

async function extractTextFromFile(filePath, ext, dir) {
  const e = String(ext || "").toLowerCase().replace(/^\./, "");
  if (e === "pdf") {
    const r = spawnSync("pdftotext", ["-layout", "-nopgbrk", filePath, "-"], {
      encoding: "utf8",
      maxBuffer: 4 * 1024 * 1024,
      timeout: 30_000,
    });
    const text = r.status === 0 ? String(r.stdout || "").trim() : "";
    if (text) return text.slice(0, MAX_EXTRACT_CHARS);
    // Empty → likely a scanned PDF. Rasterize + OCR when enabled.
    if (!ocrEnabled()) return "";
    const pages = rasterizePdf(filePath, dir);
    const parts = [];
    for (const pg of pages) {
      const buf = fs.readFileSync(pg);
      let pt = await ocrViaVL(buf, "image/png");
      if (!pt) pt = dedupeOcrText(tesseractFile(pg));
      if (pt) parts.push(pt);
    }
    return cleanOcr(parts.join("\n"));
  }
  if (IMAGE_EXTS.includes(e)) {
    if (!ocrEnabled()) return "";
    const buf = fs.readFileSync(filePath);
    let out = await ocrViaVL(buf, mimeForExt(e));
    if (!out) out = dedupeOcrText(tesseractFile(filePath));
    return cleanOcr(out);
  }
  if (["txt", "csv", "md", "log"].includes(e)) {
    try {
      return fs.readFileSync(filePath, "utf8").slice(0, MAX_EXTRACT_CHARS);
    } catch {
      return "";
    }
  }
  return "";
}

function wantExtract(ext) {
  const e = String(ext || "").toLowerCase();
  if (["pdf", "txt", "csv", "md", "log"].includes(e)) return true;
  if (IMAGE_EXTS.includes(e)) return ocrEnabled();
  return false;
}

async function downloadAndExtract(docId, file) {
  const fileSn = file.fileKey ?? file.fileSn;
  const name = file.dispFileNm || file.fileNm || file.filePath || `file-${fileSn}`;
  const ext = String(file.fileExtsn || path.extname(String(file.filePath || "")) || "")
    .replace(/^\./, "")
    .toLowerCase();
  const size = Number(file.fileSize || 0);
  if (!fileSn) return { name, ext, size, extracted: "", note: "fileKey 없음" };
  if (!wantExtract(ext)) {
    const img = ["jpg", "jpeg", "png", "tif", "tiff", "bmp", "webp"].includes(ext);
    return {
      name,
      ext,
      size,
      extracted: "",
      note: img ? "이미지 — OCR 생략 (DENEB_GROUPWARE_OCR=1 시 추출)" : "추출 미지원 형식",
    };
  }
  if (size > MAX_ATTACH_BYTES) {
    return { name, ext, size, extracted: "", note: `용량 초과(${size}B) — 메타만` };
  }

  const out = await apiPostMaybeBinary("/ecm/ecm001A03", {
    moduleGbn: "EAP",
    authKeyMap: authKeyMap(docId),
    fileSn: Number(fileSn) || fileSn,
  });
  if (!out.binary || !out.buffer?.length) {
    const msg = out.json?.resultMsg || `download failed (${out.status})`;
    return { name, ext, size, extracted: "", note: String(msg).slice(0, 120) };
  }

  const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "deneb-gw-att-"));
  const tmpFile = path.join(tmpDir, `${String(fileSn)}.${ext || "bin"}`);
  try {
    fs.writeFileSync(tmpFile, out.buffer);
    const extracted = await extractTextFromFile(tmpFile, ext, tmpDir);
    return {
      name,
      ext,
      size: out.buffer.length,
      extracted,
      note: extracted ? "" : "텍스트 추출 없음(빈 스캔이거나 OCR 실패)",
    };
  } finally {
    try {
      fs.rmSync(tmpDir, { recursive: true, force: true });
    } catch {
      /* ignore */
    }
  }
}

export function humanSize(bytes) {
  const n = Number(bytes);
  if (!Number.isFinite(n) || n <= 0) return "";
  if (n < 1024) return `${n}B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(0)}KB`;
  return `${(n / (1024 * 1024)).toFixed(1)}MB`;
}

export function attachmentName(file) {
  let name = String(file.dispFileNm || file.fileNm || file.filePath || "첨부").trim();
  // Amaranth often stores the display name already numbered ("1. 지출영수증"); drop
  // that leading ordinal so the reader's own numbered list doesn't read "1. 1. …".
  name = name.replace(/^\s*\d+\s*[.)]\s*/, "");
  const ext = String(file.fileExtsn || path.extname(String(file.filePath || "")) || "")
    .replace(/^\./, "")
    .toLowerCase();
  return ext && !name.toLowerCase().endsWith(`.${ext}`) ? `${name}.${ext}` : name;
}

// Initial document read lists attachment titles only. The agent explicitly chooses
// one attachment via action=attachment before any download/OCR work happens.
async function formatAttachments(docId) {
  const list = await fetchAttachList(docId);
  if (!list.length) return "";
  const lines = [
    `첨부 (${list.length}건 · 내용 미열람)`,
    `필요한 파일만 groupware action=attachment, doc_id=${docId}, attachment=<번호 또는 파일명> 으로 읽기`,
  ];
  for (const [i, file] of list.entries()) {
    const size = humanSize(file.fileSize);
    lines.push(`${i + 1}. ${attachmentName(file)}${size ? ` · ${size}` : ""}`);
  }
  return lines.join("\n");
}

export function selectAttachment(list, selector) {
  const raw = String(selector || "").trim();
  if (!raw) throw new Error("attachment requires a file number, filename, fileKey, or fileId");

  if (/^\d+$/.test(raw)) {
    const index = Number.parseInt(raw, 10) - 1;
    if (index >= 0 && index < list.length) return list[index];
    const byKey = list.find((f) => String(f.fileKey ?? f.fileSn ?? "") === raw);
    if (byKey) return byKey;
  }

  const needle = raw.toLowerCase();
  const exact = list.filter((f) =>
    [attachmentName(f), f.dispFileNm, f.fileNm, f.filePath, f.fileId, f.fileKey]
      .filter((v) => v != null)
      .some((v) => String(v).trim().toLowerCase() === needle),
  );
  if (exact.length === 1) return exact[0];

  const partial = list.filter((f) =>
    [attachmentName(f), f.dispFileNm, f.fileNm, f.filePath]
      .filter(Boolean)
      .some((v) => String(v).toLowerCase().includes(needle)),
  );
  if (partial.length === 1) return partial[0];
  if (partial.length > 1 || exact.length > 1) {
    const matches = (exact.length ? exact : partial).map(attachmentName).join(", ");
    throw new Error(`첨부 선택이 모호합니다: ${matches}`);
  }
  throw new Error(`첨부를 찾지 못했습니다: ${raw}`);
}

export async function readApprovalAttachment(docId, selector) {
  const id = String(docId || "").trim();
  if (!id) throw new Error("attachment requires --doc-id");
  const list = await fetchAttachList(id);
  if (!list.length) throw new Error(`첨부가 없습니다: docId=${id}`);
  const file = selectAttachment(list, selector);
  const got = await downloadAndExtract(id, file);
  const header = [
    `[그룹웨어 전자결재 · 선택 첨부]`,
    `docId: ${id}`,
    `파일: ${attachmentName(file)}`,
    got.size ? `크기: ${humanSize(got.size)}` : "",
  ].filter(Boolean).join("\n");
  if (got.extracted) return `${header}\n\n추출 본문\n${got.extracted}`;
  return `${header}\n\n(${got.note || "텍스트 추출 결과 없음"})`;
}

function mimeFromExt(ext) {
  switch (String(ext || "").toLowerCase()) {
    case "pdf":
      return "application/pdf";
    case "png":
      return "image/png";
    case "jpg":
    case "jpeg":
      return "image/jpeg";
    case "gif":
      return "image/gif";
    case "webp":
      return "image/webp";
    case "tif":
    case "tiff":
      return "image/tiff";
    case "bmp":
      return "image/bmp";
    case "txt":
    case "log":
    case "csv":
    case "md":
      return "text/plain; charset=utf-8";
    case "hwp":
      return "application/x-hwp";
    case "hwpx":
      return "application/hwp+zip";
    default:
      return "application/octet-stream";
  }
}

/** Download raw ECM bytes (no OCR) for the HTTP attachment route. */
export async function downloadApprovalAttachmentBinary(docId, selector) {
  const id = String(docId || "").trim();
  if (!id) throw new Error("attachment-download requires --doc-id");
  const list = await fetchAttachList(id);
  if (!list.length) throw new Error(`첨부가 없습니다: docId=${id}`);
  const file = selectAttachment(list, selector);
  const fileSn = file.fileKey ?? file.fileSn;
  const name = attachmentName(file);
  const ext = String(file.fileExtsn || path.extname(String(file.filePath || "")) || "")
    .replace(/^\./, "")
    .toLowerCase();
  const size = Number(file.fileSize || 0);
  if (!fileSn) {
    return { filename: name, mimeType: mimeFromExt(ext), size, base64: "", error: "fileKey 없음" };
  }
  if (size > MAX_ATTACH_BYTES) {
    return {
      filename: name,
      mimeType: mimeFromExt(ext),
      size,
      base64: "",
      error: `용량 초과(${size}B)`,
    };
  }
  const out = await apiPostMaybeBinary("/ecm/ecm001A03", {
    moduleGbn: "EAP",
    authKeyMap: authKeyMap(docId),
    fileSn: Number(fileSn) || fileSn,
  });
  if (!out.binary || !out.buffer?.length) {
    const msg = out.json?.resultMsg || `download failed (${out.status})`;
    return { filename: name, mimeType: mimeFromExt(ext), size, base64: "", error: String(msg).slice(0, 120) };
  }
  return {
    filename: name,
    mimeType: mimeFromExt(ext),
    size: out.buffer.length,
    base64: Buffer.from(out.buffer).toString("base64"),
  };
}

function formatLine(lines) {
  if (!lines.length) return "";
  const rows = lines.map((l, i) => {
    const bits = [
      l.user_name || l.userNm,
      l.grade_name || l.gradeNm,
      l.act_nm || l.actNm,
      l.doc_line_sts_nm || l.app_sts_nm,
      l.app_dt && String(l.app_dt).slice(0, 19).replace("T", " "),
    ].filter(Boolean);
    return `  ${i + 1}. ${bits.join(" · ")}`;
  });
  return `결재선\n${rows.join("\n")}`;
}

function formatDocRead(folderTitle, query, detail, lines, attachBlock) {
  const meta = [
    detail.doc_title && `제목: ${detail.doc_title}`,
    detail.doc_no && `문서번호: ${detail.doc_no}`,
    detail.form_nm && `양식: ${detail.form_nm}`,
    (detail.userNm || detail.displayNm) &&
      `기안: ${detail.displayNm || detail.userNm}${detail.deptNm ? ` (${detail.deptNm})` : ""}`,
    detail.rep_dt && `기안일: ${detail.rep_dt}`,
    detail.doc_id && `id: ${detail.doc_id}`,
  ]
    .filter(Boolean)
    .join("\n");
  const body = htmlToText(detail.doc_contents || detail.inter_contents || "");
  const lineBlock = formatLine(lines);
  return (
    `[그룹웨어 전자결재 · ${folderTitle}]\n` +
    `조회: ${query}\n\n` +
    meta +
    (lineBlock ? `\n\n${lineBlock}` : "") +
    (body ? `\n\n본문\n${body}` : "\n\n(본문 없음)") +
    (attachBlock ? `\n\n${attachBlock}` : "")
  );
}

export async function listApprovalEntries(folder, limit, loaders = {}) {
  const lim = Math.min(Math.max(limit || 20, 1), MAX_APPROVAL_LIST_ENTRIES);
  const normalizedFolder = normalizeFolder(folder);
  const loadBox = loaders.listBoxPortlet || listBoxPortlet;
  const loadTotal = loaders.listTotal || listTotal;
  if (normalizedFolder === "all") {
    const entries = [];
    for (const key of ["pending", "done", "cc", "total"]) {
      entries.push(
        ...(await listApprovalEntries(key, Math.max(4, Math.ceil(lim / 4)), loaders)),
      );
    }
    return entries;
  }
  const docs =
    normalizedFolder === "total" ? await loadTotal(lim) : await loadBox(normalizedFolder, lim);
  return docs.map((doc) => normalizeApprovalDoc(doc, normalizedFolder));
}

export async function listApproval(folder, limit, loaders = {}) {
  const lim = Math.min(Math.max(limit || 20, 1), MAX_APPROVAL_LIST_ENTRIES);
  if (folder === "all") {
    const parts = [];
    for (const key of ["pending", "done", "cc", "total"]) {
      parts.push(await listApproval(key, Math.max(4, Math.ceil(lim / 4)), loaders));
    }
    return parts.join("\n\n");
  }
  const title = FOLDER_TITLE[folder] || folder;
  const docs = await listApprovalEntries(folder, lim, loaders);
  if (!docs.length) return `전자결재 · ${title}\n\n(문서 없음)`;
  const lines = docs.map((d, i) => `${i + 1}. ${formatApprovalMeta(d, folder)}`);
  return `전자결재 · ${title} (${docs.length}건)\n\n${lines.join("\n")}`;
}

export async function readApproval(folder, query, matchText, loaders = {}) {
  const q = queryFrom(query, matchText);
  if (!q) throw new Error("read requires --query or notification body on stdin");

  const loadBox = loaders.listBoxPortlet || listBoxPortlet;
  const loadTotal = loaders.listTotal || listTotal;
  const loadDetail = loaders.fetchDocDetail || fetchDocDetail;
  const loadLine = loaders.fetchDocLine || fetchDocLine;
  const loadAttachments = loaders.formatAttachments || formatAttachments;
  const folders = folder === "all" ? ["pending", "done", "cc", "total"] : [folder];
  for (const key of folders) {
    let docs;
    if (key === "total") docs = await loadTotal(MAX_APPROVAL_LIST_ENTRIES);
    else docs = await loadBox(key, MAX_APPROVAL_LIST_ENTRIES);
    const hit = docs.find((d) => {
      const title = d.DOC_TITLE_ORIGIN || d.doc_title || "";
      const no = d.DOC_NO || d.doc_no || "";
      const id = String(d.DOC_ID || d.doc_id || "");
      return (
        title.includes(q) ||
        q.includes(title.slice(0, 12)) ||
        no.includes(q) ||
        id === q
      );
    });
    if (!hit) continue;
    const docId = hit.DOC_ID || hit.doc_id;
    const [detail, lines, attachBlock] = await Promise.all([
      loadDetail(docId),
      loadLine(docId),
      loadAttachments(docId).catch(() => ""),
    ]);
    if (!detail.doc_contents && !detail.doc_title) {
      return (
        `[그룹웨어 전자결재 · ${FOLDER_TITLE[key]}]\n조회: ${q}\n\n` +
        formatApprovalMeta(hit)
      );
    }
    return formatDocRead(FOLDER_TITLE[key], q, detail, lines, attachBlock);
  }
  throw new Error(`문서를 찾지 못했습니다: ${q}`);
}

/** Normalize one recent-board payload row into the stable JSON boundary used by
 * the gateway radar. Amaranth has emitted both snake_case and camelCase keys. */
export function normalizeBoardPost(a) {
  const post = a || {};
  return {
    postId: firstApprovalValue(post.art_seq_no, post.artSeqNo, post.postId),
    title: firstApprovalValue(post.art_title, post.artTitle, post.title),
    author: firstApprovalValue(post.mbr_nick, post.mbrNick, post.author),
    date: firstApprovalValue(post.write_date, post.writeDate, post.date),
    categoryId: firstApprovalValue(post.cat_seq_no, post.catSeqNo, post.categoryId),
  };
}

async function fetchBoardEntries(limit) {
  const lim = Math.min(Math.max(limit || 20, 1), 50);
  const r = await apiPost("/board/APIHandler/getNewNoticeListForPortlet", {
    page: "1",
    pageSize: String(lim),
  });
  if (r.json?.resultCode !== 0 && r.json?.resultCode !== 200) {
    throw new Error(r.json?.resultMsg || `board list failed (${r.status})`);
  }
  return r.json?.resultData?.articleList || [];
}

export async function listBoardEntries(limit, loaders = {}) {
  const lim = Math.min(Math.max(limit || 20, 1), 50);
  const loadEntries = loaders.fetchBoardEntries || fetchBoardEntries;
  const arts = await loadEntries(lim);
  return arts.map(normalizeBoardPost);
}

export async function listBoard(limit, loaders = {}) {
  const arts = await listBoardEntries(limit, loaders);
  if (!arts.length) return "게시판 최근 글\n\n(글 없음)";
  const lines = arts.map((a, i) => {
    const bits = [
      a.title,
      a.author && `작성 ${a.author}`,
      a.date && `일자 ${a.date}`,
      a.postId && `id=${a.postId}`,
    ].filter(Boolean);
    return `${i + 1}. ${bits.join(" · ")}`;
  });
  return `게시판 최근 글 (${arts.length}건)\n\n${lines.join("\n")}`;
}

export async function readBoard(query) {
  const q = String(query || "").trim();
  if (!q) throw new Error("board read requires --query");
  const r = await apiPost("/board/APIHandler/getNewNoticeListForPortlet", {
    page: "1",
    // Keep the read lookup window aligned with ListBoardPosts/BoardRadar so
    // every candidate from the current structured snapshot remains readable.
    pageSize: "50",
  });
  const arts = r.json?.resultData?.articleList || [];
  const hit = arts.find(
    (a) =>
      String(a.art_seq_no) === q ||
      (a.art_title || "").includes(q) ||
      q.includes((a.art_title || "").slice(0, 12)),
  );
  if (!hit) throw new Error(`게시글을 찾지 못했습니다: ${q}`);

  const view = await apiPost("/board/APIHandler/ViewPost", {
    art_seq_no: hit.art_seq_no,
    adminPage: "N",
  });
  if (view.json?.resultCode !== 0 && view.json?.resultCode !== 200) {
    throw new Error(view.json?.resultMsg || `ViewPost failed (${view.status})`);
  }
  const art = view.json?.resultData?.art || {};
  const body =
    htmlToText(art.art_content || "") ||
    String(art.contents_text || "").trim() ||
    htmlToText(art.sub_content || "");

  return (
    `[그룹웨어 게시판]\n조회: ${q}\n\n` +
    [
      `제목: ${art.art_title || hit.art_title}`,
      (art.mbr_nick || hit.mbr_nick) && `작성: ${art.mbr_nick || hit.mbr_nick}`,
      (art.write_date || hit.write_date) && `일자: ${art.write_date || hit.write_date}`,
      (art.art_seq_no || hit.art_seq_no) && `id: ${art.art_seq_no || hit.art_seq_no}`,
    ]
      .filter(Boolean)
      .join("\n") +
    (body ? `\n\n본문\n${body}` : "\n\n(본문 없음)")
  );
}

/** The signed-in user's id on an eap126A05 line (Amaranth stores it as user_id). */
function lineEmpId(line) {
  return String(line.user_id ?? line.emp_seq ?? line.empSeq ?? line.userId ?? "");
}

/**
 * Pick the operator's actionable approval line. Amaranth marks the line awaiting
 * this user as app_sts "20" (진행); already-approved lines are "30", downstream
 * lines "70" (예결). Require an exactly-pending line owned by empSeq so a mutate
 * can never target someone else's slot or an already-settled one. Exported for
 * tests — real payloads key the user as user_id, not emp_seq (that field mismatch
 * silently missed every line before this).
 */
export function selectApprovalLine(lines, empSeq) {
  const me = String(empSeq || "");
  if (!me) return null;
  return (
    lines.find((l) => lineEmpId(l) === me && String(l.app_sts ?? l.appSts ?? "") === "20") || null
  );
}

/** docLineSts: 30=승인, 50=반려. Never call without an explicit operator decision. */
export async function actApproval(docId, decision, comment = "") {
  // Opt-in mutate: the feed/Go path sets this; a bare CLI call stays read-safe.
  if (process.env.DENEB_GROUPWARE_ACT !== "1") {
    throw new Error("act blocked — set DENEB_GROUPWARE_ACT=1 to allow Amaranth approve/reject");
  }
  const id = String(docId || "").trim();
  if (!id) throw new Error("act requires --doc-id");
  const d = String(decision || "").trim().toLowerCase();
  let docLineSts;
  if (d === "approve" || d === "승인") docLineSts = "30";
  else if (d === "reject" || d === "반려") docLineSts = "50";
  else throw new Error(`unknown decision ${decision} (approve|reject)`);

  const sess = loadSession();
  if (!sess?.empSeq) throw new Error("session missing empSeq — re-login");

  const lines = await fetchDocLine(id);
  const mine = selectApprovalLine(lines, sess.empSeq);
  if (!mine) {
    // Distinguish "not my turn yet / already done" from "not on this line at all".
    const onLine = lines.some((l) => lineEmpId(l) === String(sess.empSeq));
    throw new Error(
      onLine
        ? `지금 내 차례가 아닙니다 (docId=${id}) — 이미 처리됐거나 상위 결재 대기 중`
        : `내 결재선이 아닙니다 (docId=${id}, empSeq=${sess.empSeq})`,
    );
  }

  const actID = String(mine.act_id ?? mine.actID ?? "3000");
  const docLineMSeq = mine.doc_line_m_seq ?? mine.docLineMSeq;
  const docLineSSeq = mine.doc_line_s_seq ?? mine.docLineSSeq;
  if (docLineMSeq == null || docLineSSeq == null) {
    throw new Error("결재선 seq 누락");
  }

  const r = await apiPost("/eap/eap110A21", {
    docID: id,
    actID,
    docLineMSeq,
    docLineSSeq,
    docLineSts,
    docComment: String(comment || ""),
    iframeHtml: "",
  });
  const code = r.json?.resultCode;
  const rv = r.json?.resultData?.resultValue;
  if (code !== 0 && code !== 200) {
    throw new Error(r.json?.resultMsg || `eap110A21 failed code=${code}`);
  }
  if (rv != null && Number(rv) !== 0) {
    throw new Error(`eap110A21 resultValue=${rv} msg=${r.json?.resultMsg || ""}`);
  }
  const label = docLineSts === "30" ? "승인" : "반려";
  return `결재 ${label} 완료 · docId=${id} · line=${docLineMSeq}/${docLineSSeq}`;
}


/** KST calendar helpers for sales closing (매출마감) periods. */
function kstParts(d = new Date()) {
  const fmt = new Intl.DateTimeFormat("en-CA", {
    timeZone: "Asia/Seoul",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  });
  // en-CA → YYYY-MM-DD
  const [y, m, day] = fmt.format(d).split("-").map((x) => Number(x));
  return { y, m, d: day };
}

function ymd(y, m, d) {
  return `${String(y).padStart(4, "0")}${String(m).padStart(2, "0")}${String(d).padStart(2, "0")}`;
}

function ymdDash(s) {
  const t = String(s || "");
  if (t.length === 8) return `${t.slice(0, 4)}-${t.slice(4, 6)}-${t.slice(6, 8)}`;
  return t;
}

/**
 * Resolve a sales period.
 * folder/query presets: ytd|month|today|year|last_year
 * explicit: query "YYYYMMDD:YYYYMMDD" or "YYYY-MM-DD~YYYY-MM-DD"
 */
export function resolveSalesPeriod(folder = "", query = "", now = new Date()) {
  const q = String(query || "").trim();
  const range = q.match(
    /^(\d{4})-?(\d{2})-?(\d{2})\s*[:~～-]\s*(\d{4})-?(\d{2})-?(\d{2})$/,
  );
  if (range) {
    const from = `${range[1]}${range[2]}${range[3]}`;
    const to = `${range[4]}${range[5]}${range[6]}`;
    return { from, to, label: `${ymdDash(from)} ~ ${ymdDash(to)} (지정)` };
  }
  const key = String(folder || q || "ytd")
    .trim()
    .toLowerCase()
    .replace(/\s+/g, "");
  const { y, m, d } = kstParts(now);
  const today = ymd(y, m, d);
  if (["today", "오늘", "당일"].includes(key)) {
    return { from: today, to: today, label: `${ymdDash(today)} (오늘)` };
  }
  if (["month", "이번달", "당월", "월"].includes(key)) {
    return {
      from: ymd(y, m, 1),
      to: today,
      label: `${y}-${String(m).padStart(2, "0")} (당월·오늘까지)`,
    };
  }
  if (["year", "올해", "연간", "당해"].includes(key)) {
    return {
      from: ymd(y, 1, 1),
      to: today,
      label: `${y}년 (연초~오늘)`,
    };
  }
  if (["last_year", "lastyear", "작년", "전년"].includes(key)) {
    const ly = y - 1;
    return {
      from: ymd(ly, 1, 1),
      to: ymd(ly, 12, 31),
      label: `${ly}년 (전년)`,
    };
  }
  // 전년동기: 작년 1/1 ~ 작년 오늘 날짜 — YTD 와 YoY 로 맞비교하는 창.
  if (["yoy", "전년동기", "전년동기간"].includes(key)) {
    const ly = y - 1;
    return {
      from: ymd(ly, 1, 1),
      to: ymd(ly, m, d),
      label: `${ly}년 동기 (${ymdDash(ymd(ly, 1, 1))} ~ ${ymdDash(ymd(ly, m, d))})`,
    };
  }
  if (["h1", "상반기"].includes(key)) {
    return {
      from: ymd(y, 1, 1),
      to: ymd(y, 6, 30),
      label: `${y}년 상반기`,
    };
  }
  if (["h2", "하반기"].includes(key)) {
    return {
      from: ymd(y, 7, 1),
      to: ymd(y, 12, 31),
      label: `${y}년 하반기`,
    };
  }
  // default ytd
  return {
    from: ymd(y, 1, 1),
    to: today,
    label: `${y} YTD (${ymdDash(ymd(y, 1, 1))} ~ ${ymdDash(today)})`,
  };
}

export function formatWon(n) {
  const v = Number(n) || 0;
  const abs = Math.abs(v);
  const sign = v < 0 ? "-" : "";
  const eok = Math.floor(abs / 100_000_000);
  const man = Math.floor((abs % 100_000_000) / 10_000);
  const rest = abs % 10_000;
  const parts = [];
  if (eok) parts.push(`${eok.toLocaleString("ko-KR")}억`);
  if (man) parts.push(`${man.toLocaleString("ko-KR")}만`);
  if (rest || parts.length === 0) parts.push(`${rest.toLocaleString("ko-KR")}`);
  // Human 억/만 only — the raw digit paren duplicated the same figure (2026-07-16).
  return `${sign}${parts.join(" ")}원`;
}

function sumField(rows, field) {
  let s = 0;
  for (const r of rows) {
    const v = Number(r?.[field]);
    if (Number.isFinite(v)) s += v;
  }
  return s;
}

/** 매출마감현황 summary via signed /logis/blg0070/0lo00001 (공급가액=clsgAm). */
export async function summarySales(folder = "ytd", query = "") {
  const period = resolveSalesPeriod(folder, query);
  const body = {
    divCds: [],
    deptCds: [],
    empCds: [],
    clsDtFr: period.from,
    clsDtTo: period.to,
    clsNb: "",
    remarkDc: "",
    remarkDcFg: "",
    gubun: "",
    isTotalItemCdSelect: false,
    itemCds: [],
    itemgrpCds: [],
    lCds: [],
    mCds: [],
    sCds: [],
  };
  const r = await apiPost("/logis/blg0070/0lo00001", body);
  const code = r.json?.resultCode;
  if (code !== 0 && code !== 200) {
    throw new Error(r.json?.resultMsg || `매출마감 조회 실패 code=${code}`);
  }
  const rows = Array.isArray(r.json?.resultData)
    ? r.json.resultData
    : r.json?.resultData?.data || [];
  const supply = sumField(rows, "clsgAm");

  // Operator surface: 기간 + 건수 + 매출 only.
  // 수량합·상위 항목·VAT/합계 are noise (2026-07-16).
  return [
    "매출",
    `기간: ${period.label}`,
    `건수: ${rows.length.toLocaleString("ko-KR")}`,
    `매출: ${formatWon(supply)}`,
  ].join("\n");
}


/** Reuse sales period resolver for date-scoped ERP lists. */
export const resolveErpPeriod = resolveSalesPeriod;

/** Split query into optional YYYYMMDD:YYYYMMDD period + keyword filter. */
export function splitErpQuery(query = "") {
  const q = String(query || "").trim();
  if (!q) return { periodQuery: "", filter: "" };
  if (q.includes(":") || q.includes("~")) {
    const m = q.match(/^(\d{8}\s*[:~]\s*\d{8})(?:\s+(.+))?$/);
    if (m) return { periodQuery: m[1].replace(/\s/g, ""), filter: (m[2] || "").trim() };
    return { periodQuery: q, filter: "" };
  }
  return { periodQuery: "", filter: q };
}

/** `lines:` / `라인:` → raw 전표라인; default = 품목 집계. */
export function parseErpView(query = "") {
  const raw = String(query || "").trim();
  const m = raw.match(/^(?:lines|라인|전표)\s*[:=]?\s*(.*)$/i);
  if (m) return { mode: "lines", query: (m[1] || "").trim() };
  return { mode: "items", query: raw };
}

export function capLimit(limit) {
  const n = Number(limit);
  if (!Number.isFinite(n) || n <= 0) return 20;
  return Math.min(Math.floor(n), 50);
}

/** Korean category → itemCd prefix (price master has no itemgrpNm). */
export const ERP_CATEGORY_PREFIX = {
  모듈: "M-",
  module: "M-",
  인버터: "I-",
  inverter: "I-",
  기타: "C-",
  부자재: "C-",
  커넥터: "C-",
  케이블: "C-",
};

export function expandItemFilter(filter = "") {
  const f = String(filter || "").trim();
  if (!f) return { text: "", prefix: "", note: "" };
  const prefix = ERP_CATEGORY_PREFIX[f] || ERP_CATEGORY_PREFIX[f.toLowerCase()] || "";
  if (prefix) return { text: f, prefix, note: `${f}→itemCd ${prefix}*` };
  return { text: f, prefix: "", note: "" };
}

function rowsOf(json) {
  const rd = json?.resultData;
  if (Array.isArray(rd)) return rd;
  if (Array.isArray(rd?.data)) return rd.data;
  if (Array.isArray(rd?.list)) return rd.list;
  return [];
}

function assertOk(r, label) {
  const code = r.json?.resultCode;
  if (code !== 0 && code !== 200) {
    throw new Error(r.json?.resultMsg || `${label} 실패 code=${code}`);
  }
}

export function matchQuery(row, query, fields) {
  const q = String(query || "").trim().toLowerCase();
  if (!q) return true;
  return fields.some((f) => String(row?.[f] ?? "").toLowerCase().includes(q));
}

/** Substring on fields, or itemCd prefix for category synonyms (모듈→M-). */
export function matchItemFilter(row, filter, fields) {
  const { text, prefix } = expandItemFilter(filter);
  if (!text && !prefix) return true;
  if (prefix && String(row?.itemCd || "").toUpperCase().startsWith(prefix.toUpperCase())) return true;
  if (prefix) {
    // Category synonym: also allow itemgrpNm / text hit (stock has 모듈 group)
    return matchQuery(row, text, fields);
  }
  return matchQuery(row, text, fields);
}

function fmtQty(n) {
  const v = Number(n);
  if (!Number.isFinite(v)) return "0";
  return v.toLocaleString("ko-KR");
}

function emptyPeriodBody() {
  return {
    divCds: [],
    deptCds: [],
    empCds: [],
    itemCds: [],
    itemCdExcludes: [],
    itemgrpCds: [],
    lCds: [],
    mCds: [],
    sCds: [],
  };
}

function emptyHint(kind, folder) {
  const tips = [];
  if (kind === "po" && folder === "month") tips.push("당월 발주가 비면 folder=ytd 로 재시도");
  if (kind === "price") tips.push("카테고리: 모듈→M- · 인버터→I- · 또는 itemCd 부분코드(M-LR)");
  tips.push("품목집계가 기본 · 전표라인은 query에 lines: 접두");
  tips.push("기간 folder=ytd|month|today|year|last_year 또는 query=YYYYMMDD:YYYYMMDD");
  return `힌트: ${tips.join(" · ")}`;
}

function renderHeader(title, metaLines, bodyTitle) {
  return [title, ...metaLines, "", bodyTitle];
}

function traderLabel(trCount, trSample) {
  if (trCount > 1) return ` · 거래처 ${trCount}곳${trSample ? `(${trSample})` : ""}`;
  if (trSample) return ` · ${trSample}`;
  return "";
}

/** Collapse warehouse/lot rows into one line per itemCd (sum qty). */
export function aggregateStockByItem(rows) {
  const map = new Map();
  for (const x of rows || []) {
    const key = String(x.itemCd || "").trim() || String(x.itemNm || "").trim();
    if (!key) continue;
    let a = map.get(key);
    if (!a) {
      a = {
        itemCd: x.itemCd || "",
        itemNm: x.itemNm || "",
        itemDc: x.itemDc || "",
        itemgrpNm: x.itemgrpNm || "",
        jegoQt: 0,
        gayongQt: 0,
        _wh: new Set(),
      };
      map.set(key, a);
    }
    a.jegoQt += Number(x.jegoQt) || 0;
    a.gayongQt += Number(x.gayongQt) || 0;
    if (x.whNm) a._wh.add(String(x.whNm));
    if (!a.itemNm && x.itemNm) a.itemNm = x.itemNm;
    if (!a.itemgrpNm && x.itemgrpNm) a.itemgrpNm = x.itemgrpNm;
  }
  return [...map.values()]
    .map((a) => ({
      ...a,
      whCount: a._wh.size,
      whSample: [...a._wh].slice(0, 3).join(","),
    }))
    .sort((a, b) => b.jegoQt - a.jegoQt);
}

/** Collapse line rows by itemCd, summing qty + amount. */
export function aggregateByItem(rows, { qtyField, amtField }) {
  const map = new Map();
  for (const x of rows || []) {
    const key = String(x.itemCd || "").trim() || String(x.itemNm || x.attrNm || "").trim();
    if (!key) continue;
    let a = map.get(key);
    if (!a) {
      a = {
        itemCd: x.itemCd || "",
        itemNm: x.itemNm || x.attrNm || "",
        itemgrpNm: x.itemgrpNm || "",
        qty: 0,
        amt: 0,
        lines: 0,
        lastDt: "",
        _tr: new Set(),
      };
      map.set(key, a);
    }
    a.qty += Number(x?.[qtyField]) || 0;
    a.amt += Number(x?.[amtField]) || 0;
    a.lines += 1;
    if (!a.itemNm && (x.itemNm || x.attrNm)) a.itemNm = x.itemNm || x.attrNm;
    if (x.trNm) a._tr.add(String(x.trNm));
    const dt = String(x.poDt || x.rcvDt || x.isuDt || x.clsDt || "");
    if (dt && dt > a.lastDt) a.lastDt = dt;
  }
  return [...map.values()]
    .map((a) => ({
      ...a,
      trCount: a._tr.size,
      trSample: [...a._tr].slice(0, 2).join(","),
    }))
    .sort((a, b) => b.amt - a.amt || b.qty - a.qty);
}

/** Top counterparties by supply amount. */
export function topTraders(rows, amtField, limit = 3) {
  const map = new Map();
  for (const x of rows || []) {
    const k = String(x.trNm || "").trim() || "(거래처없음)";
    map.set(k, (map.get(k) || 0) + (Number(x?.[amtField]) || 0));
  }
  return [...map.entries()]
    .sort((a, b) => b[1] - a[1])
    .slice(0, Math.max(1, limit))
    .map(([name, amt]) => ({ name, amt }));
}

function appendTraders(lines, raw, amtField) {
  const top = topTraders(raw, amtField, 3).filter((t) => t.amt > 0);
  if (!top.length) return;
  lines.push("", "거래처 상위:");
  top.forEach((t, i) => lines.push(`${i + 1}. ${t.name} · ${formatWon(t.amt)}`));
}

/** Unit-price fields on BSB0010 rows (구매/표준/기준). */
export function unitPrices(row) {
  const purch = Number(row?.purchUm);
  const std = Number(row?.stdUm);
  const sta = Number(row?.staUm);
  const any = [purch, std, sta].find((n) => Number.isFinite(n) && n !== 0) || 0;
  return {
    purch: Number.isFinite(purch) ? purch : 0,
    std: Number.isFinite(std) ? std : 0,
    sta: Number.isFinite(sta) ? sta : 0,
    any,
  };
}

function prepareErpQuery(query) {
  const view = parseErpView(query);
  const split = splitErpQuery(view.query);
  // `전체`/`all` opts out of the default 모듈·인버터 scope without text-matching.
  const all = /^(전체|all)$/i.test(split.filter);
  const filter = all ? "" : split.filter;
  const expanded = expandItemFilter(filter);
  return { mode: view.mode, periodQuery: split.periodQuery, filter, expanded, scopeDefault: !all && !filter };
}

/** Operator surface default: 모듈(M-)·인버터(I-)만. 검색어/`전체`는 전 품목. */
export function applyDefaultItemScope(rows, scoped) {
  if (!scoped) return rows;
  return (rows || []).filter((x) => /^(m-|i-)/i.test(String(x?.itemCd || "")));
}

function scopeNote(scoped, filtNote) {
  if (filtNote) return ` · 필터: ${filtNote}`;
  if (scoped) return " · 필터: 모듈·인버터(기본 — 전체는 query=전체)";
  return "";
}

/** 현재고현황 — POST /purchase/pom0010/0pu00000 (품목 집계, 창고 합산) */
export async function listStock(folder = "ytd", query = "", limit = 20) {
  const { filter, expanded, scopeDefault } = prepareErpQuery(query);
  const period = resolveErpPeriod(folder || "ytd", "");
  const yyyy = period.from.slice(0, 4);
  const body = {
    yyyy,
    divCds: [],
    whCds: [],
    lcCds: [],
    notBehalfFgs: [],
    itemCds: [],
    itemCdExcludes: [],
    itemgrpCds: [],
    gayongQt: "",
    lCds: [],
    mCds: [],
    sCds: [],
    qtFgs: [],
    acctFgs: [],
    odrFgs: [],
    badYns: [],
    avabstockYns: [],
    useYns: [],
    lotFgs: [],
    setitemFgs: [],
    menuCd: "POM0010",
    searchType: "coCd",
  };
  const r = await apiPost("/purchase/pom0010/0pu00000", body);
  assertOk(r, "현재고");
  const raw = applyDefaultItemScope(
    rowsOf(r.json).filter((x) =>
      matchItemFilter(x, filter, ["itemCd", "itemNm", "itemDc", "itemgrpNm", "whNm", "lcNm", "lNm", "mNm"]),
    ),
    scopeDefault,
  );
  const rows = aggregateStockByItem(raw).filter((x) => x.jegoQt !== 0 || x.gayongQt !== 0);
  const shown = rows.slice(0, capLimit(limit));
  const filtNote = expanded.note || filter;
  const lines = renderHeader(
    "현재고 요약 (구매/자재 · Amaranth · 품목 집계)",
    [
      `기준연도: ${yyyy}${scopeNote(scopeDefault, filtNote)}`,
      `원본행: ${raw.length.toLocaleString("ko-KR")} · 집계품목: ${rows.length.toLocaleString("ko-KR")} · 재고수량합: ${fmtQty(sumField(rows, "jegoQt"))}`,
    ],
    "재고 상위(창고 합산):",
  );
  if (!shown.length) {
    lines.push("(결과 없음)", emptyHint("stock", folder || "ytd"));
  } else {
    shown.forEach((x, i) => {
      const nm = x.itemNm || x.itemCd || "(품목)";
      const wh =
        x.whCount > 1
          ? ` · 창고 ${x.whCount}곳${x.whSample ? `(${x.whSample})` : ""}`
          : x.whSample
            ? ` · ${x.whSample}`
            : "";
      const grp = x.itemgrpNm || "";
      lines.push(
        `${i + 1}. ${nm} · 재고 ${fmtQty(x.jegoQt)} · 가용 ${fmtQty(x.gayongQt)}${grp ? ` · ${grp}` : ""}${wh} · ${x.itemCd || ""}`,
      );
    });
  }
  lines.push("", "출처: POST /purchase/pom0010/0pu00000 · searchType=coCd · 품목코드별 합산");
  return lines.join("\n");
}

function formatItemAggLine(i, x) {
  const nm = x.itemNm || x.itemCd || "(품목)";
  return `${i + 1}. ${nm} · ${formatWon(x.amt)} · 수량 ${fmtQty(x.qty)} · 라인 ${x.lines}${x.lastDt ? ` · ${ymdDash(x.lastDt)}` : ""}${traderLabel(x.trCount, x.trSample)} · ${x.itemCd || ""}`;
}

function formatRawLine(i, x, { nmFields, amtField, qtyField, dtField }) {
  const nm = nmFields.map((f) => x[f]).find(Boolean) || x.itemCd || "(품목)";
  const am = x[amtField] ?? 0;
  const qt = x[qtyField];
  const dt = ymdDash(String(x[dtField] || ""));
  const tr = x.trNm ? ` · ${x.trNm}` : "";
  return `${i + 1}. ${nm} · ${formatWon(am)} · 수량 ${fmtQty(qt)} · ${dt}${tr} · ${x.itemCd || ""}`;
}

/** 발주현황 — POST /purchase/poc0030/0pu00001 */
export async function listPurchaseOrders(folder = "ytd", query = "", limit = 20) {
  const { mode, periodQuery, filter, expanded, scopeDefault } = prepareErpQuery(query);
  const period = resolveErpPeriod(folder || "ytd", periodQuery);
  const body = {
    ...emptyPeriodBody(),
    poDtFr: period.from,
    poDtTo: period.to,
    poFgs: [],
    poNb: "",
    remarkDcFg: "",
    remarkDc: "",
    lotFgs: [],
    acctFgs: [],
    odrFgs: [],
    trCds: [],
    trCdExcludes: [],
    tradeGrps: [],
    exchCds: [],
    plnCdFg: "",
    plnCds: [],
    plnsCds: [],
    areaCds: [],
    areaGrps: [],
    mgmtCds: [],
    pjtCds: [],
    pjtgrpCds: [],
    setitemFgs: [],
    expireYns: [],
    dtFg: "0",
    dueDtFr: "",
    dueDtTo: "",
    useYns: [],
    mgmNm: "",
  };
  const r = await apiPost("/purchase/poc0030/0pu00001", body);
  assertOk(r, "발주현황");
  const raw = applyDefaultItemScope(
    rowsOf(r.json).filter((x) =>
      matchItemFilter(x, filter, ["itemCd", "itemNm", "trNm", "poNb", "plnNm", "deptNm", "itemgrpNm"]),
    ),
    scopeDefault,
  );
  const amtField = raw.some((x) => x.pohAm != null) ? "pohAm" : raw.some((x) => x.pogAm != null) ? "pogAm" : "exchAm";
  const rows = mode === "lines" ? null : aggregateByItem(raw, { qtyField: "poQt", amtField });
  const shownSrc = mode === "lines"
    ? [...raw].sort((a, b) => Number(b[amtField] || 0) - Number(a[amtField] || 0))
    : rows;
  const shown = shownSrc.slice(0, capLimit(limit));
  const filtNote = expanded.note || filter;
  const lines = renderHeader(
    `발주현황 요약 (구매/자재 · Amaranth · 공급가액 우선 · ${mode === "lines" ? "전표라인" : "품목 집계"})`,
    [
      `기간: ${period.label}${scopeNote(scopeDefault, filtNote)}`,
      `라인: ${raw.length.toLocaleString("ko-KR")}${mode === "items" ? ` · 집계품목: ${rows.length.toLocaleString("ko-KR")}` : ""} · 발주수량합: ${fmtQty(sumField(raw, "poQt"))} · 금액합: ${formatWon(sumField(raw, amtField))}`,
    ],
    mode === "lines" ? "금액 상위(라인):" : "금액 상위(품목 합산):",
  );
  if (!shown.length) {
    lines.push("(결과 없음)", emptyHint("po", folder || "ytd"));
  } else if (mode === "lines") {
    shown.forEach((x, i) =>
      lines.push(formatRawLine(i, x, { nmFields: ["itemNm"], amtField, qtyField: "poQt", dtField: "poDt" })),
    );
  } else {
    shown.forEach((x, i) => lines.push(formatItemAggLine(i, x)));
  }
  appendTraders(lines, raw, amtField);
  lines.push("", "출처: POST /purchase/poc0030/0pu00001 · 금액필드 " + amtField);
  return lines.join("\n");
}

/** 입고현황 — POST /purchase/pof0020/0pu00002 */
export async function listReceiving(folder = "month", query = "", limit = 20) {
  const { mode, periodQuery, filter, expanded, scopeDefault } = prepareErpQuery(query);
  const period = resolveErpPeriod(folder || "month", periodQuery);
  const body = {
    readType: "data",
    pagingDirection: "",
    rowCountPerPage: 5000,
    initialPageSize: 5000,
    markPreventRead: false,
    currentPage: 0,
    startRowIndex: 0,
    currentRowCount: 5000,
    ...emptyPeriodBody(),
    rcvDtFrom: period.from,
    rcvDtTo: period.to,
    rcvNb: "",
    remarkDcFg: "",
    remarkDc: "",
    poQtFg: "",
    negNumFg: "",
    lotFgs: [],
    acctFgs: [],
    odrFgs: [],
    trCds: [],
    trCdExcludes: [],
    tradeGrps: [],
    poFgs: [],
    mapFgs: [],
    exchCds: [],
    plnCdFg: "",
    plnCds: [],
    plnsCds: [],
    areaCds: [],
    areaGrps: [],
    mgmtCds: [],
    pjtCds: [],
    pjtgrpCds: [],
    vatFgs: [],
    whCds: [],
    lcCds: [],
  };
  const r = await apiPost("/purchase/pof0020/0pu00002", body);
  assertOk(r, "입고현황");
  const raw = applyDefaultItemScope(
    rowsOf(r.json).filter((x) =>
      matchItemFilter(x, filter, ["itemCd", "itemNm", "trNm", "rcvNb", "plnNm", "whNm", "itemgrpNm"]),
    ),
    scopeDefault,
  );
  const amtField = "rcvgAm";
  const rows = mode === "lines" ? null : aggregateByItem(raw, { qtyField: "rcvQt", amtField });
  const shownSrc = mode === "lines"
    ? [...raw].sort((a, b) => Number(b.rcvgAm || 0) - Number(a.rcvgAm || 0))
    : rows;
  const shown = shownSrc.slice(0, capLimit(limit));
  const filtNote = expanded.note || filter;
  const lines = renderHeader(
    `입고현황 요약 (구매/자재 · Amaranth · 공급가액 · ${mode === "lines" ? "전표라인" : "품목 집계"})`,
    [
      `기간: ${period.label}${scopeNote(scopeDefault, filtNote)}`,
      `라인: ${raw.length.toLocaleString("ko-KR")}${mode === "items" ? ` · 집계품목: ${rows.length.toLocaleString("ko-KR")}` : ""} · 입고수량합: ${fmtQty(sumField(raw, "rcvQt"))} · 공급가액합: ${formatWon(sumField(raw, amtField))}`,
    ],
    mode === "lines" ? "금액 상위(라인):" : "금액 상위(품목 합산):",
  );
  if (!shown.length) {
    lines.push("(결과 없음)", emptyHint("receive", folder || "month"));
  } else if (mode === "lines") {
    shown.forEach((x, i) =>
      lines.push(formatRawLine(i, x, { nmFields: ["itemNm"], amtField, qtyField: "rcvQt", dtField: "rcvDt" })),
    );
  } else {
    shown.forEach((x, i) => lines.push(formatItemAggLine(i, x)));
  }
  appendTraders(lines, raw, amtField);
  lines.push("", "출처: POST /purchase/pof0020/0pu00002 · rcvgAm");
  return lines.join("\n");
}

/** 출고현황 — POST /logis/blf0050/0lo00001 */
export async function listShipments(folder = "month", query = "", limit = 20) {
  const { mode, periodQuery, filter, expanded, scopeDefault } = prepareErpQuery(query);
  const period = resolveErpPeriod(folder || "month", periodQuery);
  const body = {
    readType: "data",
    pagingDirection: "",
    rowCountPerPage: 5000,
    initialPageSize: 5000,
    markPreventRead: false,
    currentPage: 0,
    startRowIndex: 0,
    currentRowCount: 5000,
    ...emptyPeriodBody(),
    isuDtFrom: period.from,
    isuDtTo: period.to,
    isuNbFg: "",
    isuNb: "",
    remarkDcFg: "",
    remarkDc: "",
    soQtFg: "",
    negNumYn: "",
    lotFgs: [],
    acctFgs: [],
    odrFgs: [],
    trCds: [],
    trCdExcludes: [],
    shipCds: [],
    tradeGrps: [],
    soFgs: [],
    mapFgs: [],
    exchCds: [],
    plnCdFg: "",
    plnCds: [],
    plnsCds: [],
    areaCds: [],
    areaGrps: [],
    mgmtCds: [],
    pjtCds: [],
    pjtgrpCds: [],
    vatFgs: [],
    whCds: [],
    lcCds: [],
    shipFgs: [],
  };
  const r = await apiPost("/logis/blf0050/0lo00001", body);
  assertOk(r, "출고현황");
  const raw = applyDefaultItemScope(
    rowsOf(r.json).filter((x) =>
      matchItemFilter(x, filter, ["itemCd", "itemNm", "trNm", "isuNb", "plnNm", "whNm", "itemgrpNm", "attrNm"]),
    ),
    scopeDefault,
  );
  const amtField = raw.some((x) => x.isugAm != null) ? "isugAm" : "exchAm";
  const rows = mode === "lines" ? null : aggregateByItem(raw, { qtyField: "isuQt", amtField });
  const shownSrc = mode === "lines"
    ? [...raw].sort((a, b) => Number(b[amtField] || 0) - Number(a[amtField] || 0))
    : rows;
  const shown = shownSrc.slice(0, capLimit(limit));
  const filtNote = expanded.note || filter;
  const lines = renderHeader(
    `출고현황 요약 (영업관리 · Amaranth · 공급가액 · ${mode === "lines" ? "전표라인" : "품목 집계"})`,
    [
      `기간: ${period.label}${scopeNote(scopeDefault, filtNote)}`,
      `라인: ${raw.length.toLocaleString("ko-KR")}${mode === "items" ? ` · 집계품목: ${rows.length.toLocaleString("ko-KR")}` : ""} · 출고수량합: ${fmtQty(sumField(raw, "isuQt"))} · 금액합: ${formatWon(sumField(raw, amtField))}`,
    ],
    mode === "lines" ? "금액 상위(라인):" : "금액 상위(품목 합산):",
  );
  if (!shown.length) {
    lines.push("(결과 없음)", emptyHint("ship", folder || "month"));
  } else if (mode === "lines") {
    shown.forEach((x, i) =>
      lines.push(
        formatRawLine(i, x, {
          nmFields: ["itemNm", "attrNm"],
          amtField,
          qtyField: "isuQt",
          dtField: "isuDt",
        }),
      ),
    );
  } else {
    shown.forEach((x, i) => lines.push(formatItemAggLine(i, x)));
  }
  appendTraders(lines, raw, amtField);
  lines.push("", "출처: POST /logis/blf0050/0lo00001 · 금액필드 " + amtField);
  return lines.join("\n");
}

/** 품목단가등록 — POST /logis/bsb0010/0lo00001 */
export async function listItemPrices(folder = "", query = "", limit = 20) {
  const { filter, expanded, scopeDefault } = prepareErpQuery(query);
  const body = {
    pagingDirection: "",
    rowCountPerPage: 2500,
    initialPageSize: 5000,
    markPreventRead: false,
    currentPage: 0,
    startRowIndex: 0,
    currentRowCount: 5000,
    itemCds: [],
    useYns: [],
    itemgrpCds: [],
    lCds: [],
    mCds: [],
    sCds: [],
    odrFgs: [],
    acctFgs: [],
    isTotalSelectItem: "0",
  };
  const r = await apiPost("/logis/bsb0010/0lo00001", body);
  assertOk(r, "품목단가");
  let rows = applyDefaultItemScope(
    rowsOf(r.json).filter((x) =>
      matchItemFilter(x, filter, ["itemCd", "itemNm", "itemDc", "attrNm", "itemgrpNm"]),
    ),
    scopeDefault,
  );
  rows = [...rows].sort((a, b) => unitPrices(b).any - unitPrices(a).any);
  if (!filter) {
    const priced = rows.filter((x) => unitPrices(x).any > 0);
    if (priced.length) rows = priced;
  }
  const shown = rows.slice(0, capLimit(limit));
  const filtNote = expanded.note || filter;
  const lines = renderHeader(
    "품목단가 요약 (물류공통 · Amaranth · 구매/표준/기준)",
    [
      filtNote ? `필터: ${filtNote}` : "필터: 모듈·인버터 단가 상위 (전체는 query=전체)",
      `매칭: ${rows.length.toLocaleString("ko-KR")}건`,
    ],
    "단가 목록:",
  );
  if (!shown.length) {
    lines.push("(결과 없음)", emptyHint("price", ""));
  } else {
    shown.forEach((x, i) => {
      const nm = x.itemNm || x.itemCd || "(품목)";
      const u = unitPrices(x);
      const bits = [];
      if (u.purch) bits.push(`구매 ${formatWon(u.purch)}`);
      if (u.std) bits.push(`표준 ${formatWon(u.std)}`);
      if (u.sta) bits.push(`기준 ${formatWon(u.sta)}`);
      const priceTxt = bits.length ? bits.join(" · ") : "(단가필드 없음/0)";
      const extra = [x.itemDc, x.attrNm].filter(Boolean).join(" · ");
      lines.push(
        `${i + 1}. ${nm} · ${priceTxt}${extra ? ` · ${extra}` : ""} · ${x.itemCd || ""}`,
      );
    });
  }
  lines.push("", "출처: POST /logis/bsb0010/0lo00001 · purchUm/stdUm/staUm · 모듈/인버터는 itemCd 접두사 매핑");
  return lines.join("\n");
}


/** Strip name prefix from korHcls ("오선택 전무" → "전무"). */
export function parseHonorific(korNm, korHcls) {
  const name = String(korNm || "").trim();
  const raw = String(korHcls || "").trim();
  if (!raw) return "";
  if (name && raw.startsWith(name)) {
    return raw.slice(name.length).trim();
  }
  const parts = raw.split(/\s+/);
  if (parts.length >= 2 && parts[0] === name) return parts.slice(1).join(" ");
  return raw;
}

/** YYYYMMDD → YYYY-MM-DD (empty if invalid). */
export function formatBirthDate(brthDt) {
  const s = String(brthDt || "").replace(/\D/g, "");
  if (s.length !== 8) return "";
  return `${s.slice(0, 4)}-${s.slice(4, 6)}-${s.slice(6, 8)}`;
}

function peopleSafeCard(detail, listRow = {}) {
  const korNm = detail.korNm || listRow.korNm || "";
  const title = parseHonorific(korNm, detail.korHcls);
  return {
    empCd: detail.empCd || listRow.empCd || "",
    name: korNm,
    dept: detail.deptNm || "",
    div: detail.divNm || "",
    title, // 직급/호칭 (korHcls에서 이름 제거)
    honorific: title,
    mobile: detail.emgcTel || "",
    officeTel: detail.tel || "",
    birth: formatBirthDate(detail.brthDt),
    status: detail.enrlFgNm || "",
  };
}

function formatPeopleLine(i, card) {
  const bits = [
    card.name || "(이름없음)",
    card.dept && `부서 ${card.dept}`,
    card.title && `직급/호칭 ${card.title}`,
    card.mobile && `휴대폰 ${card.mobile}`,
    card.birth && `생년월일 ${card.birth}`,
    card.officeTel && `회사전화 ${card.officeTel}`,
    card.status && card.status,
    card.empCd && card.empCd,
  ].filter(Boolean);
  return `${i + 1}. ${bits.join(" · ")}`;
}

/**
 * 사원 조회 — 명부(ApAperUserCode) + 상세(hph0120).
 * Returns name/dept/title/mobile/birth only (strips rsrgNo and other PII).
 */
export async function listPeople(folder = "", query = "", limit = 10) {
  const q = String(query || "").trim();
  if (!q) {
    return [
      "사원 조회 (인사 · Amaranth)",
      "query에 이름(또는 일부)이 필요합니다. 예: groupware(area=\"people\", query=\"김\")",
      "",
      "출처: POST /personal/APCodePicker/ApAperUserCode + /personal/hph0120/0hp00001",
    ].join("\n");
  }
  const listRes = await apiPost("/personal/APCodePicker/ApAperUserCode", {
    helpTy: "APER_USER_INFO",
    searchText: q,
  });
  assertOk(listRes, "사원명부");
  const candidates = rowsOf(listRes.json).filter((x) => x?.empCd && x?.usrYn !== "N");
  const cap = Math.min(capLimit(limit), 20); // detail is N+1; keep small
  const slice = candidates.slice(0, cap);
  const cards = [];
  for (const row of slice) {
    const detailRes = await apiPost("/personal/hph0120/0hp00001", { empCd: row.empCd });
    assertOk(detailRes, "사원상세");
    const details = rowsOf(detailRes.json);
    // Prefer exact empCd match when API returns multiple
    const detail = details.find((d) => d.empCd === row.empCd) || details[0] || {};
    cards.push(peopleSafeCard(detail, row));
  }
  const lines = [
    "사원 조회 (인사 · Amaranth · 이름/부서/직급·호칭/휴대폰/생년월일)",
    `검색: ${q} · 명부매칭 ${candidates.length.toLocaleString("ko-KR")}명 · 상세 ${cards.length}명`,
    "",
    "결과:",
  ];
  if (!cards.length) {
    lines.push("(결과 없음 — 이름 철자를 확인하거나 성만 입력해 보세요)");
  } else {
    cards.forEach((c, i) => lines.push(formatPeopleLine(i, c)));
  }
  if (candidates.length > cards.length) {
    lines.push("", `… 외 ${candidates.length - cards.length}명은 limit으로 생략 (limit 최대 20)`);
  }
  lines.push(
    "",
    "출처: POST /personal/APCodePicker/ApAperUserCode · /personal/hph0120/0hp00001",
    "제외: 주민번호(rsrgNo)·주소 등 민감필드. 휴대폰=emgcTel, 직급/호칭=korHcls.",
  );
  // Machine-readable cards for Go wiki/org linkage (stripped before agent sees text).
  lines.push("DENEB_PEOPLE_JSON:" + JSON.stringify(cards));
  return lines.join("\n");
}
