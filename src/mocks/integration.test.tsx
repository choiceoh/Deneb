import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { App } from "@/App";
import { server } from "./server";

// End-to-end through the REAL stack: gateway.ts → denebDataProvider → Refine →
// pane, with only the network mocked. This is the closest we get to a live
// gateway, and it exercises code that fakeProvider-based tests bypass.
beforeAll(() => server.listen({ onUnhandledRequest: "warn" }));
afterEach(() => {
  server.resetHandlers();
  localStorage.clear();
});
afterAll(() => server.close());

beforeEach(() => {
  localStorage.setItem("andromeda.gateway", JSON.stringify({ url: "http://mock.local", token: "mock" }));
});

describe("App against the mock gateway (real stack)", () => {
  it("loads todos from the gateway through the real data provider", async () => {
    render(<App />);
    // The landing view is now the 오늘 dashboard; open the 할일 pane to assert the
    // full row (scope the click to the sidebar nav — the dashboard has a 할일 card too).
    await userEvent.click(within(screen.getByRole("navigation")).getByRole("button", { name: /할일/ }));
    expect(await screen.findByText("분기 보고서 초안 작성")).toBeInTheDocument();
  });

  it("switches to the mail pane and shows mock mail", async () => {
    render(<App />);
    // Scope to the sidebar nav: the dashboard also renders a 메일 card button.
    await userEvent.click(within(screen.getByRole("navigation")).getByRole("button", { name: /메일/ }));
    expect(await screen.findByText("분기 리뷰 일정 확정")).toBeInTheDocument();
  });

  it("opens a mail row and reads the message body", async () => {
    render(<App />);
    await userEvent.click(within(screen.getByRole("navigation")).getByRole("button", { name: /메일/ }));
    await userEvent.click(await screen.findByText("분기 리뷰 일정 확정"));

    const detail = screen.getByLabelText("메일 상세");
    expect(await within(detail).findByText(/분기 리뷰 일정을 확정합니다/)).toBeInTheDocument();
    expect(within(detail).getByText(/회의 전까지 초안 자료를 공유/)).toBeInTheDocument();
  });
});
