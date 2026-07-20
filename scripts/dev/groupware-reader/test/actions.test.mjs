import assert from "node:assert/strict";
import test from "node:test";
import {
  cleanOcr,
  dedupeOcrText,
  htmlTableMatrix,
  htmlTableToMarkdown,
  htmlToText,
  listApproval,
  listApprovalEntries,
  readApproval,
  listBoard,
  listBoardEntries,
  normalizeBoardPost,
  normalizeApprovalDoc,
  normalizeFolder,
  selectApprovalLine,
  attachmentName,
  humanSize,
  selectAttachment,
  resolveSalesPeriod,
  resolveErpPeriod,
  formatWon,
  applyDefaultItemScope,
  splitErpQuery,
  parseErpView,
  capLimit,
  aggregateStockByItem,
  aggregateByItem,
  topTraders,
  unitPrices,
  matchQuery,
  matchItemFilter,
  expandItemFilter,
  parseHonorific,
  formatBirthDate,
} from "../lib/actions.mjs";

// Real eap126A05 payload keys the user as `user_id` (string), NOT `emp_seq`.
// The pending line is app_sts "20"; approved "30"; downstream "70". These fixtures
// mirror doc 99178 so the field-name regression can't silently return null again.
const lines99178 = [
  { user_name: "김승리", user_id: "1001", act_id: 3000, app_sts: "30", doc_line_m_seq: 1, doc_line_s_seq: 1 },
  { user_name: "차남두", user_id: "1002", act_id: 3000, app_sts: "30", doc_line_m_seq: 2, doc_line_s_seq: 1 },
  { user_name: "오선택", user_id: "2226", act_id: 3000, app_sts: "20", doc_line_m_seq: 3, doc_line_s_seq: 1 },
  { user_name: "유병진", user_id: "1004", act_id: 3000, app_sts: "70", doc_line_m_seq: 4, doc_line_s_seq: 1 },
];

test("selectApprovalLine picks the caller's pending (20) line by user_id", () => {
  const line = selectApprovalLine(lines99178, "2226");
  assert.ok(line, "expected a match");
  assert.equal(line.user_name, "오선택");
  assert.equal(line.doc_line_m_seq, 3);
});

test("selectApprovalLine returns null when the caller's line is already approved", () => {
  assert.equal(selectApprovalLine(lines99178, "1001"), null); // app_sts 30
});

test("selectApprovalLine returns null when the caller isn't on the line at all", () => {
  assert.equal(selectApprovalLine(lines99178, "9999"), null);
});

test("selectApprovalLine returns null for a downstream (70) approver not yet reached", () => {
  assert.equal(selectApprovalLine(lines99178, "1004"), null); // app_sts 70
});

test("selectApprovalLine tolerates alt field names (emp_seq / appSts)", () => {
  const alt = [{ user_name: "x", emp_seq: "42", appSts: "20", doc_line_m_seq: 1, doc_line_s_seq: 1 }];
  assert.equal(selectApprovalLine(alt, "42")?.user_name, "x");
});

test("normalizeFolder maps Korean + aliases", () => {
  assert.equal(normalizeFolder("미결"), "pending");
  assert.equal(normalizeFolder("기결문서"), "done");
  assert.equal(normalizeFolder("수신참조"), "cc");
  assert.equal(normalizeFolder("전체결재문서"), "total");
  assert.equal(normalizeFolder(""), "all");
});

test("normalizeApprovalDoc handles both approval list payload shapes", () => {
  assert.deepEqual(
    normalizeApprovalDoc(
      {
        DOC_ID: 99178,
        DOC_TITLE_ORIGIN: "구매 품의",
        DOC_NO: "EAP-42",
        userNm: "홍길동",
        REP_DT: "2026-07-16",
        RET_ITEM_NM: "결재대기",
      },
      "미결",
    ),
    {
      docId: "99178",
      title: "구매 품의",
      docNo: "EAP-42",
      drafter: "홍길동",
      date: "2026-07-16",
      status: "결재대기",
      folder: "pending",
    },
  );
  assert.deepEqual(normalizeApprovalDoc({ doc_id: "7", doc_title: "휴가", user_name: "김" }, "done"), {
    docId: "7",
    title: "휴가",
    docNo: "",
    drafter: "김",
    date: "",
    status: "",
    folder: "done",
  });
});

