import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";

import { renderInline } from "./renderInline";
import { mailBody } from "./panes/mailBody";

describe("renderInline", () => {
  function renderText(text: string) {
    return render(<p>{renderInline(text, "case-")}</p>);
  }

  it("returns ordinary text without extra markup", () => {
    const { container } = renderText("plain text");
    expect(container.querySelector("p")).toHaveTextContent("plain text");
    expect(container.querySelector("strong,em,code,a")).toBeNull();
  });

  it("renders bold emphasis", () => {
    renderText("before **bold** after");
    expect(screen.getByText("bold").tagName).toBe("STRONG");
    expect(screen.getByText(/before/).parentElement).toHaveTextContent("before bold after");
  });

  it("renders italic emphasis", () => {
    renderText("*italic*");
    expect(screen.getByText("italic").tagName).toBe("EM");
  });

  it("renders inline code with the Deneb UI class", () => {
    renderText("run `make check`");
    expect(screen.getByText("make check")).toHaveClass("dui-inline-code");
    expect(screen.getByText("make check").tagName).toBe("CODE");
  });

  it("renders links in a separate safe tab", () => {
    renderText("[Deneb](https://example.com/docs)");
    const link = screen.getByRole("link", { name: "Deneb" });
    expect(link).toHaveAttribute("href", "https://example.com/docs");
    expect(link).toHaveAttribute("target", "_blank");
    expect(link).toHaveAttribute("rel", "noopener noreferrer");
  });

  it("preserves mixed markup and surrounding text order", () => {
    const { container } = renderText("A **B** C *D* E `F` G [H](https://h.test) I");
    expect(container.querySelector("p")?.textContent).toBe("A B C D E F G H I");
    expect(container.querySelectorAll("strong,em,code,a")).toHaveLength(4);
  });

  it("preserves unmatched markers literal", () => {
    const { container } = renderText("unfinished **bold and `code");
    expect(container.querySelector("p")?.textContent).toBe("unfinished **bold and `code");
    expect(container.querySelector("strong,code")).toBeNull();
  });

  it("renders repeated markup independently", () => {
    const { container } = renderText("**one** and **two** and `three` and `four`");
    expect(container.querySelectorAll("strong")).toHaveLength(2);
    expect(container.querySelectorAll("code")).toHaveLength(2);
  });
});

describe("mailBody", () => {
  it("returns empty for an absent mail", () => {
    expect(mailBody()).toBe("");
  });

  it.each([
    ["body", { body: "Body" }],
    ["plain", { plain: "Plain" }],
    ["plainText", { plainText: "Plain text" }],
    ["bodyText", { bodyText: "Body text" }],
    ["text", { text: "Text" }],
  ])("reads the %s alias", (_field, mail) => {
    expect(mailBody({ id: "m1", ...mail })).toBe(Object.values(mail)[0]);
  });

  it("when uses the first populated body alias", () => {
    expect(mailBody({ id: "m1", body: "Body", plain: "Plain", text: "Text" })).toBe("Body");
  });

  it("ignores blank aliases", () => {
    expect(mailBody({ id: "m1", body: "  ", plain: "Plain" })).toBe("Plain");
  });

  it("preserves a short plain body despite reply-like text", () => {
    const text = "현재 답변입니다.\n\n-- \n서명\n\nOn Tue, Someone wrote:\n> 이전 메일";
    expect(mailBody({ id: "m1", body: text })).toBe(text);
  });

  it("when uses snippet before HTML", () => {
    expect(mailBody({ id: "m1", snippet: "목록 요약", html: "<p>HTML 본문</p>" })).toBe("목록 요약");
  });

  it("when converts HTML to visible text", () => {
    expect(mailBody({ id: "m1", html: "<h1>제목</h1><p>본문 <strong>강조</strong></p>" })).toBe("제목본문 강조");
  });

  it("decodes HTML entities through DOMParser", () => {
    expect(mailBody({ id: "m1", html: "<p>A &amp; B &lt; C</p>" })).toBe("A & B < C");
  });

  it("falls back without DOMParser", () => {
    const parser = globalThis.DOMParser;
    vi.stubGlobal("DOMParser", undefined);
    try {
      expect(mailBody({ id: "m1", html: "<p>A</p>  <p>B</p>" })).toBe("A B");
    } finally {
      vi.stubGlobal("DOMParser", parser);
    }
  });

  it("returns empty when no displayable field exists", () => {
    expect(mailBody({ id: "m1", subject: "Only subject" })).toBe("");
  });
});
