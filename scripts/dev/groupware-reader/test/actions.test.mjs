import assert from "node:assert/strict";
import test from "node:test";
import { htmlToText, normalizeFolder, selectApprovalLine } from "../lib/actions.mjs";

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

test("htmlToText strips tags and decodes entities", () => {
  const out = htmlToText("<p>금액&nbsp;(&#8361; 105,440)</p><br><script>x</script>");
  assert.ok(out.includes("금액"));
  assert.ok(out.includes("105,440"));
  assert.ok(!out.includes("<script>"));
});