test("listApprovalEntries normalizes without network and preserves human list output", async () => {
  const calls = [];
  const loaders = {
    async listBoxPortlet(folder, limit) {
      calls.push([folder, limit]);
      return [{ DOC_ID: "12", DOC_TITLE_ORIGIN: "지출결의", userNm: "이대리" }];
    },
    async listTotal() {
      throw new Error("unexpected total loader");
    },
  };
  const entries = await listApprovalEntries("pending", 80, loaders);
  assert.deepEqual(calls, [["pending", 50]]);
  assert.equal(entries[0].docId, "12");
  assert.equal(entries[0].folder, "pending");

  const human = await listApproval("pending", 80, loaders);
  assert.equal(human, "전자결재 · 미결문서 (1건)\n\n1. 지출결의 · 기안 이대리 · id=12");
});

test("readApproval searches the full list window used by the radar", async () => {
  const calls = [];
  const docs = Array.from({ length: 50 }, (_, index) => ({
    DOC_ID: String(100_000 - index),
    DOC_TITLE_ORIGIN: `수신참조 ${index + 1}`,
  }));
  docs[49] = { DOC_ID: "93481", DOC_TITLE_ORIGIN: "가장 오래된 수신참조" };

  const out = await readApproval("cc", "93481", "", {
    async listBoxPortlet(folder, limit) {
      calls.push([folder, limit]);
      return docs;
    },
    async listTotal() {
      throw new Error("unexpected total loader");
    },
    async fetchDocDetail(docId) {
      assert.equal(String(docId), "93481");
      return { doc_id: docId, doc_title: "가장 오래된 수신참조", doc_contents: "<p>본문</p>" };
    },
    async fetchDocLine() {
      return [];
    },
    async formatAttachments() {
      return "";
    },
  });

  assert.deepEqual(calls, [["cc", 50]]);
  assert.match(out, /가장 오래된 수신참조/);
  assert.match(out, /본문/);
});

test("normalizeBoardPost handles board payload aliases", () => {
  assert.deepEqual(
    normalizeBoardPost({
      art_seq_no: 17106,
      art_title: "휴무 일정",
      mbr_nick: "인사팀",
      write_date: "2026-07-16",
      cat_seq_no: 42,
    }),
    {
      postId: "17106",
      title: "휴무 일정",
      author: "인사팀",
      date: "2026-07-16",
      categoryId: "42",
    },
  );
  assert.equal(normalizeBoardPost({ postId: "7", categoryId: "9" }).postId, "7");
});

test("listBoardEntries normalizes without network and preserves human list output", async () => {
  const calls = [];
  const loaders = {
    async fetchBoardEntries(limit) {
      calls.push(limit);
      return [
        {
          art_seq_no: "17106",
          art_title: "휴무 일정",
          mbr_nick: "인사팀",
          write_date: "2026-07-16",
          cat_seq_no: "42",
        },
      ];
    },
  };
  const entries = await listBoardEntries(80, loaders);
  assert.deepEqual(calls, [50]);
  assert.deepEqual(entries[0], {
    postId: "17106",
    title: "휴무 일정",
    author: "인사팀",
    date: "2026-07-16",
    categoryId: "42",
  });

  const human = await listBoard(80, loaders);
  assert.equal(human, "게시판 최근 글 (1건)\n\n1. 휴무 일정 · 작성 인사팀 · 일자 2026-07-16 · id=17106");
});

test("htmlToText strips tags and decodes entities", () => {
  const out = htmlToText("<p>금액&nbsp;(&#8361; 105,440)</p><br><script>x</script>");
  assert.ok(out.includes("금액"));
  assert.ok(out.includes("105,440"));
  assert.ok(!out.includes("<script>"));
});

