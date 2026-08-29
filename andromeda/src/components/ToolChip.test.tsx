import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { ToolChip, formatElapsed, previewLineClass, toolLabel } from "./ToolChip";

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

describe("formatElapsed", () => {
  it("keeps millisecond resolution below a second", () => {
    // A 40ms cache hit and a 900ms one are different facts on a coding surface;
    // rounding both to "0.0초" would erase the distinction.
    expect(formatElapsed(40)).toBe("40ms");
    expect(formatElapsed(912)).toBe("912ms");
  });

  it("switches to seconds, losing the decimal once it stops mattering", () => {
    expect(formatElapsed(1_000)).toBe("1.0초");
    expect(formatElapsed(3_240)).toBe("3.2초");
    expect(formatElapsed(24_400)).toBe("24초");
  });

  it("reads minutes for long steps", () => {
    expect(formatElapsed(60_000)).toBe("1분");
    expect(formatElapsed(80_000)).toBe("1분 20초");
  });

  it("renders nothing for a nonsense span", () => {
    expect(formatElapsed(-1)).toBe("");
    expect(formatElapsed(Number.NaN)).toBe("");
  });
});

describe("ToolChip duration", () => {
  const base = { kind: "tool", id: "t1", tool: "exec", state: "completed" } as const;

  it("shows what a finished call cost", () => {
    render(<ToolChip part={{ ...base, elapsedMs: 1_500 }} />);
    expect(screen.getByText("1.5초")).toBeInTheDocument();
  });

  it("claims no duration on a restored chip that was never timed", () => {
    // Restored transcripts carry no timing — inventing one would be a fiction
    // presented as measurement.
    const { container } = render(<ToolChip part={base} />);
    expect(container.querySelector(".tool-chip-elapsed")).toBeNull();
  });

  it("waits for completion before reporting a span", () => {
    // A running chip has a start but no end; a number mid-flight reads as final.
    const { container } = render(<ToolChip part={{ ...base, state: "started", startedAtMs: 1 }} />);
    expect(container.querySelector(".tool-chip-elapsed")).toBeNull();
  });
});

describe("toolLabel", () => {
  it("prefers the gateway's Korean name over the identifier", () => {
    // Korean-first product: the desktop used to render "gmail" while the phone
    // said "메일 확인" for the same call.
    expect(toolLabel("gmail", "메일 확인")).toBe("메일 확인");
  });

  it("falls back to the humanized id when no label was sent", () => {
    // An uncurated tool, or an older gateway — readable beats blank.
    expect(toolLabel("phone_read")).toBe("phone read");
    expect(toolLabel("gmail.list_recent", "  ")).toBe("gmail list recent");
  });
});

describe("ToolChip labelling", () => {
  it("renders the gateway name when one arrives", () => {
    render(<ToolChip part={{ kind: "tool", id: "t1", tool: "gmail", state: "completed", label: "메일 확인" }} />);
    expect(screen.getByText("메일 확인")).toBeInTheDocument();
  });
});
