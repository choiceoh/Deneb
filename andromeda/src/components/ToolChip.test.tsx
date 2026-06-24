import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { ToolChip } from "./ToolChip";

describe("ToolChip", () => {
  it("humanizes the tool id and shows its detail", () => {
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
});
