import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { getLogLevel, setLogLevel } from "@/log";
import { renderWithProviders } from "@/test/util";
import { SettingsPane } from "./SettingsPane";

describe("SettingsPane (설정)", () => {
  beforeEach(() => localStorage.clear());
  afterEach(() => setLogLevel("info")); // reset the module-level singleton between tests

  it("renders the gateway, log-level, nav-tab, and about sections with the app version", () => {
    renderWithProviders(<SettingsPane />, { connected: false });
    expect(screen.getByRole("heading", { name: "설정" })).toBeInTheDocument();
    expect(screen.getByText("게이트웨이 연결")).toBeInTheDocument();
    expect(screen.getByText("로그 레벨")).toBeInTheDocument();
    expect(screen.getByText("좌측 탭")).toBeInTheDocument();
    // The About section shows a real semver from package.json (Vite `define`).
    expect(screen.getByText(/v\d+\.\d+\.\d+/)).toBeInTheDocument();
  });

  it("hides a nav tab via the 좌측 탭 toggles and persists it", () => {
    renderWithProviders(<SettingsPane />, { connected: false });
    // settings is never hideable — it's the way back to this screen.
    expect(screen.queryByRole("checkbox", { name: "설정" })).not.toBeInTheDocument();
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

  it("applies and persists the log level", () => {
    renderWithProviders(<SettingsPane />, { connected: false });
    fireEvent.click(screen.getByRole("button", { name: "오류" }));
    expect(getLogLevel()).toBe("error");
    expect(localStorage.getItem("andromeda.logLevel")).toBe("error");
    expect(screen.getByRole("button", { name: "오류" })).toHaveAttribute("aria-pressed", "true");
  });
});
