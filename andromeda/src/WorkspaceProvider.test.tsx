import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { WorkspaceProvider } from "./WorkspaceProvider";
import { useWorkspace } from "./workspaceContext";

const cfg = { url: "http://gateway.test", token: "secret" };

function Probe() {
  const value = useWorkspace();
  return (
    <div>
      <output data-testid="connected">{String(value.connected)}</output>
      <output data-testid="cfg">{`${value.cfg.url}|${value.cfg.token}`}</output>
      <output data-testid="view">{value.view}</output>
      <output data-testid="pane-target">{value.paneTarget ? JSON.stringify(value.paneTarget) : "none"}</output>
      <output data-testid="ai-text">{value.aiText}</output>
      <output data-testid="active-resource">{value.activeResource ?? "none"}</output>
      <output data-testid="wiki-target">{value.wikiTarget ?? "none"}</output>
      <output data-testid="sink">{value.noteSink ? "set" : "none"}</output>
      <output data-testid="hidden">{value.hiddenViews.join(",")}</output>
      <output data-testid="order">{value.viewOrder.join(",")}</output>
      <output data-testid="notebook-top">{value.notebookTop}</output>

      <button onClick={() => value.setView("mail")}>set mail</button>
      <button onClick={() => value.openPane("todo")}>open todo</button>
      <button onClick={() => value.openPane("files", { path: "/projects/design.md", id: 7 })}>open file target</button>
      <button onClick={value.consumePaneTarget}>consume pane target</button>
      <button onClick={() => value.registerPane("wiki", "semantic wiki text")}>register pane</button>
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
