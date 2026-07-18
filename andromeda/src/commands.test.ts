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

  it("parses spotlight only with view + ref", () => {
    expect(parseWorkspaceCommand({ action: "spotlight", view: "approvals", ref: "99391" })).toEqual({
      kind: "spotlight",
      view: "approvals",
      ref: "99391",
    });
    expect(parseWorkspaceCommand({ action: "spotlight", view: "approvals" })).toBeNull();
    expect(parseWorkspaceCommand({ action: "spotlight", ref: "1" })).toBeNull();
  });

  it("parses prefill narrowly (todo + title only)", () => {
    expect(
      parseWorkspaceCommand({ action: "prefill", view: "todo", title: "견적 회신", due: "2026-07-21", note: "메모" }),
    ).toEqual({ kind: "prefill", view: "todo", title: "견적 회신", due: "2026-07-21", note: "메모" });
    // due가 형식 밖이면 조용히 떨어뜨리되 커맨드는 산다.
    expect(parseWorkspaceCommand({ action: "prefill", view: "todo", title: "x", due: "내일" })).toEqual({
      kind: "prefill",
      view: "todo",
      title: "x",
      due: undefined,
      note: undefined,
    });
    expect(parseWorkspaceCommand({ action: "prefill", view: "mail", title: "x" })).toBeNull();
    expect(parseWorkspaceCommand({ action: "prefill", view: "todo" })).toBeNull();
  });

  it("parses the date jump on split too", () => {
    expect(parseWorkspaceCommand({ action: "split", view: "mail", date: "2026-07-15" })).toMatchObject({
      kind: "split",
      view: "mail",
      date: "2026-07-15",
    });
  });

  it("parses the date jump on open and drops malformed dates", () => {
    expect(parseWorkspaceCommand({ action: "open", view: "mail", date: "2026-07-15" })).toMatchObject({
      kind: "open",
      view: "mail",
      date: "2026-07-15",
    });
    expect(parseWorkspaceCommand({ action: "open", view: "mail", date: "7월 15일" })).toMatchObject({
      kind: "open",
      view: "mail",
      date: undefined,
    });
  });
});

describe("describeCommand", () => {
  it("names the pane in Korean", () => {
    expect(describeCommand({ kind: "open", view: "mail" })).toContain("메일");
    expect(describeCommand({ kind: "split", view: "calendar" })).toContain("일정");
    expect(describeCommand({ kind: "layout", views: ["mail", "wiki"] })).toContain("메일 · 위키");
    expect(describeCommand({ kind: "wiki", path: "a/b.md" })).toContain("a/b.md");
    expect(describeCommand({ kind: "spotlight", view: "approvals", ref: "1" })).toContain("강조");
    expect(describeCommand({ kind: "prefill", view: "todo", title: "견적 회신" })).toContain("견적 회신");
    expect(describeCommand({ kind: "open", view: "mail", date: "2026-07-15" })).toContain("2026-07-15");
  });
});
