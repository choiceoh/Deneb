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

  it("reports the body's own height, not the viewport-clamped one", () => {
    // max(content, viewport) would mean a frame that overshot hears its own height
    // back and can never shrink — the blank-under-the-card bug. Twin assertion in
    // DenebHtmlPreludeContractTest.kt covers the native copy of this script.
    const doc = buildSrcdoc("<div/>");
    expect(doc).not.toContain("documentElement.scrollHeight");
    expect(doc).toContain("b.getBoundingClientRect().height");
    expect(doc).toContain("document.fonts.ready.then(r)");
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

// Both failures this guards are silent. A frame that stops growing shows the top
// of the card and nothing says the rest exists; a frame that cannot shrink keeps
// whatever inflated number it heard first and leaves screens of blank under the
// content. Twin: client-android .../dynamicui/DenebHtmlFrameTest.kt.
describe("deneb-html frame height", () => {
  it("gives a tall card a frame that fits it", () => {
    expect(denebHtmlFrameHeight(2800)).toBeGreaterThanOrEqual(2800);
  });

  it("shrinks when a later report is smaller", () => {
    // THE regression: a first measurement can land before fonts or final width do.
    const inflated = denebHtmlFrameHeight(3700);
    const settled = denebHtmlFrameHeight(2800);
    expect(settled).toBeLessThan(inflated);
    expect(settled).toBeGreaterThanOrEqual(2800);
  });

  it("keeps a short card at the minimum and bounds a runaway page", () => {
    expect(denebHtmlFrameHeight(40)).toBe(DENEB_HTML_MIN_HEIGHT);
    expect(denebHtmlFrameHeight(20000)).toBe(DENEB_HTML_MAX_HEIGHT);
  });

  it("ignores nonsense reports instead of collapsing the frame", () => {
    expect(denebHtmlFrameHeight(-5000)).toBe(DENEB_HTML_MIN_HEIGHT);
    expect(denebHtmlFrameHeight(Number.POSITIVE_INFINITY)).toBe(DENEB_HTML_MIN_HEIGHT);
    expect(denebHtmlFrameHeight(1e9)).toBe(DENEB_HTML_MAX_HEIGHT);
  });
});
