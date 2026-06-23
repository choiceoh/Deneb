import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";

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
});
