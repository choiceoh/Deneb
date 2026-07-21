import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { useContext } from "react";
import { WorkspaceProvider } from "./WorkspaceProvider";
import { FeedWriteCtx, useAiFeed, useWorkspace } from "./workspaceContext";

const cfg = { url: "http://gateway.test", token: "secret" };

function Probe() {
  const value = useWorkspace();
  const { aiText, activeResource } = useAiFeed();
  const feed = useContext(FeedWriteCtx)!;
  return (
    <div>
      <output data-testid="connected">{String(value.connected)}</output>
      <output data-testid="cfg">{`${value.cfg.url}|${value.cfg.token}`}</output>
      <output data-testid="view">{value.view}</output>
      <output data-testid="tiles">{value.tiles.join(",")}</output>
      <output data-testid="layouts">{value.layouts.map((l) => `${l.name}:${l.views.join("+")}`).join("|")}</output>
      <output data-testid="pane-target">{value.paneTarget ? JSON.stringify(value.paneTarget) : "none"}</output>
      <output data-testid="spotlight">{value.spotlight ? value.spotlight.view : "none"}</output>
      <output data-testid="ai-text">{aiText}</output>
      <output data-testid="active-resource">{activeResource ?? "none"}</output>
      <output data-testid="wiki-target">{value.wikiTarget ?? "none"}</output>
      <output data-testid="sink">{value.noteSink ? "set" : "none"}</output>
      <output data-testid="hidden">{value.hiddenViews.join(",")}</output>
      <output data-testid="order">{value.viewOrder.join(",")}</output>
      <output data-testid="notebook-top">{value.notebookTop}</output>

      <button onClick={() => value.setView("mail")}>set mail</button>
      <button onClick={() => value.openPane("todo")}>open todo</button>
      <button onClick={() => value.openPane("files", { path: "/projects/design.md", id: 7 })}>open file target</button>
      <button onClick={value.consumePaneTarget}>consume pane target</button>
      <button onClick={() => feed.registerPane(value.view, "wiki", "semantic wiki text")}>register pane</button>
      <button onClick={() => feed.registerPane("wiki", "wiki", "위키 본문")}>register wiki feed</button>
      <button onClick={() => feed.registerPane("mail", "mail", "메일 목록")}>register mail feed</button>
      <button onClick={() => value.openWiki("projects/deneb.md")}>open wiki</button>
      <button onClick={value.consumeWikiTarget}>consume wiki target</button>
      <button onClick={() => value.setNoteSink(async (text) => text === "ok")}>set sink</button>
      <button onClick={() => value.setNoteSink(null)}>clear sink</button>
      <button onClick={() => value.toggleViewHidden("mail")}>toggle mail hidden</button>
      <button onClick={() => value.toggleViewHidden("settings")}>toggle settings hidden</button>
      <button onClick={() => value.setViewOrder(["wiki", "today", "mail"])}>set order</button>
      <button onClick={() => value.setNotebookTop("folded")}>fold notebook</button>
      <button onClick={() => value.setNotebookTop("expanded")}>expand notebook</button>
      <button onClick={() => value.setCfg({ url: "http://changed.test", token: "changed" })}>set config</button>
      <button onClick={() => value.splitPane("wiki")}>split wiki</button>
      <button onClick={() => value.splitPane("mail")}>split mail</button>
      <button onClick={() => value.splitPane("calendar")}>split calendar</button>
      <button onClick={() => value.closePane("wiki")}>close wiki</button>
      <button onClick={() => value.closePane()}>close focused</button>
      <button onClick={() => value.applyLayout(["wiki", "mail"])}>apply wiki mail layout</button>
      <button onClick={() => value.saveLayout("아침 루틴")}>save layout</button>
      <button onClick={() => value.runCommand({ kind: "layout", views: ["wiki", "todo"] })}>run layout command</button>
      <button onClick={() => value.runCommand({ kind: "open", view: "mail" })}>run open command</button>
    </div>
  );
}

function renderProvider(
  overrides: Partial<{
    connected: boolean;
    cfg: typeof cfg;
    setCfg: (value: typeof cfg) => void;
  }> = {},
) {
  const props = {
    connected: true,
    cfg,
    setCfg: vi.fn(),
    ...overrides,
  };
  const view = render(
    <WorkspaceProvider {...props}>
      <Probe />
    </WorkspaceProvider>,
  );
  return { props, ...view };
}

