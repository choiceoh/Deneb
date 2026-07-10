import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { printClosest, printElement } from "./print";

let printSpy: ReturnType<typeof vi.fn>;

beforeEach(() => {
  printSpy = vi.fn();
  window.print = printSpy as unknown as typeof window.print;
});

afterEach(() => {
  document.body.className = "";
  document.body.innerHTML = "";
});

describe("printElement", () => {
  it("marks the region + ancestor chain and calls window.print", () => {
    document.body.innerHTML = `<div class="root"><section class="outer"><article class="target"><p>hi</p></article><article class="sibling"></article></section></div>`;
    const target = document.querySelector<HTMLElement>(".target")!;

    printElement(target);

    expect(printSpy).toHaveBeenCalledTimes(1);
    expect(document.body.classList.contains("deneb-printing")).toBe(true);
    expect(target.classList.contains("deneb-print-region")).toBe(true);
    expect(document.querySelector(".outer")!.classList.contains("deneb-print-ancestor")).toBe(true);
    expect(document.querySelector(".root")!.classList.contains("deneb-print-ancestor")).toBe(true);
    // Off-path sibling and inner content are left untouched.
    expect(document.querySelector(".sibling")!.className).toBe("sibling");
    expect(document.querySelector("p")!.className).toBe("");
  });

  it("clears every marker on afterprint", () => {
    document.body.innerHTML = `<div class="outer"><div class="target"></div></div>`;
    printElement(document.querySelector<HTMLElement>(".target")!);

    window.dispatchEvent(new Event("afterprint"));

    expect(document.body.classList.contains("deneb-printing")).toBe(false);
    expect(document.querySelector(".target")!.className).toBe("target");
    expect(document.querySelector(".outer")!.className).toBe("outer");
  });

  it("drops markers left behind by a previous uncleaned print", () => {
    document.body.innerHTML = `<div class="a"><div class="b"></div></div>`;
    const a = document.querySelector<HTMLElement>(".a")!;
    const b = document.querySelector<HTMLElement>(".b")!;

    printElement(a); // no afterprint fired → markers leak
    printElement(b); // must re-scope cleanly

    expect(a.classList.contains("deneb-print-region")).toBe(false);
    expect(a.classList.contains("deneb-print-ancestor")).toBe(true); // now b's ancestor
    expect(b.classList.contains("deneb-print-region")).toBe(true);
  });

  it("falls back to a full-window print when the element is null", () => {
    printElement(null);

    expect(printSpy).toHaveBeenCalledTimes(1);
    expect(document.body.classList.contains("deneb-printing")).toBe(false);
  });
});

describe("printClosest", () => {
  it("prints the nearest matching ancestor of the target", () => {
    document.body.innerHTML = `<div class="turn"><button class="p">인쇄</button></div>`;

    printClosest(document.querySelector<HTMLElement>(".p"), ".turn");

    expect(document.querySelector(".turn")!.classList.contains("deneb-print-region")).toBe(true);
    expect(printSpy).toHaveBeenCalledTimes(1);
  });

  it("marks nothing when no ancestor matches (whole-window fallback)", () => {
    document.body.innerHTML = `<div class="turn"><button class="p"></button></div>`;

    printClosest(document.querySelector<HTMLElement>(".p"), ".nope");

    // No match → printElement(null) → plain window.print(), no scoping markers.
    expect(document.querySelector(".turn")!.className).toBe("turn");
    expect(document.body.classList.contains("deneb-printing")).toBe(false);
  });
});
