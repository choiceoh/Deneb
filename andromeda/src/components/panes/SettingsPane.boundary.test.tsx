import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { checkForUpdates } from "@/updater";
import { getLogLevel, setLogLevel } from "@/log";
import { renderWithProviders } from "@/test/util";
import { SettingsPane } from "./SettingsPane";

vi.mock("@/updater", () => ({ checkForUpdates: vi.fn() }));

type RpcCall = { method: string; params: Record<string, unknown> };

const promptRows = [
  {
    id: "mail.analysis",
    title: "메일 분석",
    category: "mail",
    editable: true,
    overridden: true,
  },
  {
    id: "system.guardrail",
    title: "시스템 안전 지침",
    category: "system",
    editable: false,
    overridden: false,
  },
  {
    id: "",
    title: "식별자 없는 항목",
    category: "broken",
    editable: true,
    overridden: false,
  },
];

const promptDetails: Record<string, Record<string, unknown>> = {
  "mail.analysis": {
    id: "mail.analysis",
    title: "메일 분석",
    description: "메일의 요청, 기한, 위험을 구조화합니다.",
    category: "mail",
    editable: true,
    overridden: true,
    text: "요청과 마감부터 확인한다.",
    defaultText: "메일을 요약한다.",
    updatedAtMs: 1_752_480_000_000,
  },
  "system.guardrail": {
    id: "system.guardrail",
    title: "시스템 안전 지침",
    description: "외부 발송 전 확인합니다.",
    category: "system",
    editable: false,
    overridden: false,
    text: "외부 변경은 확인 후 실행한다.",
    defaultText: "외부 변경은 확인 후 실행한다.",
  },
};

function rpcResponse(payload: unknown, ok = true): Response {
  return {
    ok,
    status: ok ? 200 : 500,
    json: async () => (ok ? { ok: true, payload } : { ok: false, error: String(payload) }),
  } as Response;
}

function installRpc(
  calls: RpcCall[],
  overrides: Partial<Record<string, (params: Record<string, unknown>) => Promise<Response> | Response>> = {},
) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url: string, init?: RequestInit) => {
      const body = JSON.parse(String(init?.body ?? "{}")) as RpcCall;
      const method = body.method ?? "";
      const params = body.params ?? {};
      calls.push({ method, params });
      if (overrides[method]) return overrides[method]!(params);
      switch (method) {
        case "miniapp.prompts.list":
          return rpcResponse({ prompts: promptRows });
        case "miniapp.prompts.get":
          return rpcResponse(promptDetails[String(params.id)] ?? {});
        case "miniapp.prompts.update":
          return rpcResponse({
            ...promptDetails[String(params.id)],
            overridden: true,
            text: params.text,
          });
        case "miniapp.prompts.reset":
          return rpcResponse({
            ...promptDetails[String(params.id)],
            overridden: false,
            text: "메일을 요약한다.",
          });
        case "miniapp.health":
          return rpcResponse({ ok: true });
        default:
          return rpcResponse({});
      }
    }),
  );
}

async function openPromptTab() {
  await userEvent.click(screen.getByRole("tab", { name: "프롬프트" }));
  return screen.findByRole("complementary", { name: "프롬프트 목록" });
}