beforeEach(() => localStorage.clear());

describe("WorkspaceProvider core state", () => {
  it("starts on today and reflects gateway props", () => {
    renderProvider();
    expect(screen.getByTestId("connected")).toHaveTextContent("true");
    expect(screen.getByTestId("cfg")).toHaveTextContent("http://gateway.test|secret");
    expect(screen.getByTestId("view")).toHaveTextContent("today");
    expect(screen.getByTestId("pane-target")).toHaveTextContent("none");
  });

  it("reflects disconnected state without altering config", () => {
    renderProvider({ connected: false });
    expect(screen.getByTestId("connected")).toHaveTextContent("false");
    expect(screen.getByTestId("cfg")).toHaveTextContent("http://gateway.test|secret");
  });

  it("allows direct view changes", async () => {
    renderProvider();
    await userEvent.click(screen.getByRole("button", { name: "set mail" }));
    expect(screen.getByTestId("view")).toHaveTextContent("mail");
  });

  it("opens a pane without a target and clears any old target", async () => {
    renderProvider();
    await userEvent.click(screen.getByRole("button", { name: "open file target" }));
    expect(screen.getByTestId("pane-target")).toHaveTextContent("design.md");

    await userEvent.click(screen.getByRole("button", { name: "open todo" }));

    expect(screen.getByTestId("view")).toHaveTextContent("todo");
    expect(screen.getByTestId("pane-target")).toHaveTextContent("none");
  });

  it("flashes the destination tile on every deep-link open (spotlight)", async () => {
    renderProvider();
    expect(screen.getByTestId("spotlight")).toHaveTextContent("none");

    // A deep-link jump (오늘 KPI·타임라인·팔레트 열기 모두 openPane) must flash where
    // it landed — otherwise arriving on an already-tiled pane is invisible.
    await userEvent.click(screen.getByRole("button", { name: "open todo" }));
    expect(screen.getByTestId("spotlight")).toHaveTextContent("todo");

    await userEvent.click(screen.getByRole("button", { name: "run open command" }));
    expect(screen.getByTestId("spotlight")).toHaveTextContent("mail");
  });

  it("publishes a typed pane target with its destination view", async () => {
    renderProvider();
    await userEvent.click(screen.getByRole("button", { name: "open file target" }));

    expect(screen.getByTestId("view")).toHaveTextContent("files");
    expect(JSON.parse(screen.getByTestId("pane-target").textContent ?? "{}")).toEqual({
      view: "files",
      path: "/projects/design.md",
      id: 7,
    });
  });

  it("consumes a pane target without changing the active view", async () => {
    renderProvider();
    await userEvent.click(screen.getByRole("button", { name: "open file target" }));
    await userEvent.click(screen.getByRole("button", { name: "consume pane target" }));
    expect(screen.getByTestId("pane-target")).toHaveTextContent("none");
    expect(screen.getByTestId("view")).toHaveTextContent("files");
  });

  it("when registers the active pane's semantic AI projection", async () => {
    renderProvider();
    await userEvent.click(screen.getByRole("button", { name: "register pane" }));
    expect(screen.getByTestId("ai-text")).toHaveTextContent("semantic wiki text");
    expect(screen.getByTestId("active-resource")).toHaveTextContent("wiki");
  });

  it("when opens and consumes a wiki target", async () => {
    renderProvider();
    await userEvent.click(screen.getByRole("button", { name: "open wiki" }));
    expect(screen.getByTestId("view")).toHaveTextContent("wiki");
    expect(screen.getByTestId("wiki-target")).toHaveTextContent("projects/deneb.md");

    await userEvent.click(screen.getByRole("button", { name: "consume wiki target" }));
    expect(screen.getByTestId("wiki-target")).toHaveTextContent("none");
    expect(screen.getByTestId("view")).toHaveTextContent("wiki");
  });

  it("stores a function-valued note sink without invoking it as a state updater", async () => {
    renderProvider();
    await userEvent.click(screen.getByRole("button", { name: "set sink" }));
    expect(screen.getByTestId("sink")).toHaveTextContent("set");

    await userEvent.click(screen.getByRole("button", { name: "clear sink" }));
    expect(screen.getByTestId("sink")).toHaveTextContent("none");
  });

  it("when forwards config edits to the lifted owner", async () => {
    const setCfg = vi.fn();
    renderProvider({ setCfg });
    await userEvent.click(screen.getByRole("button", { name: "set config" }));
    expect(setCfg).toHaveBeenCalledWith({ url: "http://changed.test", token: "changed" });
  });

  it("reflects a new config prop without remounting provider state", async () => {
    const { rerender } = renderProvider();
    await userEvent.click(screen.getByRole("button", { name: "set mail" }));
    const next = { url: "http://second.test", token: "second" };
    rerender(
      <WorkspaceProvider connected cfg={next} setCfg={() => {}}>
        <Probe />
      </WorkspaceProvider>,
    );
    expect(screen.getByTestId("cfg")).toHaveTextContent("http://second.test|second");
    expect(screen.getByTestId("view")).toHaveTextContent("mail");
  });
});

