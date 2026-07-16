import { describe, expect, it } from "vitest";
import { describeCommand, isWorkspaceCommandKind, parseWorkspaceCommand } from "./commands";

describe("isWorkspaceCommandKind", () => {
  it("matches only the workspace control kinds", () => {
    expect(isWorkspaceCommandKind("workspace")).toBe(true);
    expect(isWorkspaceCommandKind("workspace.command")).toBe(true);
    expect(isWorkspaceCommandKind("mail")).toBe(false);
    expect(isWorkspaceCommandKind(undefined)).toBe(false);
  });
});

describe("parseWorkspaceCommand", () => {
  it("parses open with view, ref and query", () => {
    expect(parseWorkspaceCommand({ action: "open", view: "mail", ref: "m1" })).toEqual({
      kind: "open",
      view: "mail",
      ref: "m1",
      query: undefined,
    });
    expect(parseWorkspaceCommand({ action: "open", view: "search", query: "태양광" })).toEqual({
      kind: "open",
      view: "search",
      ref: undefined,
      query: "태양광",
    });
  });

  it("routes open with a path to the wiki", () => {
    expect(parseWorkspaceCommand({ action: "open", path: "프로젝트/데네브.md" })).toEqual({
      kind: "wiki",
      path: "프로젝트/데네브.md",
    });
    expect(parseWorkspaceCommand({ action: "wiki", ref: "인물/김호수.md" })).toEqual({
      kind: "wiki",
      path: "인물/김호수.md",
    });
    // The events stream's standard ref field also carries the page for an
    // explicit wiki open (WikiPane consumes wikiTarget, not a pane-target id).
    expect(parseWorkspaceCommand({ action: "open", view: "wiki", ref: "projects/foo.md" })).toEqual({
      kind: "wiki",
      path: "projects/foo.md",
    });
  });

  it("parses split/focus/close", () => {
    expect(parseWorkspaceCommand({ action: "split", view: "calendar" })).toEqual({
      kind: "split",
      view: "calendar",
      ref: undefined,
    });
    expect(parseWorkspaceCommand({ action: "focus", pane: "todo" })).toEqual({ kind: "focus", view: "todo" });
    expect(parseWorkspaceCommand({ action: "close", view: "mail" })).toEqual({ kind: "close", view: "mail" });
    expect(parseWorkspaceCommand({ action: "close" })).toEqual({ kind: "close", view: undefined });
  });

  it("rejects a close naming an unknown view instead of closing the focused tile", () => {
    expect(parseWorkspaceCommand({ action: "close", view: "bogus" })).toBeNull();
    expect(parseWorkspaceCommand({ action: "close", pane: "nonsense" })).toBeNull();
  });

  it("rejects splits of non-tileable views", () => {
    expect(parseWorkspaceCommand({ action: "split", view: "settings" })).toBeNull();
    expect(parseWorkspaceCommand({ action: "split", view: "chat" })).toBeNull();
  });

  it("parses layout from arrays and comma strings, dropping junk", () => {
    expect(parseWorkspaceCommand({ action: "layout", views: ["mail", "calendar"] })).toEqual({
      kind: "layout",
      views: ["mail", "calendar"],
    });
    expect(parseWorkspaceCommand({ action: "layout", views: "wiki, todo, nope, chat" })).toEqual({
      kind: "layout",
      views: ["wiki", "todo"],
    });
    expect(parseWorkspaceCommand({ action: "layout", views: [] })).toBeNull();
  });

  it("rejects unknown actions and views", () => {
    expect(parseWorkspaceCommand({ action: "delete_all", view: "mail" })).toBeNull();
    expect(parseWorkspaceCommand({ action: "open", view: "nonsense" })).toBeNull();
    expect(parseWorkspaceCommand({})).toBeNull();
  });
});

describe("describeCommand", () => {
  it("names the pane in Korean", () => {
    expect(describeCommand({ kind: "open", view: "mail" })).toContain("메일");
    expect(describeCommand({ kind: "split", view: "calendar" })).toContain("일정");
    expect(describeCommand({ kind: "layout", views: ["mail", "wiki"] })).toContain("메일 · 위키");
    expect(describeCommand({ kind: "wiki", path: "a/b.md" })).toContain("a/b.md");
  });
});
