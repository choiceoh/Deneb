import { describe, expect, it } from "vitest";
import { parseApprovalDocBody } from "./approvalBody";

const SAMPLE = `[그룹웨어 전자결재 · 전체결재문서]
조회: 99178

제목: 다과비 품의
문서번호: DOC-1
기안: 김승리

결재선
  1. 김승리 · 차장 · 승인
  2. 차남두 · 부장 · 대기

본문
| 항목 | 금액 |
| --- | --- |
| 다과 | 10,000 |

첨부 (2건 · 내용 미열람)
필요한 파일만 groupware action=attachment …
1. 영수증.pdf · 12KB
2. 견적.xlsx
`;

describe("parseApprovalDocBody", () => {
  it("splits header fields, line, body, and attachments", () => {
    const s = parseApprovalDocBody(SAMPLE);
    expect(s.title).toBe("다과비 품의");
    expect(s.drafter).toBe("김승리");
    expect(s.lineCount).toBe(2);
    expect(s.line).toContain("김승리");
    expect(s.body).toContain("| 항목 | 금액 |");
    expect(s.attachmentCount).toBe(2);
    expect(s.attachmentHeader).toMatch(/^첨부/);
  });

  it("falls back to full text as body when markers are absent", () => {
    const s = parseApprovalDocBody("그냥 본문만");
    expect(s.body).toBe("그냥 본문만");
    expect(s.line).toBe("");
    expect(s.attachments).toBe("");
  });
});
