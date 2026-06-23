import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { getLogLevel, setLogLevel } from "@/log";
import { renderWithProviders } from "@/test/util";
import { SettingsPane } from "./SettingsPane";

describe("SettingsPane (설정)", () => {
  beforeEach(() => localStorage.clear());
  afterEach(() => setLogLevel("info")); // reset the module-level singleton between tests

  it("shows the section tabs and the gateway section by default", () => {
    renderWithProviders(<SettingsPane />, { connected: false });
    expect(screen.getByRole("heading", { name: "설정" })).toBeInTheDocument();
    // The three section tabs are present, with 연결 active first…
    expect(screen.getByRole("tab", { name: "연결" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("tab", { name: "일반" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "정보" })).toBeInTheDocument();
    // …and only the active (연결) tab's content is mounted.
    expect(screen.getByText("게이트웨이 연결")).toBeInTheDocument();
    expect(screen.queryByText("로그 레벨")).not.toBeInTheDocument();
    expect(screen.queryByText("좌측 탭")).not.toBeInTheDocument();
  });

  it("switches to the 정보 tab and shows the app version", () => {
    renderWithProviders(<SettingsPane />, { connected: false });
    expect(screen.queryByText(/v\d+\.\d+\.\d+/)).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("tab", { name: "정보" }));
    // The About section shows a real semver from package.json (Vite `define`).
    expect(screen.getByText(/v\d+\.\d+\.\d+/)).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "정보" })).toHaveAttribute("aria-selected", "true");
  });

  it("hides a nav tab via the 좌측 탭 toggles on the 일반 tab and keeps the reorder controls", () => {
    renderWithProviders(<SettingsPane />, { connected: false });
    fireEvent.click(screen.getByRole("tab", { name: "일반" }));
    // settings is never hideable — it's the way back to this screen.
    expect(screen.queryByRole("checkbox", { name: "설정" })).not.toBeInTheDocument();
    // The reorder (▲▼) controls survive the move into the 일반 tab.
    expect(screen.getAllByTitle("위로").length).toBeGreaterThan(0);
    const mail = screen.getByRole("checkbox", { name: "메일" });
    expect(mail).toBeChecked();
    fireEvent.click(mail);
    expect(mail).not.toBeChecked();
    expect(JSON.parse(localStorage.getItem("andromeda.hiddenPanes") ?? "[]")).toContain("mail");
  });

  it("edits the gateway URL through setCfg", () => {
    const setCfg = vi.fn();
    renderWithProviders(<SettingsPane />, { connected: false, cfg: { url: "", token: "" }, setCfg });
    fireEvent.change(screen.getByPlaceholderText("https://gateway.example"), {
      target: { value: "https://gw.test" },
    });
    expect(setCfg).toHaveBeenCalledWith({ url: "https://gw.test", token: "" });
  });

  it("applies and persists the log level on the 일반 tab", () => {
    renderWithProviders(<SettingsPane />, { connected: false });
    fireEvent.click(screen.getByRole("tab", { name: "일반" }));
    fireEvent.click(screen.getByRole("button", { name: "오류" }));
    expect(getLogLevel()).toBe("error");
    expect(localStorage.getItem("andromeda.logLevel")).toBe("error");
    expect(screen.getByRole("button", { name: "오류" })).toHaveAttribute("aria-pressed", "true");
  });
});
