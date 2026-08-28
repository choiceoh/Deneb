import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { ToolChip, previewLineClass } from "./ToolChip";

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

  it("tints diff lines and offers copy in the expanded body", () => {
    const part = {
      kind: "tool",
      id: "t1",
      tool: "apply_patch",
      state: "completed",
      resultPreview: "@@ -1,2 +1,2 @@\n-old line\n+new line\n context",
    } as const;
    const { container } = render(<ToolChip part={part} />);
    expect(container.querySelector(".pv-hunk")?.textContent).toContain("@@");
    expect(container.querySelector(".pv-add")?.textContent).toContain("+new line");
    expect(container.querySelector(".pv-del")?.textContent).toContain("-old line");
    expect(screen.getByRole("button", { name: "복사" })).toBeInTheDocument();
  });

  it("classifies preview lines without over-matching diff headers", () => {
    expect(previewLineClass("+added")).toBe("add");
    expect(previewLineClass("-removed")).toBe("del");
    expect(previewLineClass("@@ -1 +1 @@")).toBe("hunk");
    // File headers are not content lines.
    expect(previewLineClass("+++ b/file.ts")).toBe("");
    expect(previewLineClass("--- a/file.ts")).toBe("");
    expect(previewLineClass("plain")).toBe("");
  });
});
