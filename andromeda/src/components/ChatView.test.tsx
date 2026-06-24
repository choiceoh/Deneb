import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { ChatView } from "./ChatView";
import { renderWithProviders } from "@/test/util";

beforeEach(() => {
  localStorage.clear();
  // ChatView loads models + recent sessions on connect; keep tests offline.
  vi.stubGlobal(
    "fetch",
    vi.fn(() => Promise.reject(new Error("offline test"))),
  );
});
afterEach(() => {
  vi.unstubAllGlobals();
});

describe("ChatView (비업무 채팅 탭)", () => {
  it("greets and offers a composer when connected with no messages", () => {
    renderWithProviders(<ChatView cfg={{ url: "http://test", token: "tok" }} />, { connected: true });

    // non-work greeting (mirrors the native chatbot mode)
    expect(screen.getByText("안녕하세요? 무슨 대화를 할까요?")).toBeInTheDocument();
    // composer with the native placeholder
    expect(screen.getByRole("textbox", { name: "Deneb에게 메시지" })).toHaveAttribute(
      "placeholder",
      "질문을 입력하세요",
    );
    // its own conversation-history column lives to the right
    expect(screen.getByRole("group", { name: "대화 기록" })).toBeInTheDocument();
  });

  it("shows the connection prompt when disconnected", () => {
    renderWithProviders(<ChatView cfg={{ url: "", token: "" }} />, { connected: false });
    expect(screen.getByText("게이트웨이 연결 대기 중")).toBeInTheDocument();
  });

  it("focuses the composer when shown so you can type right away", () => {
    renderWithProviders(<ChatView cfg={{ url: "http://test", token: "tok" }} />, { connected: true });
    expect(screen.getByRole("textbox", { name: "Deneb에게 메시지" })).toHaveFocus();
  });

  it("offers a file-attach button (image OCR · document · audio)", () => {
    renderWithProviders(<ChatView cfg={{ url: "http://test", token: "tok" }} />, { connected: true });
    expect(screen.getByRole("button", { name: "파일 첨부" })).toBeInTheDocument();
  });

  it("sends the typed composer text as the image attachment caption", async () => {
    const rpcCalls: Array<{ method: string; params: Record<string, unknown> }> = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_url: string, init?: RequestInit) => {
        const body = JSON.parse(String(init?.body ?? "{}")) as {
          method?: string;
          params?: Record<string, unknown>;
        };
        const method = String(body.method ?? "");
        const params = body.params ?? {};
        rpcCalls.push({ method, params });
        const payload =
          method === "miniapp.models.list"
            ? { current: "", sections: [] }
            : method === "miniapp.sessions.recent"
              ? { sessions: [], count: 0 }
              : method === "miniapp.capture.image"
                ? { text: "분석 완료" }
                : {};
        return new Response(JSON.stringify({ ok: true, payload }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }),
    );

    renderWithProviders(<ChatView cfg={{ url: "http://test", token: "tok" }} />, { connected: true });
    const user = userEvent.setup();
    const composer = screen.getByRole("textbox", { name: "Deneb에게 메시지" });
    await user.type(composer, "이 이미지에서 금액만 찾아줘");
    const input = document.querySelector('input[type="file"]') as HTMLInputElement;
    await user.upload(input, new File(["fake image"], "quote.png", { type: "image/png" }));

    await waitFor(() => expect(rpcCalls.some((c) => c.method === "miniapp.capture.image")).toBe(true));
    const capture = rpcCalls.find((c) => c.method === "miniapp.capture.image");
    expect(capture?.params).toMatchObject({
      mimeType: "image/png",
      sessionKey: "chat:main",
      caption: "이 이미지에서 금액만 찾아줘",
    });
    expect(typeof capture?.params.image).toBe("string");
    expect(composer).toHaveValue("");
    expect(await screen.findByText("분석 완료")).toBeInTheDocument();
  });
});