test("dedupeOcrText caps runaway repeated lines from OCR loops", () => {
  const looped = Array(40).fill("부가세 권세 물품가액: 39,500").join("\n");
  const out = dedupeOcrText(looped);
  const n = out.split("\n").filter((l) => l.includes("39,500")).length;
  assert.ok(n <= 3, `expected <=3 repeats, got ${n}`);
});

test("cleanOcr keeps real Korean receipt text", () => {
  const receipt = "영광축협 하나로마트\n총 구 매 액: 65,940\n부가세: 5,993";
  assert.equal(cleanOcr(receipt), receipt);
});

test("cleanOcr suppresses symbol-soup OCR of a photo collage", () => {
  const soup = "| = =\n0 a | ey\nar | 「 < Nia aes\n『 칙 NY cy ^ 져 ^ i) ESS\n~ ~ 00 세";
  assert.equal(cleanOcr(soup), "");
});


test("htmlTableToMarkdown preserves rows and columns", () => {
  const html = `<table>
    <tr><th>발전소명</th><th>수량</th><th>합계</th></tr>
    <tr><td>석문호</td><td>70EA</td><td>231,000,000원</td></tr>
    <tr><td>두온에너지</td><td>1EA</td><td>3,740,000원</td></tr>
  </table>`;
  assert.equal(
    htmlTableToMarkdown(html),
    "| 발전소명 | 수량 | 합계 |\n| --- | --- | --- |\n| 석문호 | 70EA | 231,000,000원 |\n| 두온에너지 | 1EA | 3,740,000원 |",
  );
});

test("htmlTableMatrix expands colspan and rowspan to rectangular blanks", () => {
  const html = `<table>
    <tr><td rowspan="2">구분</td><td>A</td><td>B</td></tr>
    <tr><td colspan="2">합계</td></tr>
  </table>`;
  assert.deepEqual(htmlTableMatrix(html), [["구분", "A", "B"], ["", "합계", ""]]);
});

test("one-row layout table remains a sentence, not a fake table", () => {
  const html = "<table><tr><td>금 액</td><td>一金</td><td>105,440</td><td>원整</td></tr></table>";
  assert.equal(htmlTableToMarkdown(html), "금 액 一金 105,440 원整");
});

test("htmlToText reinserts markdown table between surrounding prose", () => {
  const html = `<p>구매 내역</p><table><tr><td>품목</td><td>금액</td></tr><tr><td>인버터</td><td>100원</td></tr></table><p>이상.</p>`;
  const out = htmlToText(html);
  assert.ok(out.includes("구매 내역"));
  assert.ok(out.includes("| 품목 | 금액 |"));
  assert.ok(out.includes("| 인버터 | 100원 |"));
  assert.ok(out.endsWith("이상."));
});

test("table cells escape markdown pipes and keep line breaks", () => {
  const html = `<table><tr><td>A|B</td><td>설명</td></tr><tr><td>1</td><td>첫줄<br>둘째줄</td></tr></table>`;
  const out = htmlTableToMarkdown(html);
  assert.ok(out.includes("A\\|B"));
  assert.ok(out.includes("첫줄<br>둘째줄"));
});


test("selectAttachment resolves one-based number, exact title, and fileKey", () => {
  const files = [
    { dispFileNm: "영수증", fileExtsn: "jpg", fileKey: 101 },
    { dispFileNm: "사진대지", fileExtsn: "pdf", fileKey: 202 },
  ];
  assert.equal(selectAttachment(files, "2")?.fileKey, 202);
  assert.equal(selectAttachment(files, "영수증.jpg")?.fileKey, 101);
  assert.equal(selectAttachment(files, "202")?.fileKey, 202);
});

test("selectAttachment refuses ambiguous partial filenames", () => {
  const files = [
    { dispFileNm: "6월 거래명세서", fileExtsn: "pdf", fileKey: 1 },
    { dispFileNm: "7월 거래명세서", fileExtsn: "pdf", fileKey: 2 },
  ];
  assert.throws(() => selectAttachment(files, "거래명세서"), /모호/);
});


test("humanSize renders B/KB/MB and skips zero", () => {
  assert.equal(humanSize(512), "512B");
  assert.equal(humanSize(64968), "63KB");
  assert.equal(humanSize(2627887), "2.5MB");
  assert.equal(humanSize(0), "");
});

