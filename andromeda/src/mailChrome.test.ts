import { describe, expect, it } from "vitest";
import { stripMailChrome, visibleRuneCount } from "./mailChrome";

// Filler that clears the 200-byte SHORT_BODY_FLOOR so chrome stripping fires.
// Ported from the gateway's gmail/mail_chrome_test.go.
const chromeBody = "이번 주 출시 노트의 주요 변경 사항입니다. ".repeat(8);

describe("stripMailChrome — top banner (preamble)", () => {
  const cases: [string, string][] = [
    ["Having trouble viewing this email?", "trouble viewing"],
    ["Can't see this email? Click here to view in browser.", "Can't see"],
    ["Unable to view this message? View it online.", "Unable to view"],
    ["Email not rendering correctly?", "not rendering"],
    ["Click here to view this email in your browser.", "Click here"],
    ["온라인에서 보기", "온라인에서"],
    ["브라우저에서 보기", "브라우저에서"],
    ["이메일이 깨져 보이시나요?", "깨져 보이"],
    ["[광고]", "[광고]"],
    ["[AD]", "[AD]"],
  ];
  for (const [preamble, absent] of cases) {
    it(`strips "${absent}"`, () => {
      const got = stripMailChrome(`${preamble}\n\n${chromeBody}\n`);
      expect(got).not.toContain(absent);
      expect(got).toContain("이번 주 출시");
    });
  }
});

describe("stripMailChrome — footer / signature", () => {
  const cases: [string, string][] = [
    ["Please do not reply to this email.", "do not reply"],
    ["This is an automatically generated message.", "automatically generated"],
    ["본 메일은 자동으로 발송된 메일입니다.", "자동으로 발송"],
    ["발신 전용 메일입니다.", "발신 전용"],
    ["회신하지 마세요.", "회신하지 마"],
    ["사업자등록번호: 123-45-67890", "사업자등록번호"],
    ["통신판매업 신고번호: 2026-서울강남-1234", "통신판매업"],
    ["Privacy Policy | Contact us", "Privacy Policy"],
    ["개인정보 처리방침", "개인정보 처리방침"],
    ["You are receiving this email because you subscribed.", "receiving this email because"],
    ["Get Outlook for iOS", "Get Outlook"],
    ["-- \n홍길동\n팀장 / 마케팅", "홍길동"],
    ["--\n홍길동\n팀장 / 마케팅", "홍길동"],
  ];
  for (const [footer, absent] of cases) {
    it(`strips "${absent}"`, () => {
      const got = stripMailChrome(`${chromeBody}\n\n${footer}\n`);
      expect(got).not.toContain(absent);
      expect(got).toContain("이번 주 출시");
    });
  }
});

describe("stripMailChrome — quoted reply / forward", () => {
  const reply = "답장입니다. 검토 후 회신드리겠습니다. ".repeat(4);
  const cases: [string, string, string][] = [
    ["On Mon, Jan 1, 2026 at 1:23 PM, Alice <alice@example.com> wrote:", "> 이전 메일 내용입니다.", "이전 메일"],
    ["2026년 5월 27일 (화) 오후 1:23, Alice <alice@example.com>님이 작성:", "> 이전 메일 내용입니다.", "이전 메일"],
    ["----- Original Message -----", "From: alice@example.com\n\nOriginal body text", "Original body"],
    ["----- 원본 메시지 -----", "보낸 사람: alice@example.com\n\n원본 내용입니다.", "원본 내용"],
    ["보낸 사람: Alice <alice@example.com>", "받는 사람: Bob\n\n원본 내용입니다.", "원본 내용"],
    ["[원문 메시지]", "보낸 사람: alice\n\n원본 내용입니다.", "원본 내용"],
  ];
  for (const [marker, quoted, leak] of cases) {
    it(`cuts at "${marker.slice(0, 20)}…"`, () => {
      const got = stripMailChrome(`${reply}\n\n${marker}\n${quoted}`);
      expect(got).toContain("답장입니다");
      expect(got).not.toContain(marker);
      expect(got).not.toContain(leak);
    });
  }
});

describe("stripMailChrome — safety gates", () => {
  it("leaves short bodies untouched", () => {
    const input = "OTP: 123456 — Sent from my iPhone";
    expect(stripMailChrome(input)).toBe(input);
  });
  it("aborts an over-aggressive chrome cut", () => {
    const input = "View in browser. ".repeat(30) + "\nshort";
    expect(stripMailChrome(input)).toBe(input);
  });
  it("keeps a bare forward with no commentary above the marker", () => {
    const input =
      "----- 전달된 메시지 -----\n보낸 사람: alice@example.com\n\n" +
      "전달받은 본문 내용입니다. 중요한 정보가 들어 있습니다. ".repeat(6);
    expect(stripMailChrome(input)).toContain("전달받은 본문 내용");
  });
  it("skips the cut when the reply prefix is too short", () => {
    const input =
      "ㅇㅋ\n\n----- Original Message -----\n" + "긴 원본 본문이 여기에 길게 들어 있다고 가정합니다. ".repeat(8);
    expect(stripMailChrome(input)).toContain("긴 원본 본문");
  });
  it("does not treat mid-sentence '보낸 사람을' as an Outlook header", () => {
    const body = "이번 분기 보낸 사람을 추적하는 시스템 개선안에 대해 검토했습니다. ".repeat(5);
    expect(stripMailChrome(body)).toContain("추적하는 시스템");
  });
  it("strips a preamble that lands deep inside the head window", () => {
    const filler = "Acme Corporation Newsletter Header Line. ".repeat(14);
    const got = stripMailChrome(`${filler}\nView this email online\n\n${chromeBody}`);
    expect(got).not.toContain("View this email online");
    expect(got).toContain("이번 주 출시");
  });
});

describe("visibleRuneCount", () => {
  const cases: [string, number][] = [
    ["", 0],
    ["   \n\t\r", 0],
    ["abc", 3],
    ["  abc  ", 3],
    ["한글", 2],
    ["a b\nc", 3],
  ];
  for (const [input, want] of cases) {
    it(`counts ${JSON.stringify(input)} = ${want}`, () => {
      expect(visibleRuneCount(input)).toBe(want);
    });
  }
});
