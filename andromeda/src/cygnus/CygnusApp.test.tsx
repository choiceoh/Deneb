import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it } from "vitest";

import { CygnusApp } from "./CygnusApp";

// jsdom has no saved config and no Tauri shell, so the app renders its
// disconnected companion state — that offline path is what CI can exercise
// (same policy as the workstation tests; live chat is verified on the host).
// jsdom ships no matchMedia; these tests install a minimal one to pick the
// docked (wide) or overlay (narrow) rail branch.
function stubMatchMedia(matches: boolean): void {
  Object.defineProperty(window, "matchMedia", {
    configurable: true,
    writable: true,
    value: (media: string) => ({
      matches,
      media,
      addEventListener: () => {},
      removeEventListener: () => {},
    }),
  });
}

describe("CygnusApp", () => {
  afterEach(() => {
    cleanup();
    localStorage.clear();
    delete (window as { matchMedia?: unknown }).matchMedia;
  });

  it("renders the disconnected empty state with its own identity", () => {
    render(<CygnusApp />);
    expect(screen.getByText("Cygnus")).toBeInTheDocument();
    expect(screen.getByText("게이트웨이 연결 대기 중")).toBeInTheDocument();
    expect(screen.getByText(/Andromeda 본창에서 연결하면/)).toBeInTheDocument();
    expect(document.title).toBe("Cygnus");
  });

  it("defaults to the light skin and persists a theme toggle", async () => {
    const user = userEvent.setup();
    const { container } = render(<CygnusApp />);
    const root = container.querySelector(".cygnus-root");
    expect(root).not.toBeNull();
    expect(root!.getAttribute("data-theme")).toBe("light");
    await user.click(screen.getByRole("button", { name: "테마 전환" }));
    expect(root!.getAttribute("data-theme")).toBe("dark");
    expect(localStorage.getItem("cygnus.theme")).toContain("dark");
  });

  it("keeps the docked thread rail up by default and remembers being closed", async () => {
    stubMatchMedia(true);
    const user = userEvent.setup();
    render(<CygnusApp />);
    // A standing part of the surface, not a popup to re-summon every visit.
    expect(document.querySelector(".cy-rail")).not.toBeNull();
    // Docked, it sits beside the thread — no scrim, nothing covered.
    expect(document.querySelector(".cy-scrim")).toBeNull();
    await user.click(screen.getByRole("button", { name: "스레드 목록" }));
    expect(document.querySelector(".cy-rail")).toBeNull();
    expect(localStorage.getItem("cygnus.rail")).toBe("closed");
    await user.click(screen.getByRole("button", { name: "스레드 목록" }));
    expect(document.querySelector(".cy-rail")).not.toBeNull();
    expect(localStorage.getItem("cygnus.rail")).toBe("open");
  });

  it("honours a stored close across a remount", () => {
    stubMatchMedia(true);
    localStorage.setItem("cygnus.rail", "closed");
    render(<CygnusApp />);
    expect(document.querySelector(".cy-rail")).toBeNull();
  });

  it("does not auto-open the rail when it can only overlay the thread", () => {
    // Narrow: an open rail rides a scrim over the conversation, so summoning
    // the window must not greet the user with a covered thread — even though
    // the stored preference says open.
    stubMatchMedia(false);
    localStorage.setItem("cygnus.rail", "open");
    render(<CygnusApp />);
    expect(document.querySelector(".cy-rail")).toBeNull();
  });

  it("opens as a scrimmed overlay when asked on a narrow window", async () => {
    stubMatchMedia(false);
    const user = userEvent.setup();
    render(<CygnusApp />);
    await user.click(screen.getByRole("button", { name: "스레드 목록" }));
    expect(document.querySelector(".cy-rail")).not.toBeNull();
    expect(document.querySelector(".cy-scrim")).not.toBeNull();
  });
});
