import { describe, expect, it } from "vitest";
import { render } from "@testing-library/react";
import { splitDenebUi } from "@/markdown/denebUiParse";
import { buildSrcdoc, DenebHtmlAnswer, parseDenebHtmlMessage } from "./DenebHtml";

describe("deneb-html splitting", () => {
  it("splits a closed deneb-html fence into an html segment between prose", () => {
    const segs = splitDenebUi("앞 설명\n```deneb-html\n<!doctype html>\n<div>페이지</div>\n```\n뒤 설명");
    expect(segs.map((s) => s.kind)).toEqual(["md", "html", "md"]);
    expect(segs[1]).toMatchObject({ body: "<!doctype html>\n<div>페이지</div>" });
  });

  it("flags an unclosed deneb-html fence as pending (streaming)", () => {
    const segs = splitDenebUi("```deneb-html\n<div>아직");
    expect(segs.at(-1)?.kind).toBe("html-pending");
  });

  it("does not confuse deneb-html with deneb-ui", () => {
    const segs = splitDenebUi("```deneb-ui\n<column><text>카드</text></column>\n```");
    expect(segs.map((s) => s.kind)).toEqual(["ui"]);
  });
});

describe("deneb-html sandbox document", () => {
  it("prepends a network-blocking CSP and the deneb.send bridge", () => {
    const doc = buildSrcdoc("<div>본문</div>");
    expect(doc).toContain("Content-Security-Policy");
    expect(doc).toContain("default-src 'none'");
    expect(doc).toContain("window.deneb={send:");
    expect(doc.endsWith("<div>본문</div>")).toBe(true);
  });

  it("classifies bridge messages and rejects foreign data", () => {
    expect(parseDenebHtmlMessage({ __deneb: "prompt", text: " 확인 " })).toEqual({ type: "prompt", text: "확인" });
    expect(parseDenebHtmlMessage({ __deneb: "height", h: 320 })).toEqual({ type: "height", h: 320 });
    expect(parseDenebHtmlMessage({ __deneb: "prompt", text: "" })).toBeNull();
    expect(parseDenebHtmlMessage({ random: true })).toBeNull();
    expect(parseDenebHtmlMessage("문자열")).toBeNull();
  });

  it("renders a sandboxed iframe without same-origin power", () => {
    const { container } = render(<DenebHtmlAnswer body="<div>페이지</div>" onSubmit={() => {}} />);
    const frame = container.querySelector("iframe.deneb-html-frame");
    expect(frame).not.toBeNull();
    expect(frame!.getAttribute("sandbox")).toBe("allow-scripts");
    expect(frame!.getAttribute("srcdoc")).toContain("<div>페이지</div>");
  });
});