describe("SettingsPane boundary behavior", () => {
  let calls: RpcCall[];

  beforeEach(() => {
    localStorage.clear();
    calls = [];
    installRpc(calls);
    vi.mocked(checkForUpdates).mockReset();
  });

  afterEach(() => {
    setLogLevel("info");
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  describe("connection identity and persistence", () => {
    it("preserves URL and token edits independent and sends the complete next config", async () => {
      const setCfg = vi.fn();
      renderWithProviders(<SettingsPane />, {
        connected: false,
        cfg: { url: "https://old.example", token: "old-token" },
        setCfg,
      });

      fireEvent.change(screen.getByPlaceholderText("https://gateway.example"), {
        target: { value: "https://new.example" },
      });
      expect(setCfg).toHaveBeenLastCalledWith({ url: "https://new.example", token: "old-token" });

      fireEvent.change(screen.getByPlaceholderText("토큰"), { target: { value: "next-token" } });
      expect(setCfg).toHaveBeenLastCalledWith({ url: "https://old.example", token: "next-token" });
    });

    it("masks the token and associates both fields with visible labels", () => {
      renderWithProviders(<SettingsPane />, {
        connected: false,
        cfg: { url: "https://gateway.example", token: "secret" },
      });

      expect(screen.getByLabelText("URL")).toHaveValue("https://gateway.example");
      expect(screen.getByLabelText("클라이언트 토큰")).toHaveAttribute("type", "password");
      expect(screen.getByLabelText("클라이언트 토큰")).toHaveValue("secret");
    });

    it("shows the disconnected state without misrepresenting it as a transport error", () => {
      renderWithProviders(<SettingsPane />, { connected: false });

      expect(screen.getByText("미연결")).toBeInTheDocument();
      expect(screen.queryByText(/^오류/)).not.toBeInTheDocument();
    });

    it("checks gateway health when the connection is saved", async () => {
      renderWithProviders(<SettingsPane />, {
        connected: true,
        cfg: { url: "http://test", token: "tok" },
      });

      await userEvent.click(screen.getByRole("button", { name: "연결 / 저장" }));

      await waitFor(() => expect(calls.some((call) => call.method === "miniapp.ping")).toBe(true));
    });

    it("renders a gateway check failure as an explicit error state", async () => {
      calls = [];
      installRpc(calls, {
        "miniapp.ping": () => Promise.reject(new Error("connection refused")),
      });
      renderWithProviders(<SettingsPane />, {
        connected: true,
        cfg: { url: "http://test", token: "tok" },
      });

      await userEvent.click(screen.getByRole("button", { name: "연결 / 저장" }));

      expect((await screen.findAllByText(/connection refused/)).length).toBeGreaterThan(0);
    });
  });

  describe("accessible tab navigation", () => {
    it.each([
      ["연결", "ArrowRight", "일반"],
      ["일반", "ArrowRight", "프롬프트"],
      ["프롬프트", "ArrowRight", "정보"],
      ["정보", "ArrowRight", "연결"],
      ["연결", "ArrowLeft", "정보"],
    ])("moves from %s with %s and selects %s", (from, key, to) => {
      renderWithProviders(<SettingsPane />, { connected: false });
      fireEvent.click(screen.getByRole("tab", { name: from }));
      fireEvent.keyDown(screen.getByRole("tab", { name: from }), { key });
      expect(screen.getByRole("tab", { name: to })).toHaveAttribute("aria-selected", "true");
      expect(screen.getByRole("tab", { name: to })).toHaveFocus();
    });

    it("jumps to the first and last tab with Home and End", () => {
      renderWithProviders(<SettingsPane />, { connected: false });
      const general = screen.getByRole("tab", { name: "일반" });
      fireEvent.click(general);

      fireEvent.keyDown(general, { key: "End" });
      expect(screen.getByRole("tab", { name: "정보" })).toHaveFocus();
      expect(screen.getByRole("tabpanel")).toHaveAccessibleName("정보");

      fireEvent.keyDown(screen.getByRole("tab", { name: "정보" }), { key: "Home" });
      expect(screen.getByRole("tab", { name: "연결" })).toHaveFocus();
      expect(screen.getByRole("tabpanel")).toHaveAccessibleName("연결");
    });

    it("without hijack unrelated keys", () => {
      renderWithProviders(<SettingsPane />, { connected: false });
      const first = screen.getByRole("tab", { name: "연결" });
      first.focus();
      fireEvent.keyDown(first, { key: "PageDown" });
      expect(first).toHaveFocus();
      expect(first).toHaveAttribute("aria-selected", "true");
    });

    it("preserves exactly one tab in the sequential focus order", () => {
      renderWithProviders(<SettingsPane />, { connected: false });
      fireEvent.click(screen.getByRole("tab", { name: "프롬프트" }));
      const tabs = screen.getAllByRole("tab");
      expect(tabs.filter((tab) => tab.tabIndex === 0)).toHaveLength(1);
      expect(screen.getByRole("tab", { name: "프롬프트" })).toHaveAttribute("tabindex", "0");
    });
  });

  describe("rail customization and log policy", () => {
    it("persists every supported log level and updates the pressed state", async () => {
      renderWithProviders(<SettingsPane />, { connected: false });
      await userEvent.click(screen.getByRole("tab", { name: "일반" }));

      for (const [label, value] of [
        ["디버그", "debug"],
        ["정보", "info"],
        ["경고", "warn"],
        ["오류", "error"],
        ["끄기", "silent"],
      ] as const) {
        await userEvent.click(screen.getByRole("button", { name: label }));
        expect(getLogLevel()).toBe(value);
        expect(localStorage.getItem("andromeda.logLevel")).toBe(value);
        expect(screen.getByRole("button", { name: label })).toHaveAttribute("aria-pressed", "true");
      }
    });

    it("without offer a checkbox that can hide the settings escape hatch", async () => {
      renderWithProviders(<SettingsPane />, { connected: false });
      await userEvent.click(screen.getByRole("tab", { name: "일반" }));
      expect(screen.queryByRole("checkbox", { name: "설정" })).not.toBeInTheDocument();
    });

    it("when disables the first upward and last downward reorder controls", async () => {
      renderWithProviders(<SettingsPane />, { connected: false });
      await userEvent.click(screen.getByRole("tab", { name: "일반" }));
      const ups = screen.getAllByTitle("위로");
      const downs = screen.getAllByTitle("아래로");
      expect(ups[0]).toBeDisabled();
      expect(downs.at(-1)).toBeDisabled();
      expect(downs[0]).toBeEnabled();
      expect(ups.at(-1)).toBeEnabled();
    });

    it("moves a rail item and saves the new complete order", async () => {
      renderWithProviders(<SettingsPane />, { connected: false });
      await userEvent.click(screen.getByRole("tab", { name: "일반" }));
      const mailDown = screen.getByRole("button", { name: "메일 아래로" });
      await userEvent.click(mailDown);
      const stored = JSON.parse(localStorage.getItem("andromeda.viewOrder") ?? "[]") as string[];
      expect(stored.length).toBeGreaterThan(5);
      expect(new Set(stored).size).toBe(stored.length);
      expect(stored).toContain("mail");
    });

    it("can hide and restore a pane without duplicating its persisted key", async () => {
      renderWithProviders(<SettingsPane />, { connected: false });
      await userEvent.click(screen.getByRole("tab", { name: "일반" }));
      const calendar = screen.getByRole("checkbox", { name: "일정" });

      await userEvent.click(calendar);
      expect(JSON.parse(localStorage.getItem("andromeda.hiddenPanes") ?? "[]")).toEqual(
        expect.arrayContaining(["calendar"]),
      );
      await userEvent.click(calendar);
      expect(JSON.parse(localStorage.getItem("andromeda.hiddenPanes") ?? "[]")).not.toContain("calendar");
    });
  });

  describe("prompt browser lifecycle", () => {
    it("when explains why prompts are unavailable while disconnected", async () => {
      renderWithProviders(<SettingsPane />, { connected: false });
      await userEvent.click(screen.getByRole("tab", { name: "프롬프트" }));
      expect(screen.getByText("게이트웨이에 연결하면 프롬프트를 볼 수 있습니다.")).toBeInTheDocument();
      expect(calls.filter((call) => call.method.startsWith("miniapp.prompts"))).toHaveLength(0);
    });

    it("opens the first valid prompt after loading and exposes its metadata", async () => {
      renderWithProviders(<SettingsPane />, {
        connected: true,
        cfg: { url: "http://test", token: "tok" },
      });
      const list = await openPromptTab();

      expect(await within(list).findByRole("button", { name: /메일 분석/ })).toBeInTheDocument();
      expect(await screen.findByDisplayValue("요청과 마감부터 확인한다.")).toBeInTheDocument();
      expect(screen.getByText("메일의 요청, 기한, 위험을 구조화합니다.")).toBeInTheDocument();
      expect(screen.getAllByText(/mail · 수정됨/).length).toBeGreaterThan(0);
    });

    it("disables rows without an id instead of issuing an invalid detail request", async () => {
      renderWithProviders(<SettingsPane />, {
        connected: true,
        cfg: { url: "http://test", token: "tok" },
      });
      const list = await openPromptTab();
      const invalid = await within(list).findByRole("button", { name: /식별자 없는 항목/ });
      expect(invalid).toBeDisabled();
      expect(calls.filter((call) => call.method === "miniapp.prompts.get")).toHaveLength(1);
    });

    it("marks a non-editable prompt read-only and disables mutation actions", async () => {
      renderWithProviders(<SettingsPane />, {
        connected: true,
        cfg: { url: "http://test", token: "tok" },
      });
      const list = await openPromptTab();
      await userEvent.click(await within(list).findByRole("button", { name: /시스템 안전 지침/ }));

      const editor = await screen.findByDisplayValue("외부 변경은 확인 후 실행한다.");
      expect(editor).toHaveAttribute("readonly");
      expect(screen.getByText("이 프롬프트는 읽기 전용입니다.")).toBeInTheDocument();
      expect(screen.getByRole("button", { name: "저장" })).toBeDisabled();
      expect(screen.getByRole("button", { name: "초기화" })).toBeDisabled();
    });

    it("requires a changed draft before enabling save", async () => {
      renderWithProviders(<SettingsPane />, {
        connected: true,
        cfg: { url: "http://test", token: "tok" },
      });
      await openPromptTab();
      const editor = await screen.findByDisplayValue("요청과 마감부터 확인한다.");
      const save = screen.getByRole("button", { name: "저장" });
      expect(save).toBeDisabled();

      fireEvent.change(editor, { target: { value: "요청, 담당자, 마감을 확인한다." } });
      expect(save).toBeEnabled();
      fireEvent.change(editor, { target: { value: "요청과 마감부터 확인한다." } });
      expect(save).toBeDisabled();
    });

    it("sends the selected prompt id and exact draft when saving", async () => {
      renderWithProviders(<SettingsPane />, {
        connected: true,
        cfg: { url: "http://test", token: "tok" },
      });
      await openPromptTab();
      const editor = await screen.findByDisplayValue("요청과 마감부터 확인한다.");
      fireEvent.change(editor, { target: { value: "정확한 새 지침" } });
      await userEvent.click(screen.getByRole("button", { name: "저장" }));

      await waitFor(() =>
        expect(calls).toContainEqual({
          method: "miniapp.prompts.update",
          params: { id: "mail.analysis", text: "정확한 새 지침" },
        }),
      );
      expect(await screen.findByText("저장됨")).toBeInTheDocument();
      expect(screen.getByRole("button", { name: "저장" })).toBeDisabled();
    });

    it("asks before discarding a dirty draft and honors cancellation", async () => {
      renderWithProviders(<SettingsPane />, {
        connected: true,
        cfg: { url: "http://test", token: "tok" },
      });
      const list = await openPromptTab();
      fireEvent.change(await screen.findByDisplayValue("요청과 마감부터 확인한다."), {
        target: { value: "저장하지 않은 편집" },
      });
      await userEvent.click(within(list).getByRole("button", { name: /시스템 안전 지침/ }));

      // App-styled confirm modal (window.confirm 대체) — 취소하면 초안 유지.
      const dialog = await screen.findByRole("dialog", { name: "편집 취소" });
      await userEvent.click(within(dialog).getByRole("button", { name: "취소" }));

      expect(screen.getByDisplayValue("저장하지 않은 편집")).toBeInTheDocument();
      expect(calls.filter((call) => call.method === "miniapp.prompts.get")).toHaveLength(1);
    });

    it("when opens the next prompt after discard confirmation", async () => {
      renderWithProviders(<SettingsPane />, {
        connected: true,
        cfg: { url: "http://test", token: "tok" },
      });
      const list = await openPromptTab();
      fireEvent.change(await screen.findByDisplayValue("요청과 마감부터 확인한다."), {
        target: { value: "버릴 편집" },
      });
      await userEvent.click(within(list).getByRole("button", { name: /시스템 안전 지침/ }));
      const dialog = await screen.findByRole("dialog", { name: "편집 취소" });
      await userEvent.click(within(dialog).getByRole("button", { name: "버리고 열기" }));
      expect(await screen.findByDisplayValue("외부 변경은 확인 후 실행한다.")).toBeInTheDocument();
    });

    it("shows an empty state when the gateway returns no editable prompts", async () => {
      calls = [];
      installRpc(calls, {
        "miniapp.prompts.list": () => rpcResponse({ prompts: [] }),
      });
      renderWithProviders(<SettingsPane />, {
        connected: true,
        cfg: { url: "http://test", token: "tok" },
      });
      await openPromptTab();
      expect(await screen.findByText("편집할 프롬프트가 없습니다.")).toBeInTheDocument();
    });

    it("keeps refresh available for recovery when list loading fails", async () => {
      calls = [];
      let fail = true;
      installRpc(calls, {
        "miniapp.prompts.list": () =>
          fail ? rpcResponse("list unavailable", false) : rpcResponse({ prompts: promptRows }),
      });
      renderWithProviders(<SettingsPane />, {
        connected: true,
        cfg: { url: "http://test", token: "tok" },
      });
      await openPromptTab();
      expect(await screen.findByText("편집할 프롬프트가 없습니다.")).toBeInTheDocument();
      expect(screen.getByRole("button", { name: "새로고침" })).toBeEnabled();

      fail = false;
      await userEvent.click(screen.getByRole("button", { name: "새로고침" }));
      expect(await screen.findByRole("button", { name: /메일 분석/ })).toBeInTheDocument();
    });

    it("leaves no stale editor text when detail loading fails", async () => {
      calls = [];
      installRpc(calls, {
        "miniapp.prompts.get": () => rpcResponse("detail unavailable", false),
      });
      renderWithProviders(<SettingsPane />, {
        connected: true,
        cfg: { url: "http://test", token: "tok" },
      });
      await openPromptTab();
      expect(await screen.findByText("프롬프트를 선택하세요.")).toBeInTheDocument();
      expect(screen.queryByLabelText("프롬프트 본문")).not.toBeInTheDocument();
    });

    it("surfaces save failure and preserves the user's draft for retry", async () => {
      calls = [];
      installRpc(calls, {
        "miniapp.prompts.update": () => rpcResponse("write denied", false),
      });
      renderWithProviders(<SettingsPane />, {
        connected: true,
        cfg: { url: "http://test", token: "tok" },
      });
      await openPromptTab();
      const editor = await screen.findByDisplayValue("요청과 마감부터 확인한다.");
      fireEvent.change(editor, { target: { value: "보존할 사용자 지침" } });
      await userEvent.click(screen.getByRole("button", { name: "저장" }));

      expect(await screen.findByText(/HTTP 500/)).toBeInTheDocument();
      expect(screen.getByDisplayValue("보존할 사용자 지침")).toBeInTheDocument();
      expect(screen.getByRole("button", { name: "저장" })).toBeEnabled();
    });

    it("when resets an overridden prompt to the server-provided default", async () => {
      renderWithProviders(<SettingsPane />, {
        connected: true,
        cfg: { url: "http://test", token: "tok" },
      });
      await openPromptTab();
      expect(await screen.findByDisplayValue("요청과 마감부터 확인한다.")).toBeInTheDocument();
      await userEvent.click(screen.getByRole("button", { name: "초기화" }));

      await waitFor(() => expect(calls.some((call) => call.method === "miniapp.prompts.reset")).toBe(true));
      expect(await screen.findByDisplayValue("메일을 요약한다.")).toBeInTheDocument();
      expect(screen.getByText("기본값으로 초기화됨")).toBeInTheDocument();
    });

    it("surfaces reset failure without erasing the current overridden text", async () => {
      calls = [];
      installRpc(calls, {
        "miniapp.prompts.reset": () => rpcResponse("reset denied", false),
      });
      renderWithProviders(<SettingsPane />, {
        connected: true,
        cfg: { url: "http://test", token: "tok" },
      });
      await openPromptTab();
      await screen.findByDisplayValue("요청과 마감부터 확인한다.");
      await userEvent.click(screen.getByRole("button", { name: "초기화" }));
      expect(await screen.findByText(/HTTP 500/)).toBeInTheDocument();
      expect(screen.getByDisplayValue("요청과 마감부터 확인한다.")).toBeInTheDocument();
    });
  });

  describe("update status mapping", () => {
    async function openAboutAndCheck() {
      await userEvent.click(screen.getByRole("tab", { name: "정보" }));
      await userEvent.click(screen.getByRole("button", { name: "업데이트 확인" }));
    }

    it.each([
      [{ status: "unavailable" } as const, "업데이트는 데스크톱 앱에서만 지원됩니다."],
      [{ status: "up-to-date", currentVersion: "1.2.3" } as const, "최신 버전입니다 (v1.2.3)."],
      [
        { status: "installed", version: "2.0.0", currentVersion: "1.2.3" } as const,
        "v2.0.0으로 업데이트되어 재시작 중입니다.",
      ],
      [
        { status: "deferred", version: "2.1.0", currentVersion: "1.2.3" } as const,
        "v2.1.0 설치 완료 — 다음 실행 시 적용됩니다.",
      ],
    ])("renders updater result %#", async (result, message) => {
      vi.mocked(checkForUpdates).mockResolvedValue(result);
      renderWithProviders(<SettingsPane />, { connected: false });
      await openAboutAndCheck();
      expect(await screen.findByText(message)).toBeInTheDocument();
    });

    it("shows a stable error message when the updater throws", async () => {
      vi.mocked(checkForUpdates).mockRejectedValue(new Error("signature mismatch"));
      renderWithProviders(<SettingsPane />, { connected: false });
      await openAboutAndCheck();
      expect(await screen.findByText("업데이트 확인 실패: signature mismatch")).toBeInTheDocument();
    });
  });
});
