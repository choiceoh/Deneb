import { describe, expect, it } from "vitest";
import { closeInTiles, focusedTile, isTileable, MAX_TILES, openInTiles, sanitizeTiles, splitInTiles } from "./tiling";
import type { View } from "./types";

const VALID: ReadonlySet<View> = new Set(["today", "mail", "calendar", "wiki", "todo", "chat", "files", "settings"]);

describe("isTileable", () => {
  it("excludes the dedicated surfaces", () => {
    expect(isTileable("chat")).toBe(false);
    expect(isTileable("files")).toBe(false);
    expect(isTileable("settings")).toBe(false);
    expect(isTileable("mail")).toBe(true);
  });
});

describe("focusedTile", () => {
  it("returns the view when tiled, else the first tile", () => {
    expect(focusedTile(["mail", "wiki"], "wiki")).toBe("wiki");
    expect(focusedTile(["mail", "wiki"], "settings")).toBe("mail");
  });
});

describe("openInTiles", () => {
  it("focuses an already-tiled view without reshaping", () => {
    expect(openInTiles(["mail", "wiki"], "mail", "wiki")).toEqual({ tiles: ["mail", "wiki"], focused: "wiki" });
  });

  it("replaces the focused slot for a new view", () => {
    expect(openInTiles(["mail", "wiki"], "mail", "todo")).toEqual({ tiles: ["todo", "wiki"], focused: "todo" });
  });

  it("leaves tiles alone for non-tileable views", () => {
    expect(openInTiles(["mail"], "mail", "settings")).toEqual({ tiles: ["mail"], focused: "mail" });
  });
});

describe("splitInTiles", () => {
  it("appends and focuses below the cap", () => {
    expect(splitInTiles(["mail"], "mail", "wiki")).toEqual({ tiles: ["mail", "wiki"], focused: "wiki" });
  });

  it("focuses instead of duplicating an existing tile", () => {
    expect(splitInTiles(["mail", "wiki"], "mail", "wiki")).toEqual({ tiles: ["mail", "wiki"], focused: "wiki" });
  });

  it("replaces the last non-focused slot at capacity", () => {
    expect(splitInTiles(["today", "wiki", "mail"], "mail", "calendar")).toEqual({
      tiles: ["today", "calendar", "mail"],
      focused: "calendar",
    });
  });

  it("refuses non-tileable views", () => {
    expect(splitInTiles(["mail"], "mail", "files")).toEqual({ tiles: ["mail"], focused: "mail" });
  });
});

describe("closeInTiles", () => {
  it("closes the focused tile and refocuses a neighbor", () => {
    expect(closeInTiles(["mail", "wiki", "todo"], "wiki")).toEqual({ tiles: ["mail", "todo"], focused: "todo" });
  });

  it("keeps focus when closing another tile", () => {
    expect(closeInTiles(["mail", "wiki"], "mail", "wiki")).toEqual({ tiles: ["mail"], focused: "mail" });
  });

  it("never closes the last tile", () => {
    expect(closeInTiles(["mail"], "mail")).toEqual({ tiles: ["mail"], focused: "mail" });
  });
});

describe("sanitizeTiles", () => {
  it("filters unknown, non-tileable, and duplicate entries", () => {
    expect(sanitizeTiles(["mail", "chat", "mail", "nope", 7, "wiki"], VALID)).toEqual(["mail", "wiki"]);
  });

  it("caps at MAX_TILES", () => {
    expect(sanitizeTiles(["today", "mail", "calendar", "wiki"], VALID)).toHaveLength(MAX_TILES);
  });

  it("falls back to today for garbage", () => {
    expect(sanitizeTiles("nonsense", VALID)).toEqual(["today"]);
    expect(sanitizeTiles(["chat"], VALID)).toEqual(["today"]);
  });
});
