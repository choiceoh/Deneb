import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { fakeProvider, renderWithProviders } from "@/test/util";
import { WorkfeedPane } from "./WorkfeedPane";

// The list flows through the (fake) data provider; the action RPCs go straight to
// callRpc → fetch. Stub fetch to capture the last RPC method + params.
let lastRpc: { method: string; params: Record<string, unknown> } | null;

beforeEach(() => {
  lastRpc = null;
  if (!globalThis.crypto?.randomUUID) vi.stubGlobal("crypto", { randomUUID: () => "test-uuid" });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url: string, init?: RequestInit) => {
      const body = JSON.parse(String(init?.body ?? "{}")) as { method: string; params: Record<string, unknown> };
      lastRpc = { method: body.method, params: body.params };
      return { ok: true, json: async () => ({ ok: true, payload: { ok: true } }) } as unknown as Response;
    }),
  );
});
afterEach(() => {
  localStorage.clear();
  vi.unstubAllGlobals();
});

describe("WorkfeedPane", () => {
  it("answers a question item via workfeed.feedback", async () => {
    const dataProvider = fakeProvider({
      workfeed: [{ id: "w1", source: "deal_question", title: "검토 요청", body: "승인 여부를 알려주세요." }],
    });
    renderWithProviders(<WorkfeedPane />, { connected: true, dataProvider });

    const box = await screen.findByPlaceholderText("답변 입력…");
    await userEvent.type(box, "승인합니다");
    await userEvent.click(screen.getByRole("button", { name: "답변" }));

    await waitFor(() => expect(lastRpc).toBeTruthy());
    expect(lastRpc?.method).toBe("miniapp.workfeed.feedback");
    expect(lastRpc?.params).toMatchObject({ itemId: "w1", feedback: "승인합니다" });
  });

  it("regenerates a card via workfeed.rewrite", async () => {
    const dataProvider = fakeProvider({
      workfeed: [{ id: "w2", source: "followup", title: "미답장 메일 3건" }],
    });
    renderWithProviders(<WorkfeedPane />, { connected: true, dataProvider });

    await userEvent.click(await screen.findByRole("button", { name: "다시 작성" }));

    await waitFor(() => expect(lastRpc).toBeTruthy());
    expect(lastRpc?.method).toBe("miniapp.workfeed.rewrite");
    expect(lastRpc?.params).toMatchObject({ itemId: "w2" });
  });

  it("reveals a correction box behind 정정 for non-question items", async () => {
    const dataProvider = fakeProvider({
      workfeed: [{ id: "w2", source: "followup", title: "미답장 메일 3건" }],
    });
    renderWithProviders(<WorkfeedPane />, { connected: true, dataProvider });

    // A non-question item shows no free-text box until 정정 is toggled.
    expect(await screen.findByText("미답장 메일 3건")).toBeInTheDocument();
    expect(screen.queryByPlaceholderText("답변 입력…")).not.toBeInTheDocument();
    expect(screen.queryByPlaceholderText("정정·피드백 입력…")).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "정정" }));
    const box = await screen.findByPlaceholderText("정정·피드백 입력…");
    await userEvent.type(box, "이미 답장함");
    await userEvent.click(screen.getByRole("button", { name: "보내기" }));

    await waitFor(() => expect(lastRpc).toBeTruthy());
    expect(lastRpc?.method).toBe("miniapp.workfeed.feedback");
    expect(lastRpc?.params).toMatchObject({ itemId: "w2", feedback: "이미 답장함" });
  });
});
