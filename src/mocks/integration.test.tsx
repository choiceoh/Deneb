import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
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
    expect(await screen.findByText("분기 보고서 초안 작성")).toBeInTheDocument();
  });

  it("switches to the mail pane and shows mock mail", async () => {
    render(<App />);
    await userEvent.click(screen.getByRole("button", { name: /메일/ }));
    expect(await screen.findByText("분기 리뷰 일정 확정")).toBeInTheDocument();
  });
});
