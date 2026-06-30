import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import type { GatewayConfig } from "@/gateway";
import { renderWithProviders } from "@/test/util";
import type { CodeSession } from "@/types";
import { CodeTaskDetail } from "./CodeTaskDetail";

// codePr runs on mount; codeClose fires on the "세션 닫기" button.
const { codeCloseMock, codePrMock } = vi.hoisted(() => ({ codeCloseMock: vi.fn(), codePrMock: vi.fn() }));
vi.mock("@/gateway", async (orig) => ({
  ...(await orig<typeof import("@/gateway")>()),
  codePr: codePrMock,
  codeClose: codeCloseMock,
}));

const cfg: GatewayConfig = { url: "http://test", token: "tok" };
const session = {
  id: "t1",
  title: "로그인 폼",
  status: "passed",
  repo: { owner: "acme", name: "app" },
  branch: "deneb/t1",
} as CodeSession;

afterEach(() => vi.clearAllMocks());

describe("CodeTaskDetail — 세션 닫기", () => {
  it("closes the session via codeClose(cfg,id) and refreshes the list", async () => {
    codePrMock.mockResolvedValue("");
    codeCloseMock.mockResolvedValue(true);
    const onChange = vi.fn();
    renderWithProviders(<CodeTaskDetail session={session} cfg={cfg} onChange={onChange} />, { connected: true, cfg });

    await userEvent.click(screen.getByRole("button", { name: "세션 닫기" }));

    await waitFor(() => expect(codeCloseMock).toHaveBeenCalledWith(cfg, "t1"));
    expect(onChange).toHaveBeenCalled();
  });
});
