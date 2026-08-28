import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it } from "vitest";

import { CygnusApp } from "./CygnusApp";

// jsdom has no saved config and no Tauri shell, so the app renders its
// disconnected companion state — that offline path is what CI can exercise
// (same policy as the workstation tests; live chat is verified on the host).
describe("CygnusApp", () => {
  afterEach(() => {
    cleanup();
    localStorage.clear();
  });

  it("renders the disconnected empty state with its own identity", () => {
    render(<CygnusApp />);
    expect(screen.getByText("Cygnus")).toBeInTheDocument();
    expect(screen.getByText("게이트웨이 연결 대기 중")).toBeInTheDocument();
    expect(screen.getByText(/Andromeda 본창에서 게이트웨이를 연결하면/)).toBeInTheDocument();
    expect(document.title).toBe("Cygnus");
  });

  it("defaults to the dark skin and persists a theme toggle", async () => {
    const user = userEvent.setup();
    const { container } = render(<CygnusApp />);
    const root = container.querySelector(".cygnus-root");
    expect(root).not.toBeNull();
    expect(root!.getAttribute("data-theme")).toBe("dark");
    await user.click(screen.getByRole("button", { name: "테마 전환" }));
    expect(root!.getAttribute("data-theme")).toBe("light");
    expect(localStorage.getItem("cygnus.theme")).toContain("light");
  });

  it("opens and closes the thread rail", async () => {
    const user = userEvent.setup();
    render(<CygnusApp />);
    await user.click(screen.getByRole("button", { name: "스레드 목록" }));
    expect(document.querySelector(".cy-rail")).not.toBeNull();
    await user.click(screen.getByRole("button", { name: "스레드 목록 닫기" }));
    expect(document.querySelector(".cy-rail")).toBeNull();
  });
});