describe("WorkspaceProvider tiled workspace", () => {
  it("splits into a new focused tile and closes back", async () => {
    renderProvider();
    await userEvent.click(screen.getByRole("button", { name: "split wiki" }));
    expect(screen.getByTestId("tiles")).toHaveTextContent("today,wiki");
    expect(screen.getByTestId("view")).toHaveTextContent("wiki");

    await userEvent.click(screen.getByRole("button", { name: "close wiki" }));
    expect(screen.getByTestId("tiles")).toHaveTextContent(/^today$/);
    expect(screen.getByTestId("view")).toHaveTextContent("today");
  });

  it("setView replaces the focused slot, preserving the split", async () => {
    renderProvider();
    await userEvent.click(screen.getByRole("button", { name: "split wiki" }));
    await userEvent.click(screen.getByRole("button", { name: "set mail" }));
    expect(screen.getByTestId("tiles")).toHaveTextContent("today,mail");
    expect(screen.getByTestId("view")).toHaveTextContent("mail");
  });

  it("caps at three tiles by replacing the last non-focused slot", async () => {
    renderProvider();
    await userEvent.click(screen.getByRole("button", { name: "split wiki" }));
    await userEvent.click(screen.getByRole("button", { name: "split mail" }));
    expect(screen.getByTestId("tiles")).toHaveTextContent("today,wiki,mail");

    await userEvent.click(screen.getByRole("button", { name: "split calendar" }));
    expect(screen.getByTestId("tiles")).toHaveTextContent("today,calendar,mail");
    expect(screen.getByTestId("view")).toHaveTextContent("calendar");
  });

  it("persists tiles + focus and restores them on next launch", async () => {
    const first = renderProvider();
    await userEvent.click(screen.getByRole("button", { name: "split wiki" }));
    await waitFor(() =>
      expect(JSON.parse(localStorage.getItem("andromeda.tiles") ?? "{}")).toEqual({
        tiles: ["today", "wiki"],
        focused: "wiki",
      }),
    );
    first.unmount();

    renderProvider();
    expect(screen.getByTestId("tiles")).toHaveTextContent("today,wiki");
    expect(screen.getByTestId("view")).toHaveTextContent("wiki");
  });

  it("merges visible tile feeds focused-first with 화면 headers", async () => {
    renderProvider();
    await userEvent.click(screen.getByRole("button", { name: "register wiki feed" }));
    await userEvent.click(screen.getByRole("button", { name: "register mail feed" }));
    await userEvent.click(screen.getByRole("button", { name: "apply wiki mail layout" }));

    expect(screen.getByTestId("ai-text")).toHaveTextContent("위키 본문");
    expect(screen.getByTestId("ai-text")).toHaveTextContent("[분할 화면: 메일]");
    expect(screen.getByTestId("active-resource")).toHaveTextContent("wiki");
  });

  it("saves and lists named layouts", async () => {
    renderProvider();
    await userEvent.click(screen.getByRole("button", { name: "split wiki" }));
    await userEvent.click(screen.getByRole("button", { name: "save layout" }));
    expect(screen.getByTestId("layouts")).toHaveTextContent("아침 루틴:today+wiki");
    await waitFor(() =>
      expect(JSON.parse(localStorage.getItem("andromeda.layouts") ?? "[]")).toEqual([
        { name: "아침 루틴", views: ["today", "wiki"] },
      ]),
    );
  });

  it("runs workspace commands through the bus", async () => {
    renderProvider();
    await userEvent.click(screen.getByRole("button", { name: "run layout command" }));
    expect(screen.getByTestId("tiles")).toHaveTextContent("wiki,todo");

    await userEvent.click(screen.getByRole("button", { name: "run open command" }));
    expect(screen.getByTestId("view")).toHaveTextContent("mail");
    expect(screen.getByTestId("tiles")).toHaveTextContent("mail,todo");
  });
});