test("attachmentName strips Amaranth's leading ordinal so lists don't double-number", () => {
  assert.equal(attachmentName({ dispFileNm: "1. 지출영수증", fileExtsn: "jpg" }), "지출영수증.jpg");
  assert.equal(attachmentName({ dispFileNm: "3) 사진대지", fileExtsn: "pdf" }), "사진대지.pdf");
  assert.equal(attachmentName({ dispFileNm: "거래명세서.pdf" }), "거래명세서.pdf");
});

test("resolveSalesPeriod defaults to YTD", () => {
  const p = resolveSalesPeriod("", "", new Date("2026-07-16T01:00:00+09:00"));
  assert.equal(p.from, "20260101");
  assert.equal(p.to, "20260716");
  assert.match(p.label, /YTD|연초/);
});

test("resolveSalesPeriod month/today/last_year", () => {
  const now = new Date("2026-07-16T01:00:00+09:00");
  assert.deepEqual(
    { from: resolveSalesPeriod("month", "", now).from, to: resolveSalesPeriod("month", "", now).to },
    { from: "20260701", to: "20260716" },
  );
  assert.equal(resolveSalesPeriod("today", "", now).from, "20260716");
  assert.equal(resolveSalesPeriod("작년", "", now).from, "20250101");
  assert.equal(resolveSalesPeriod("작년", "", now).to, "20251231");
});

test("applyDefaultItemScope keeps 모듈·인버터 only when scoped", () => {
  const rows = [
    { itemCd: "M-LR0615-03" },
    { itemCd: "I-OP3000-01" },
    { itemCd: "C-CABLE-01" },
    { itemCd: "S-공사-00001" },
  ];
  assert.deepEqual(
    applyDefaultItemScope(rows, true).map((x) => x.itemCd),
    ["M-LR0615-03", "I-OP3000-01"],
  );
  assert.equal(applyDefaultItemScope(rows, false).length, 4);
});

test("resolveSalesPeriod yoy/h1/h2 comparison windows", () => {
  const now = new Date("2026-07-16T01:00:00+09:00");
  const yoy = resolveSalesPeriod("yoy", "", now);
  assert.equal(yoy.from, "20250101");
  assert.equal(yoy.to, "20250716");
  assert.match(yoy.label, /동기/);
  const h1 = resolveSalesPeriod("상반기", "", now);
  assert.deepEqual({ from: h1.from, to: h1.to }, { from: "20260101", to: "20260630" });
  const h2 = resolveSalesPeriod("h2", "", now);
  assert.deepEqual({ from: h2.from, to: h2.to }, { from: "20260701", to: "20261231" });
});

test("resolveSalesPeriod explicit range", () => {
  const p = resolveSalesPeriod("ytd", "20260301:20260331");
  assert.equal(p.from, "20260301");
  assert.equal(p.to, "20260331");
});

test("formatWon uses eok/man without raw-digit paren", () => {
  assert.equal(formatWon(294031347655), "2,940억 3,134만 7,655원");
  assert.equal(formatWon(12345), "1만 2,345원");
  assert.equal(formatWon(500), "500원");
  assert.ok(!formatWon(12345).includes("("));
});

test("resolveErpPeriod aliases resolveSalesPeriod", () => {
  const a = resolveSalesPeriod("month", "", new Date("2026-07-16T01:00:00+09:00"));
  const b = resolveErpPeriod("month", "", new Date("2026-07-16T01:00:00+09:00"));
  assert.deepEqual(a, b);
});

test("splitErpQuery separates range and keyword", () => {
  assert.deepEqual(splitErpQuery("20260301:20260331 모듈"), {
    periodQuery: "20260301:20260331",
    filter: "모듈",
  });
  assert.deepEqual(splitErpQuery("인버터"), { periodQuery: "", filter: "인버터" });
  assert.deepEqual(splitErpQuery(""), { periodQuery: "", filter: "" });
});

