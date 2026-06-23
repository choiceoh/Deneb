import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { AssistantText, DenebUi, parseDenebUi, splitDenebUi } from "./DenebUi";

describe("deneb-ui parsing", () => {
  it("parses an object, wraps a bare array as a column, and accepts NDJSON", () => {
    expect(parseDenebUi('{"type":"text","value":"hi"}')).toMatchObject({ type: "text", value: "hi" });
    expect(parseDenebUi('[{"type":"text","value":"a"}]')).toMatchObject({ type: "column" });
    const nd = parseDenebUi('{"type":"text","value":"a"}\n{"type":"text","value":"b"}');
    expect(nd).toMatchObject({ type: "column" });
    expect(nd?.children).toHaveLength(2);
  });

  it("splits a reply into markdown spans and deneb-ui blocks; flags an unclosed block", () => {
    const segs = splitDenebUi('앞\n```deneb-ui\n{"type":"text","value":"x"}\n```\n뒤');
    expect(segs.map((s) => s.kind)).toEqual(["md", "ui", "md"]);

    const pending = splitDenebUi('```deneb-ui\n{"type":"text"'); // closing fence not arrived yet
    expect(pending.at(-1)?.kind).toBe("ui-pending");
  });
});

describe("DenebUi rendering + callback round-trip", () => {
  it("collects input values and round-trips a callback as a 'Responded with' message", async () => {
    const onSubmit = vi.fn();
    const spec = {
      type: "column",
      children: [
        { type: "text_input", id: "name", label: "이름" },
        { type: "button", label: "보내기", action: { type: "callback", event: "submit", collectFrom: ["name"] } },
      ],
    };
    render(<DenebUi spec={spec} onSubmit={onSubmit} />);

    await userEvent.type(screen.getByRole("textbox"), "홍길동");
    await userEvent.click(screen.getByRole("button", { name: "보내기" }));

    expect(onSubmit).toHaveBeenCalledWith("Responded with: name: 홍길동");
  });

  it("sends 'Pressed: <event>' for a callback with no data", async () => {
    const onSubmit = vi.fn();
    render(
      <DenebUi
        spec={{ type: "button", label: "확인", action: { type: "callback", event: "ok" } }}
        onSubmit={onSubmit}
      />,
    );
    await userEvent.click(screen.getByRole("button", { name: "확인" }));
    expect(onSubmit).toHaveBeenCalledWith("Pressed: ok");
  });

  it("blocks a callback while a required collected input is empty", async () => {
    const onSubmit = vi.fn();
    const spec = {
      type: "column",
      children: [
        { type: "text_input", id: "q", label: "답", required: true },
        { type: "button", label: "전송", action: { type: "callback", event: "x", collectFrom: ["q"] } },
      ],
    };
    render(<DenebUi spec={spec} onSubmit={onSubmit} />);
    await userEvent.click(screen.getByRole("button", { name: "전송" }));
    expect(onSubmit).not.toHaveBeenCalled(); // required input empty → gated
  });

  it("renders content nodes (stat, alert)", () => {
    const spec = {
      type: "column",
      children: [
        { type: "stat", value: "12", label: "미열람" },
        { type: "alert", severity: "warning", title: "주의", message: "마감 임박" },
      ],
    };
    render(<DenebUi spec={spec} onSubmit={() => {}} />);
    expect(screen.getByText("12")).toBeInTheDocument();
    expect(screen.getByText("미열람")).toBeInTheDocument();
    expect(screen.getByText("마감 임박")).toBeInTheDocument();
  });
});

describe("AssistantText", () => {
  it("renders an embedded deneb-ui block as interactive UI alongside markdown", () => {
    const text =
      '**요약**\n\n```deneb-ui\n{"type":"button","label":"승인","action":{"type":"callback","event":"approve"}}\n```';
    render(<AssistantText text={text} onUiSubmit={() => {}} />);
    expect(screen.getByText("요약").tagName).toBe("STRONG"); // markdown span
    expect(screen.getByRole("button", { name: "승인" })).toBeInTheDocument(); // drawn UI
  });
});
