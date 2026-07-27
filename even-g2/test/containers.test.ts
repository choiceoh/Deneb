import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { checkContainers, containerBlocks } from "../src/containers";

const SRC = readFileSync(new URL("../src/main.ts", import.meta.url), "utf8");

const text = (id: number, extra = "") =>
  `new TextContainerProperty({\n  containerID: ${id},\n  isEventCapture: 0,\n${extra}})`;
const image = (id: number, w = 280, h = 140, extra = "") =>
  `new ImageContainerProperty({\n  containerID: ${id},\n  width: ${w},\n  height: ${h},\n${extra}})`;

describe("checkContainers — proven against known-bad samples", () => {
  it("passes a sound page", () => {
    const src =
      text(1, "  isEventCapture: 1,\n  zOrderIndex: 0,\n") +
      text(2, "  zOrderIndex: 1,\n") +
      image(3, 280, 140, "  zOrderIndex: 2,\n");
    expect(checkContainers(src)).toEqual([]);
  });

  it("CATCHES the bug that bricked the app: partial zOrderIndex", () => {
    // The real regression — container 1 had none while the other three did, the
    // SDK rejected the page, and the glasses showed nothing at all.
    const src =
      text(1, "  isEventCapture: 1,\n") +
      text(2, "  zOrderIndex: 1,\n") +
      image(3, 280, 140, "  zOrderIndex: 2,\n");
    expect(checkContainers(src).join(" ")).toMatch(/zOrderIndex on 2\/3/);
  });

  it("catches duplicate zOrderIndex", () => {
    const src =
      text(1, "  isEventCapture: 1,\n  zOrderIndex: 1,\n") +
      text(2, "  zOrderIndex: 1,\n");
    expect(checkContainers(src)).toContain("duplicate zOrderIndex");
  });

  it("catches duplicate containerID", () => {
    const src = text(1, "  isEventCapture: 1,\n") + text(1, "");
    expect(checkContainers(src)).toContain("duplicate containerID");
  });

  it("catches a missing or doubled event-capture container", () => {
    expect(checkContainers(text(1) + text(2)).join(" ")).toMatch(
      /0 event-capture/,
    );
    const two =
      text(1, "  isEventCapture: 1,\n") + text(2, "  isEventCapture: 1,\n");
    expect(checkContainers(two).join(" ")).toMatch(/2 event-capture/);
  });

  it("catches an image container sitting on the size boundary", () => {
    const src = text(1, "  isEventCapture: 1,\n") + image(2, 288, 144);
    const out = checkContainers(src).join(" ");
    expect(out).toMatch(/image width 288/);
    expect(out).toMatch(/image height 144/);
  });

  it("finds every declaration in the real main.ts", () => {
    expect(containerBlocks(SRC).length).toBeGreaterThan(1);
  });

  it("the shipped page is sound", () => {
    expect(checkContainers(SRC)).toEqual([]);
  });
});
