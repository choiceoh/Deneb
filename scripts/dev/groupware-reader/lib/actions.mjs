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

/** Collapse Amaranth form HTML into readable Korean text. */
export function htmlToText(html) {
  let s = String(html || "");
  s = s.replace(/<script[\s\S]*?<\/script>/gi, "");
  s = s.replace(/<style[\s\S]*?<\/style>/gi, "");
  s = s.replace(/<\/(p|div|tr|li|h[1-6]|br|table)>/gi, "\n");
  s = s.replace(/<(br|hr)\s*\/?>/gi, "\n");
  s = s.replace(/<\/td>/gi, "\t");
  s = s.replace(/<[^>]+>/g, "");
  s = s
    .replace(/&nbsp;/g, " ")
    .replace(/&amp;/g, "&")
    .replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">")
    .replace(/&quot;/g, '"')
    .replace(/&#39;/g, "'");
  const lines = s
    .split(/\n+/)
    .map((l) => l.replace(/[ \t]+/g, " ").trim())
    .filter(Boolean);
  const merged = [];
  for (const line of lines) {
    if (
      merged.length &&
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

function formatApprovalMeta(d) {
  const title = d.DOC_TITLE_ORIGIN || d.doc_title || d.DOC_TITLE || "";
  const no = d.DOC_NO || d.doc_no || "";
  const user = d.userNm || d.user_name || d.DISP_TITLE_HEAD || "";
  const when = d.REP_DT || d.req_dt || d.draft_dt || d.rep_dt || "";
  const sts = d.RET_ITEM_NM || d.box_nm || "";
  const id = d.DOC_ID || d.doc_id || "";
  const bits = [
    title,
    no && `문서번호 ${no}`,
    user && `기안 ${user}`,
    when && `일자 ${when}`,
    sts && `상태 ${sts}`,
    id && `id=${id}`,
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

function extractTextFromFile(filePath, ext) {
  const e = String(ext || "").toLowerCase().replace(/^\./, "");
  if (e === "pdf") {
    const r = spawnSync("pdftotext", ["-layout", "-nopgbrk", filePath, "-"], {
      encoding: "utf8",
      maxBuffer: 2 * 1024 * 1024,
      timeout: 30_000,
    });
    if (r.status === 0) return String(r.stdout || "").trim().slice(0, MAX_EXTRACT_CHARS);
    return "";
  }
  if (["jpg", "jpeg", "png", "tif", "tiff", "bmp", "webp"].includes(e)) {
    const r = spawnSync(
      "tesseract",
      [filePath, "stdout", "-l", "kor+eng", "--psm", "6"],
      { encoding: "utf8", maxBuffer: 2 * 1024 * 1024, timeout: 60_000 },
    );
    if (r.status === 0) return String(r.stdout || "").trim().slice(0, MAX_EXTRACT_CHARS);
    return "";
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
  if (["jpg", "jpeg", "png", "tif", "tiff", "bmp", "webp"].includes(e)) {
    return process.env.DENEB_GROUPWARE_OCR === "1";
  }
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
    const extracted = extractTextFromFile(tmpFile, ext);
    return {
      name,
      ext,
      size: out.buffer.length,
      extracted,
      note: extracted ? "" : "텍스트 추출 없음(이미지·스캔일 수 있음)",
    };
  } finally {
    try {
      fs.rmSync(tmpDir, { recursive: true, force: true });
    } catch {
      /* ignore */
    }
  }
}

async function formatAttachments(docId) {
  const list = await fetchAttachList(docId);
  if (!list.length) return "";
  const lines = [`첨부 (${list.length}건)`];
  let downloaded = 0;
  for (const f of list) {
    const name = f.dispFileNm || f.fileNm || f.filePath || "첨부";
    const ext = String(f.fileExtsn || path.extname(String(f.filePath || "")) || "")
      .replace(/^\./, "")
      .toLowerCase();
    const size = f.fileSize != null ? `${f.fileSize}B` : "";
    lines.push(`- ${name}${ext ? `.${ext}` : ""}${size ? ` (${size})` : ""}`);
    // Metadata-only for skipped types; don't spend the download budget on a JPG
    // we won't OCR (image OCR is opt-in via DENEB_GROUPWARE_OCR=1).
    if (!wantExtract(ext)) {
      const img = ["jpg", "jpeg", "png", "tif", "tiff", "bmp", "webp"].includes(ext);
      lines.push(
        `  (${img ? "이미지 — OCR 생략 (DENEB_GROUPWARE_OCR=1 시 추출)" : "추출 미지원 형식"})`,
      );
      continue;
    }
    if (downloaded >= MAX_ATTACH_DOWNLOAD) {
      lines.push("  (다운로드 상한 도달 — 메타만)");
      continue;
    }
    downloaded += 1;
    try {
      const got = await downloadAndExtract(docId, f);
      if (got.extracted) {
        lines.push(`  추출:\n${got.extracted.split("\n").map((l) => `    ${l}`).join("\n")}`);
      } else if (got.note) {
        lines.push(`  (${got.note})`);
      }
    } catch (err) {
      lines.push(`  (다운로드 실패: ${String(err?.message || err).slice(0, 80)})`);
    }
  }
  return lines.join("\n");
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

export async function listApproval(folder, limit) {
  const lim = Math.min(Math.max(limit || 20, 1), 50);
  if (folder === "all") {
    const parts = [];
    for (const key of ["pending", "done", "cc", "total"]) {
      parts.push(await listApproval(key, Math.max(4, Math.ceil(lim / 4))));
    }
    return parts.join("\n\n");
  }
  const title = FOLDER_TITLE[folder] || folder;
  let docs;
  if (folder === "total") docs = await listTotal(lim);
  else docs = await listBoxPortlet(folder, lim);
  if (!docs.length) return `전자결재 · ${title}\n\n(문서 없음)`;
  const lines = docs.map((d, i) => `${i + 1}. ${formatApprovalMeta(d)}`);
  return `전자결재 · ${title} (${docs.length}건)\n\n${lines.join("\n")}`;
}

export async function readApproval(folder, query, matchText) {
  const q = queryFrom(query, matchText);
  if (!q) throw new Error("read requires --query or notification body on stdin");

  const folders = folder === "all" ? ["pending", "done", "cc", "total"] : [folder];
  for (const key of folders) {
    let docs;
    if (key === "total") docs = await listTotal(40);
    else docs = await listBoxPortlet(key, 40);
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
      fetchDocDetail(docId),
      fetchDocLine(docId),
      formatAttachments(docId).catch(() => ""),
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

export async function listBoard(limit) {
  const lim = Math.min(Math.max(limit || 20, 1), 50);
  const r = await apiPost("/board/APIHandler/getNewNoticeListForPortlet", {
    page: "1",
    pageSize: String(lim),
  });
  if (r.json?.resultCode !== 0 && r.json?.resultCode !== 200) {
    throw new Error(r.json?.resultMsg || `board list failed (${r.status})`);
  }
  const arts = r.json?.resultData?.articleList || [];
  if (!arts.length) return "게시판 최근 글\n\n(글 없음)";
  const lines = arts.map((a, i) => {
    const bits = [
      a.art_title,
      a.mbr_nick && `작성 ${a.mbr_nick}`,
      a.write_date && `일자 ${a.write_date}`,
      a.art_seq_no && `id=${a.art_seq_no}`,
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
    pageSize: "40",
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
