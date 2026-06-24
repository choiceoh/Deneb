import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { getLogLevel, setLogLevel } from "@/log";
import { renderWithProviders } from "@/test/util";
import { SettingsPane } from "./SettingsPane";

describe("SettingsPane (설정)", () => {
  beforeEach(() => localStorage.clear());
  afterEach(() => {
    setLogLevel("info"); // reset the module-level singleton between tests
    vi.unstubAllGlobals();
  });

  it("shows the section tabs and the gateway section by default", () => {
    renderWithProviders(<SettingsPane />, { connected: false });
    expect(screen.getByRole("heading", { name: "설정" })).toBeInTheDocument();
    // The three section tabs are present, with 연결 active first…
    expect(screen.getByRole("tab", { name: "연결" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("tab", { name: "일반" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "프롬프트" })).toBeInTheDocument();
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

  it("navigates tabs with the keyboard (arrows wrap, Home jumps) and roves tabindex", () => {
    renderWithProviders(<SettingsPane />, { connected: false });
    // Roving tabindex: only the active tab is in the page tab order.
    expect(screen.getByRole("tab", { name: "연결" })).toHaveAttribute("tabindex", "0");
    expect(screen.getByRole("tab", { name: "정보" })).toHaveAttribute("tabindex", "-1");

    // ArrowLeft from the first tab wraps to the last (정보).
    fireEvent.keyDown(screen.getByRole("tab", { name: "연결" }), { key: "ArrowLeft" });
    expect(screen.getByRole("tab", { name: "정보" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByText(/v\d+\.\d+\.\d+/)).toBeInTheDocument();

    // Home jumps back to the first tab and its content.
    fireEvent.keyDown(screen.getByRole("tab", { name: "정보" }), { key: "Home" });
    expect(screen.getByRole("tab", { name: "연결" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByText("게이트웨이 연결")).toBeInTheDocument();
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

  it("loads, edits, and resets prompt templates on the 프롬프트 tab", async () => {
    const calls: Array<{ method: string; params: Record<string, unknown> }> = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_url: string, init?: RequestInit) => {
        const body = JSON.parse(String(init?.body ?? "{}")) as { method: string; params: Record<string, unknown> };
        calls.push({ method: body.method, params: body.params ?? {} });
        const reply = (payload: unknown) =>
          ({ ok: true, json: async () => ({ ok: true, payload }) }) as unknown as Response;
        switch (body.method) {
          case "miniapp.prompts.list":
            return reply({
              prompts: [
                {
                  id: "mail.analysis",
                  title: "메일 분석",
                  category: "mail",
                  editable: true,
                  overridden: false,
                },
              ],
            });
          case "miniapp.prompts.get":
            return reply({
              id: "mail.analysis",
              title: "메일 분석",
              description: "메일을 요약합니다.",
              category: "mail",
              editable: true,
              overridden: false,
              text: "기본 분석 지시",
              defaultText: "기본 분석 지시",
            });
          case "miniapp.prompts.update":
            return reply({
              id: "mail.analysis",
              title: "메일 분석",
              category: "mail",
              editable: true,
              overridden: true,
              text: body.params.text,
              defaultText: "기본 분석 지시",
            });
          case "miniapp.prompts.reset":
            return reply({
              id: "mail.analysis",
              title: "메일 분석",
              category: "mail",
              editable: true,
              overridden: false,
              text: "기본 분석 지시",
              defaultText: "기본 분석 지시",
            });
          default:
            return reply({});
        }
      }),
    );

    renderWithProviders(<SettingsPane />, { connected: true, cfg: { url: "http://test", token: "tok" } });
    fireEvent.click(screen.getByRole("tab", { name: "프롬프트" }));

    expect(await screen.findByRole("button", { name: /메일 분석/ })).toBeInTheDocument();
    expect(await screen.findByDisplayValue("기본 분석 지시")).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("프롬프트 본문"), { target: { value: "새 분석 지시" } });
    fireEvent.click(screen.getByRole("button", { name: "저장" }));
    await waitFor(() =>
      expect(
        calls.some((call) => call.method === "miniapp.prompts.update" && call.params.text === "새 분석 지시"),
      ).toBe(true),
    );
    expect(await screen.findByText("저장됨")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "초기화" }));
    await waitFor(() => expect(calls.some((call) => call.method === "miniapp.prompts.reset")).toBe(true));
    expect(await screen.findByDisplayValue("기본 분석 지시")).toBeInTheDocument();
  });
});
