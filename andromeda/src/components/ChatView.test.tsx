import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
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

describe("ChatView (업무 채팅 탭)", () => {
  it("greets and offers a composer when connected with no messages", () => {
    renderWithProviders(<ChatView cfg={{ url: "http://test", token: "tok" }} />, { connected: true });

    // personalized time-of-day greeting (mirrors the native 업무 EmptyState)
    expect(screen.getByText(/^선택님, /)).toBeInTheDocument();
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

  it("when offers a file-attach button (image OCR · document · audio)", () => {
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
              : method === "miniapp.capture.batch"
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
    await screen.findByRole("group", { name: "첨부 대기 파일" });
    fireEvent.click(screen.getByRole("button", { name: "전송" }));

    await waitFor(() => expect(rpcCalls.some((c) => c.method === "miniapp.capture.batch")).toBe(true));
    const capture = rpcCalls.find((c) => c.method === "miniapp.capture.batch");
    expect(capture?.params).toMatchObject({
      files: [expect.objectContaining({ filename: "contract.pdf", mimeType: "application/pdf" })],
      sessionKey: "client:main",
    });
  });

  it("when drops a file anywhere on the chat column to attach", async () => {
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
              : method === "miniapp.capture.batch"
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

    await screen.findByRole("group", { name: "첨부 대기 파일" });
    fireEvent.click(screen.getByRole("button", { name: "전송" }));

    await waitFor(() => expect(rpcCalls.some((c) => c.method === "miniapp.capture.batch")).toBe(true));
    expect(rpcCalls.find((c) => c.method === "miniapp.capture.batch")?.params).toMatchObject({
      files: [expect.objectContaining({ filename: "contract.pdf", mimeType: "application/pdf" })],
      sessionKey: "client:main",
    });
  });

  it("when pastes a clipboard image as an attachment (client:main)", async () => {
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
              : method === "miniapp.capture.batch"
                ? { text: "ok" }
                : {};
        return new Response(JSON.stringify({ ok: true, payload }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }),
    );

    renderWithProviders(<ChatView cfg={{ url: "http://test", token: "tok" }} />, { connected: true });

    const composer = screen.getByRole("textbox", { name: "Deneb에게 메시지" });
    fireEvent.paste(composer, {
      clipboardData: { files: [new File(["x"], "shot.png", { type: "image/png" })] },
    });

    await screen.findByRole("group", { name: "첨부 대기 파일" });
    fireEvent.click(screen.getByRole("button", { name: "전송" }));

    await waitFor(() => expect(rpcCalls.some((c) => c.method === "miniapp.capture.batch")).toBe(true));
    expect(rpcCalls.find((c) => c.method === "miniapp.capture.batch")?.params).toMatchObject({
      files: [expect.objectContaining({ mimeType: "image/png" })],
      sessionKey: "client:main",
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
              : method === "miniapp.capture.batch"
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
    await screen.findByRole("group", { name: "첨부 대기 파일" });
    await user.click(screen.getByRole("button", { name: "전송" }));

    await waitFor(() => expect(rpcCalls.some((c) => c.method === "miniapp.capture.batch")).toBe(true));
    const capture = rpcCalls.find((c) => c.method === "miniapp.capture.batch");
    expect(capture?.params).toMatchObject({
      files: [expect.objectContaining({ mimeType: "audio/mpeg" })],
      sessionKey: "client:main",
    });
    expect(capture?.params).not.toHaveProperty("caption");
    expect(composer).toHaveValue("이 녹음 요약해줘");
  });

  it("when sends the typed composer text as the image attachment caption", async () => {
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
              : method === "miniapp.capture.batch"
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
    await screen.findByRole("group", { name: "첨부 대기 파일" });
    await user.click(screen.getByRole("button", { name: "전송" }));

    await waitFor(() => expect(rpcCalls.some((c) => c.method === "miniapp.capture.batch")).toBe(true));
    const capture = rpcCalls.find((c) => c.method === "miniapp.capture.batch");
    expect(capture?.params).toMatchObject({
      files: [expect.objectContaining({ mimeType: "image/png" })],
      sessionKey: "client:main",
      caption: "이 이미지에서 금액만 찾아줘",
    });
    expect(typeof (capture?.params.files as { data: string }[])[0].data).toBe("string");
    expect(composer).toHaveValue("");
    // The batch lands as one turn: the assistant analysis renders, and the user turn
    // lists the attached file (its name/mime are asserted on the RPC params above).
    expect(await screen.findByText("분석 완료")).toBeInTheDocument();
    expect(screen.getByText((t) => t.includes("quote.png"))).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "다시 생성" })).not.toBeInTheDocument();
  });
});
