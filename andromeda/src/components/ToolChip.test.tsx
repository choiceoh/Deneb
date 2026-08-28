import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { ToolChip } from "./ToolChip";

describe("ToolChip", () => {
  it("humanizes the tool id and displays its detail", () => {
    render(
      <ToolChip part={{ kind: "tool", id: "t1", tool: "gmail.list_recent", state: "completed", detail: "메일 3건" }} />,
    );
    expect(screen.getByText("gmail list recent")).toBeInTheDocument();
    expect(screen.getByText("메일 3건")).toBeInTheDocument();
  });

  it("exposes running / done / error state to assistive tech", () => {
    const { rerender } = render(<ToolChip part={{ kind: "tool", id: "t1", tool: "x", state: "started" }} />);
    expect(screen.getByRole("img", { name: "실행 중" })).toBeInTheDocument();

    rerender(<ToolChip part={{ kind: "tool", id: "t1", tool: "x", state: "completed" }} />);
    expect(screen.getByRole("img", { name: "완료" })).toBeInTheDocument();

    rerender(<ToolChip part={{ kind: "tool", id: "t1", tool: "x", state: "completed", isError: true }} />);
    expect(screen.getByRole("img", { name: "실패" })).toBeInTheDocument();
  });

  it("stays a plain row without a preview and becomes a disclosure with one", () => {
    const base = { kind: "tool", id: "t1", tool: "exec", state: "completed" } as const;
    const { container, rerender } = render(<ToolChip part={{ ...base, resultSummary: "합계 60 · 8줄" }} />);
    expect(screen.getByText("합계 60 · 8줄")).toBeInTheDocument();
    expect(container.querySelector("details")).toBeNull();

    rerender(<ToolChip part={{ ...base, resultSummary: "합계 60 · 8줄", resultPreview: "a.ts\nb.ts" }} />);
    const details = container.querySelector("details");
    expect(details).not.toBeNull();
    // Closed by default — the preview is opt-in, not noise in the transcript.
    expect(details).not.toHaveAttribute("open");
    expect(screen.getByText(/a\.ts/)).toBeInTheDocument();
  });
});
