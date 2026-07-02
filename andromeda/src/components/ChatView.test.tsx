import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
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

  it("infers document MIME type when the browser omits File.type", async () => {
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
              : method === "miniapp.capture.document"
                ? { text: "ok" }
                : {};
        return new Response(JSON.stringify({ ok: true, payload }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }),
    );

    renderWithProviders(<ChatView cfg={{ url: "http://test", token: "tok" }} />, { connected: true });
    const input = document.querySelector('input[type="file"]') as HTMLInputElement;
    await userEvent.upload(input, new File(["fake pdf"], "contract.pdf", { type: "" }));

    await waitFor(() => expect(rpcCalls.some((c) => c.method === "miniapp.capture.document")).toBe(true));
    const capture = rpcCalls.find((c) => c.method === "miniapp.capture.document");
    expect(capture?.params).toMatchObject({
      filename: "contract.pdf",
      mimeType: "application/pdf",
      sessionKey: "chat:main",
    });
  });

  it("drops a file anywhere on the chat column to attach", async () => {
    const rpcCalls: Array<{ method: string; params: Record<string, unknown> }> = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_url: string, init?: RequestInit) => {
        const body = JSON.parse(String(init?.body ?? "{}")) as {
          method?: string;
          params?: Record<string, unknown>;
        };
        const method = String(body.method ?? "");
        rpcCalls.push({ method, params: body.params ?? {} });
        const payload =
          method === "miniapp.models.list"
            ? { current: "", sections: [] }
            : method === "miniapp.sessions.recent"
              ? { sessions: [], count: 0 }
              : method === "miniapp.capture.document"
                ? { text: "ok" }
                : {};
        return new Response(JSON.stringify({ ok: true, payload }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }),
    );

    renderWithProviders(<ChatView cfg={{ url: "http://test", token: "tok" }} />, { connected: true });

    // The whole chat column is the drop zone; the ring shows only mid-drag.
    const zone = screen.getByRole("main");
    const dt = { files: [new File(["fake pdf"], "contract.pdf", { type: "application/pdf" })], types: ["Files"] };
    expect(zone).not.toHaveClass("drop-over");
    fireEvent.dragEnter(zone, { dataTransfer: dt });
    expect(zone).toHaveClass("drop-over");
    fireEvent.drop(zone, { dataTransfer: dt });
    expect(zone).not.toHaveClass("drop-over");

    await waitFor(() => expect(rpcCalls.some((c) => c.method === "miniapp.capture.document")).toBe(true));
    expect(rpcCalls.find((c) => c.method === "miniapp.capture.document")?.params).toMatchObject({
      filename: "contract.pdf",
      mimeType: "application/pdf",
      sessionKey: "chat:main",
    });
  });

  it("routes extension-inferred audio attachments without sending typed text as a caption", async () => {
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
              : method === "miniapp.capture.audio"
                ? { text: "전사 완료" }
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
    await user.type(composer, "이 녹음 요약해줘");
    const input = document.querySelector('input[type="file"]') as HTMLInputElement;
    await user.upload(input, new File(["fake audio"], "meeting.mp3", { type: "" }));

    await waitFor(() => expect(rpcCalls.some((c) => c.method === "miniapp.capture.audio")).toBe(true));
    const capture = rpcCalls.find((c) => c.method === "miniapp.capture.audio");
    expect(capture?.params).toMatchObject({
      mimeType: "audio/mpeg",
      sessionKey: "chat:main",
    });
    expect(capture?.params).not.toHaveProperty("caption");
    expect(composer).toHaveValue("이 녹음 요약해줘");
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
    const result = await screen.findByRole("group", { name: "첨부 분석 결과" });
    expect(result).toBeInTheDocument();
    expect(within(result).getByText("이미지 분석")).toBeInTheDocument();
    expect(within(result).getByText("quote.png")).toBeInTheDocument();
    expect(within(result).getByText("image/png")).toBeInTheDocument();
    expect(within(result).getByText("이 이미지에서 금액만 찾아줘")).toBeInTheDocument();
    expect(within(result).getByText("분석 완료")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "다시 생성" })).not.toBeInTheDocument();
  });
});