test("capLimit clamps 1..50", () => {
  assert.equal(capLimit(0), 20);
  assert.equal(capLimit(3), 3);
  assert.equal(capLimit(999), 50);
});

test("aggregateStockByItem sums warehouses", () => {
  const rows = aggregateStockByItem([
    { itemCd: "A", itemNm: "모듈", jegoQt: 10, gayongQt: 8, whNm: "본사" },
    { itemCd: "A", itemNm: "모듈", jegoQt: 5, gayongQt: 5, whNm: "부산" },
    { itemCd: "B", itemNm: "인버터", jegoQt: 0, gayongQt: 0, whNm: "본사" },
  ]);
  assert.equal(rows.length, 2);
  assert.equal(rows[0].itemCd, "A");
  assert.equal(rows[0].jegoQt, 15);
  assert.equal(rows[0].whCount, 2);
});

test("aggregateByItem sums amount and qty", () => {
  const rows = aggregateByItem(
    [
      { itemCd: "A", itemNm: "x", poQt: 2, pohAm: 100, poDt: "20260701", trNm: "갑" },
      { itemCd: "A", itemNm: "x", poQt: 3, pohAm: 150, poDt: "20260710", trNm: "을" },
    ],
    { qtyField: "poQt", amtField: "pohAm" },
  );
  assert.equal(rows[0].qty, 5);
  assert.equal(rows[0].amt, 250);
  assert.equal(rows[0].lines, 2);
  assert.equal(rows[0].lastDt, "20260710");
  assert.equal(rows[0].trCount, 2);
});

test("unitPrices prefers purch/std/sta", () => {
  assert.equal(unitPrices({ purchUm: 1000, stdUm: 0, staUm: 0 }).any, 1000);
  assert.equal(unitPrices({ stdUm: 200 }).std, 200);
});

test("matchQuery is case-insensitive substring", () => {
  assert.equal(matchQuery({ itemNm: "태양광모듈" }, "모듈", ["itemNm"]), true);
  assert.equal(matchQuery({ itemNm: "인버터" }, "모듈", ["itemNm"]), false);
});

test("expandItemFilter maps Korean categories to itemCd prefix", () => {
  assert.equal(expandItemFilter("모듈").prefix, "M-");
  assert.equal(expandItemFilter("인버터").prefix, "I-");
  assert.equal(expandItemFilter("M-LR").prefix, "");
});

test("matchItemFilter accepts 모듈 via M- prefix", () => {
  assert.equal(matchItemFilter({ itemCd: "M-LR0650-02", itemNm: "LR8" }, "모듈", ["itemNm"]), true);
  assert.equal(matchItemFilter({ itemCd: "I-OP3000-01", itemNm: "x" }, "모듈", ["itemNm"]), false);
  assert.equal(matchItemFilter({ itemCd: "I-OP3000-01", itemNm: "x" }, "인버터", ["itemNm"]), true);
});

test("parseErpView detects lines mode", () => {
  assert.deepEqual(parseErpView("lines:모듈"), { mode: "lines", query: "모듈" });
  assert.deepEqual(parseErpView("라인:20260701:20260710"), { mode: "lines", query: "20260701:20260710" });
  assert.deepEqual(parseErpView("모듈"), { mode: "items", query: "모듈" });
});

test("topTraders ranks by amount", () => {
  const top = topTraders(
    [
      { trNm: "갑", isugAm: 100 },
      { trNm: "을", isugAm: 300 },
      { trNm: "갑", isugAm: 50 },
    ],
    "isugAm",
    2,
  );
  assert.equal(top[0].name, "을");
  assert.equal(top[1].amt, 150);
});

test("parseHonorific strips name prefix", () => {
  assert.equal(parseHonorific("오선택", "오선택 전무"), "전무");
  assert.equal(parseHonorific("김영길", "김영길"), "");
  assert.equal(parseHonorific("", "전무"), "전무");
});

test("formatBirthDate formats YYYYMMDD", () => {
  assert.equal(formatBirthDate("19900115"), "1990-01-15");
  assert.equal(formatBirthDate(""), "");
  assert.equal(formatBirthDate("90"), "");
});
