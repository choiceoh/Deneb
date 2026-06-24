import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ProactiveList } from "./ProactivePanel";
import type { ProactiveEvent } from "@/events";

const NOW = 1_700_000_000_000;
const ev = (over: Partial<ProactiveEvent>): ProactiveEvent => ({ id: "e1", raw: {}, ...over });

describe("ProactiveList", () => {
  it("renders nothing when there are no events (stays out of the way when quiet)", () => {
    const { container } = render(<ProactiveList events={[]} onDismiss={() => {}} onClearAll={() => {}} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("shows the count, title, body, and a relative receipt time", () => {
    const events = [ev({ id: "e1", title: "회의 10분 전", body: "분기 리뷰 준비", ts: NOW - 5 * 60_000 })];
    render(<ProactiveList events={events} onDismiss={() => {}} onClearAll={() => {}} now={NOW} />);
    expect(screen.getByText("알림 1")).toBeInTheDocument();
    expect(screen.getByText("회의 10분 전")).toBeInTheDocument();
    expect(screen.getByText("분기 리뷰 준비")).toBeInTheDocument();
    expect(screen.getByText("5분 전")).toBeInTheDocument();
  });

  it("dismisses one nudge and clears all", async () => {
    const onDismiss = vi.fn();
    const onClearAll = vi.fn();
    const events = [ev({ id: "e1", title: "n1", ts: NOW }), ev({ id: "e2", title: "n2", ts: NOW })];
    render(<ProactiveList events={events} onDismiss={onDismiss} onClearAll={onClearAll} now={NOW} />);

    await userEvent.click(screen.getAllByRole("button", { name: "닫기" })[0]);
    expect(onDismiss).toHaveBeenCalledWith("e1");

    await userEvent.click(screen.getByRole("button", { name: "모두 지우기" }));
    expect(onClearAll).toHaveBeenCalledTimes(1);
  });
});
