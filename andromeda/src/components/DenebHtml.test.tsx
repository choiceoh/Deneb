import { describe, expect, it } from "vitest";
import { render } from "@testing-library/react";
import { splitDenebUi } from "@/markdown/denebUiParse";
import { DenebHtmlAnswer } from "./DenebHtml";
import {
  DENEB_HTML_MAX_HEIGHT,
  DENEB_HTML_MIN_HEIGHT,
  buildSrcdoc,
  denebHtmlFrameHeight,
  parseDenebHtmlMessage,
} from "./denebHtmlSandbox";

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

  it("injects the base stylesheet before the page body so page styles win", () => {
    const doc = buildSrcdoc("<style>body{margin:0}</style><div>본문</div>");
    expect(doc).toContain("color-scheme:light");
    expect(doc).toContain("Noto Sans KR");
    expect(doc.indexOf("Noto Sans KR")).toBeLessThan(doc.indexOf("body{margin:0}"));
  });

  it("ships the theme variants and design-system utilities the contract teaches", () => {
    const doc = buildSrcdoc("<div/>");
    for (const marker of [
      "body.theme-dark",
      "body.theme-warm",
      "body.theme-mono",
      ".card{",
      ".grid{",
      ".stat-value{",
      ".badge.warn{",
      ".bar>i{",
      "button.primary{",
    ]) {
      expect(doc).toContain(marker);
    }
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

// The failure this guards is silent: a frame that stops growing shows the top of
// the card and nothing tells the reader the rest exists. The mirror failure is a
// frame that grows on its own echo forever. Both are here. Twin:
// client-android .../dynamicui/DenebHtmlFrameTest.kt.
describe("deneb-html frame height", () => {
  it("gives a tall card a frame that fits it", () => {
    expect(denebHtmlFrameHeight(DENEB_HTML_MIN_HEIGHT, 2800)).toBeGreaterThanOrEqual(2800);
  });

  it("does not grow on the echo once the frame fits", () => {
    const fitted = denebHtmlFrameHeight(DENEB_HTML_MIN_HEIGHT, 2800);
    expect(denebHtmlFrameHeight(fitted, fitted)).toBe(fitted);
    expect(denebHtmlFrameHeight(fitted, fitted - 1)).toBe(fitted);
  });

  it("settles on a viewport-sized page instead of climbing", () => {
    let h = DENEB_HTML_MIN_HEIGHT;
    for (let i = 0; i < 50; i++) h = denebHtmlFrameHeight(h, h);
    expect(h).toBe(DENEB_HTML_MIN_HEIGHT);
  });

  it("stops at the backstop when the page multiplies the viewport", () => {
    let h = DENEB_HTML_MIN_HEIGHT;
    for (let i = 0; i < 100; i++) h = denebHtmlFrameHeight(h, h * 2);
    expect(h).toBe(DENEB_HTML_MAX_HEIGHT);
  });

  it("keeps growing when content appears later, and ignores nonsense", () => {
    const first = denebHtmlFrameHeight(DENEB_HTML_MIN_HEIGHT, 600);
    const second = denebHtmlFrameHeight(first, 1400);
    expect(second).toBeGreaterThanOrEqual(1400);
    expect(denebHtmlFrameHeight(second, -5000)).toBe(second);
    expect(denebHtmlFrameHeight(second, Number.POSITIVE_INFINITY)).toBe(second);
    expect(denebHtmlFrameHeight(second, 1e9)).toBe(DENEB_HTML_MAX_HEIGHT);
  });
});
