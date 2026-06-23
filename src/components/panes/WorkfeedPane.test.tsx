import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { fakeProvider, renderWithProviders } from "@/test/util";
import { WorkfeedPane } from "./WorkfeedPane";

// The list flows through the (fake) data provider; the answer action goes straight
// to callRpc → fetch. Stub fetch to capture the answer RPC params.
let answerParams: Record<string, unknown> | null;

beforeEach(() => {
  answerParams = null;
  if (!globalThis.crypto?.randomUUID) vi.stubGlobal("crypto", { randomUUID: () => "test-uuid" });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url: string, init?: RequestInit) => {
      const { method, params } = JSON.parse(String(init?.body ?? "{}")) as {
        method: string;
        params: Record<string, unknown>;
      };
      if (method === "miniapp.workfeed.answer") answerParams = params;
      return { ok: true, json: async () => ({ ok: true, payload: { ok: true } }) } as unknown as Response;
    }),
  );
});
afterEach(() => vi.unstubAllGlobals());

describe("WorkfeedPane", () => {
  it("answers a question item via workfeed.answer", async () => {
    const dataProvider = fakeProvider({
      workfeed: [{ id: "w1", source: "deal_question", title: "검토 요청", body: "승인 여부를 알려주세요." }],
    });
    renderWithProviders(<WorkfeedPane />, { connected: true, dataProvider });

    const box = await screen.findByPlaceholderText("답변 입력…");
    await userEvent.type(box, "승인합니다");
    await userEvent.click(screen.getByRole("button", { name: "답변" }));

    await waitFor(() => expect(answerParams).toBeTruthy());
    expect(answerParams).toMatchObject({ itemId: "w1", text: "승인합니다" });
  });

  it("shows no answer box for non-question items", async () => {
    const dataProvider = fakeProvider({
      workfeed: [{ id: "w2", source: "followup", title: "미답장 메일 3건" }],
    });
    renderWithProviders(<WorkfeedPane />, { connected: true, dataProvider });
    expect(await screen.findByText("미답장 메일 3건")).toBeInTheDocument();
    expect(screen.queryByPlaceholderText("답변 입력…")).not.toBeInTheDocument();
  });
});