describe("WorkspaceProvider navigation persistence", () => {
  it("loads and filters hidden views", () => {
    localStorage.setItem("andromeda.hiddenPanes", JSON.stringify(["mail", "settings", 7, null, "wiki"]));
    renderProvider();
    expect(screen.getByTestId("hidden")).toHaveTextContent("mail,wiki");
  });

  it.each(["not-json", "{}", "null", "7"])("falls back safely for invalid hidden storage %s", (stored) => {
    localStorage.setItem("andromeda.hiddenPanes", stored);
    renderProvider();
    expect(screen.getByTestId("hidden")).toBeEmptyDOMElement();
  });

  it("toggles a normal view and saves both transitions", async () => {
    renderProvider();
    await userEvent.click(screen.getByRole("button", { name: "toggle mail hidden" }));
    expect(screen.getByTestId("hidden")).toHaveTextContent("mail");
    await waitFor(() => expect(localStorage.getItem("andromeda.hiddenPanes")).toBe('["mail"]'));

    await userEvent.click(screen.getByRole("button", { name: "toggle mail hidden" }));
    expect(screen.getByTestId("hidden")).toBeEmptyDOMElement();
    await waitFor(() => expect(localStorage.getItem("andromeda.hiddenPanes")).toBe("[]"));
  });

  it("rejects hides settings", async () => {
    renderProvider();
    await userEvent.click(screen.getByRole("button", { name: "toggle settings hidden" }));
    expect(screen.getByTestId("hidden")).toBeEmptyDOMElement();
    expect(localStorage.getItem("andromeda.hiddenPanes")).toBe("[]");
  });

  it("loads string view order entries and ignores malformed values", () => {
    localStorage.setItem("andromeda.viewOrder", JSON.stringify(["wiki", 9, null, "mail"]));
    renderProvider();
    expect(screen.getByTestId("order")).toHaveTextContent("wiki,mail");
  });

  it("saves a replacement view order", async () => {
    renderProvider();
    await userEvent.click(screen.getByRole("button", { name: "set order" }));
    expect(screen.getByTestId("order")).toHaveTextContent("wiki,today,mail");
    await waitFor(() => expect(localStorage.getItem("andromeda.viewOrder")).toBe('["wiki","today","mail"]'));
  });
});

describe("WorkspaceProvider notebook sizing", () => {
  it.each(["folded", "default", "expanded"] as const)("loads valid notebook mode %s", (mode) => {
    localStorage.setItem("andromeda.notebook.top", JSON.stringify(mode));
    renderProvider();
    expect(screen.getByTestId("notebook-top")).toHaveTextContent(mode);
  });

  it("migrates the legacy folded boolean", () => {
    localStorage.setItem("andromeda.notebook.folded", "true");
    renderProvider();
    expect(screen.getByTestId("notebook-top")).toHaveTextContent("folded");
  });

  it.each(["false", "null", '"bad"'])("uses default when legacy/current values are %s", (stored) => {
    localStorage.setItem("andromeda.notebook.folded", stored);
    renderProvider();
    expect(screen.getByTestId("notebook-top")).toHaveTextContent("default");
  });

  it("saves folded and expanded transitions", async () => {
    renderProvider();
    await userEvent.click(screen.getByRole("button", { name: "fold notebook" }));
    expect(screen.getByTestId("notebook-top")).toHaveTextContent("folded");
    await waitFor(() => expect(localStorage.getItem("andromeda.notebook.top")).toBe('"folded"'));

    await userEvent.click(screen.getByRole("button", { name: "expand notebook" }));
    expect(screen.getByTestId("notebook-top")).toHaveTextContent("expanded");
    await waitFor(() => expect(localStorage.getItem("andromeda.notebook.top")).toBe('"expanded"'));
  });
});
