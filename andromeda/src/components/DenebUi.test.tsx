import { describe, expect, it, vi } from "vitest";
import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { parseDenebUi, splitDenebUi } from "@/markdown/denebUiParse";
import { AssistantText, DenebUi } from "./DenebUi";
import { statCountUpFrame } from "./statCountUp";

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

  it("blocks a callback while a required collected input is empty — and says so", async () => {
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
    // The blocked submit flags the field (the old silent return read as a dead button).
    expect(screen.getByText("필수 입력입니다")).toBeInTheDocument();
    // Editing the field clears its flag; the submit then goes through.
    await userEvent.type(screen.getByRole("textbox"), "42");
    expect(screen.queryByText("필수 입력입니다")).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "전송" }));
    expect(onSubmit).toHaveBeenCalledWith("Responded with: q: 42");
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
    const { container } = render(<AssistantText text={text} onUiSubmit={() => {}} />);
    expect(container.querySelector(".assistant-text.mixed")).toBeInTheDocument();
    expect(container.querySelector(".assistant-segment-md")).toBeInTheDocument();
    expect(container.querySelector(".assistant-segment-ui")).toBeInTheDocument();
    expect(screen.getByText("요약").tagName).toBe("STRONG"); // markdown span
    expect(screen.getByRole("button", { name: "승인" })).toBeInTheDocument(); // drawn UI
  });

  it("paints a streaming display-only HTML card progressively instead of a placeholder", () => {
    // Unclosed fence mid-stream, display-only tree → progressive render.
    const text = "브리핑입니다\n```deneb-ui\n<column><card><text>부가세 신고</text><badge>D-2</badge></card>";
    const { container } = render(<AssistantText text={text} onUiSubmit={() => {}} />);
    expect(container.querySelector(".dui-pending")).not.toBeInTheDocument();
    expect(screen.getByText("부가세 신고")).toBeInTheDocument();
    expect(screen.getByText("D-2")).toBeInTheDocument();
  });

  it("holds the placeholder for a streaming card that contains interactive nodes", () => {
    const text = '질문입니다\n```deneb-ui\n<column><card><button event="answer">42</button></card>';
    const { container } = render(<AssistantText text={text} onUiSubmit={() => {}} />);
    expect(container.querySelector(".dui-pending")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "42" })).not.toBeInTheDocument();
  });
});

describe("deneb-ui renderer parity conventions", () => {
  it("renders HH:MM lists as a timeline with bold time keys", () => {
    const spec = {
      type: "list",
      items: [
        { type: "text", value: "09:00 — 팀 스탠드업" },
        { type: "text", value: "14:00 — 거래처 미팅" },
      ],
    };
    const { container } = render(<DenebUi spec={spec} onSubmit={() => {}} />);
    expect(container.querySelectorAll(".dui-timeline-row")).toHaveLength(2);
    expect(container.querySelector(".dui-timeline-time")?.textContent).toBe("09:00");
  });

  it("when bolds the key of '키 — 내용' list items", () => {
    const spec = { type: "list", items: [{ type: "text", value: "김부장 — 견적서 회신 요청" }] };
    const { container } = render(<DenebUi spec={spec} onSubmit={() => {}} />);
    expect(container.querySelector("li strong")?.textContent).toBe("김부장");
  });

  it("consumes ** markers when the model marks the key itself", () => {
    // 2026-07-07 live letter regression: a raw key render showed literal
    // asterisks for **고건** — the key must run through inline rendering.
    const spec = { type: "list", items: [{ type: "text", value: "**고건** — 구조검토 요청" }] };
    const { container } = render(<DenebUi spec={spec} onSubmit={() => {}} />);
    const li = container.querySelector("li");
    expect(li?.textContent).not.toContain("**");
    expect(li?.textContent).toContain("고건 — 구조검토 요청");
  });

  it("when tints status badges and stat trends", () => {
    const spec = {
      type: "column",
      children: [
        { type: "badge", value: "완료", color: "success" },
        { type: "stat", value: "381톤", label: "주간 생산", description: "+2.1%" },
      ],
    };
    const { container } = render(<DenebUi spec={spec} onSubmit={() => {}} />);
    expect(container.querySelector(".dui-badge.success")).not.toBeNull();
    const desc = container.querySelector(".dui-stat-desc.pos");
    expect(desc?.textContent).toBe("▲ 2.1%");
  });

  it("when right-aligns numeric table columns", () => {
    const spec = {
      type: "table",
      headers: ["현장", "수량"],
      rows: [
        ["화성산단", "12"],
        ["부산 썬탑", "4"],
      ],
    };
    const { container } = render(<DenebUi spec={spec} onSubmit={() => {}} />);
    const cells = container.querySelectorAll("tbody td");
    expect((cells[1] as HTMLElement).style.textAlign).toBe("right");
    expect((cells[0] as HTMLElement).style.textAlign).toBe("");
  });

  it("when marks a 3+ column table dense, but not a 2-column one", () => {
    const three = render(
      <DenebUi
        spec={{ type: "table", headers: ["현장", "규격", "수량"], rows: [["화성", "RPS", "12"]] }}
        onSubmit={() => {}}
      />,
    );
    expect(three.container.querySelector("table.md-table")).toHaveClass("md-table-dense");
    const two = render(
      <DenebUi spec={{ type: "table", headers: ["현장", "수량"], rows: [["화성", "12"]] }} onSubmit={() => {}} />,
    );
    expect(two.container.querySelector("table.md-table")).not.toHaveClass("md-table-dense");
  });

  it("renders inline emphasis inside table cells and keeps numeric align", () => {
    const spec = {
      type: "table",
      headers: ["현장", "수량"],
      rows: [
        ["**화성산단**", "**12**"],
        ["부산 썬탑", "4"],
      ],
    };
    const { container } = render(<DenebUi spec={spec} onSubmit={() => {}} />);
    const cells = container.querySelectorAll("tbody td");
    expect(cells[0].querySelector("strong")?.textContent).toBe("화성산단");
    expect(cells[0].textContent).not.toContain("**");
    // A bolded number still classifies the column as numeric.
    expect((cells[1] as HTMLElement).style.textAlign).toBe("right");
  });

  it("renders inline emphasis inside alert and quote prose", () => {
    const spec = {
      type: "column",
      children: [
        { type: "alert", severity: "warning", title: "**긴급**", message: "**중요**: 확인 필요" },
        { type: "quote", text: "**품질** 우선", source: "회의" },
      ],
    };
    const { container } = render(<DenebUi spec={spec} onSubmit={() => {}} />);
    expect(container.querySelector(".dui-alert strong")?.textContent).toBe("긴급");
    expect(container.querySelector(".dui-alert")?.textContent).not.toContain("**");
    expect(container.querySelector(".dui-quote strong")?.textContent).toBe("품질");
  });

  it("when draws vertical bars scaled from the real series max (fractional series)", () => {
    const spec = { type: "chart", chartType: "bar", labels: ["a", "b"], values: [0.12, 0.18] };
    const { container } = render(<DenebUi spec={spec} onSubmit={() => {}} />);
    const rects = container.querySelectorAll(".dui-bar-rect");
    expect(rects).toHaveLength(2);
    const h0 = Number(rects[0].getAttribute("height"));
    const h1 = Number(rects[1].getAttribute("height"));
    // 0.18 is the series max → full plot height; 0.12 scales to 0.12/0.18 of it
    // (flooring at 1 would have flattened both to near-empty).
    expect(h1).toBeGreaterThan(h0);
    expect(h0 / h1).toBeCloseTo(0.12 / 0.18, 2);
  });

  it("without the bar for a zero value but keeps its label", () => {
    const spec = { type: "chart", chartType: "bar", labels: ["a", "b"], values: [0, 5] };
    const { container } = render(<DenebUi spec={spec} onSubmit={() => {}} />);
    // Zero is a real claim → no rect drawn, but the value text (0) still shows.
    expect(container.querySelectorAll(".dui-bar-rect")).toHaveLength(1);
    expect(container.querySelector(".dui-bar-svg")?.textContent).toContain("0");
  });

  it("renders inline emphasis inside text nodes", () => {
    const spec = { type: "text", value: "이번주 **필수** 확인" };
    const { container } = render(<DenebUi spec={spec} onSubmit={() => {}} />);
    expect(container.querySelector("strong")?.textContent).toBe("필수");
  });

  it("renders inline markdown links (native InlineTokenizer parity)", () => {
    // The HTML parser merges <a href> into [label](url); the phone renders a
    // real link, so desktop must not leak the literal brackets.
    const spec = { type: "text", value: "보고서 [링크](https://x)는 여기" };
    const { container } = render(<DenebUi spec={spec} onSubmit={() => {}} />);
    const a = container.querySelector("a");
    expect(a?.getAttribute("href")).toBe("https://x");
    expect(a?.textContent).toBe("링크");
    expect(container.textContent).not.toContain("](");
  });

  it("when promotes an icon+caption first row to the card header voice", () => {
    const spec = {
      type: "card",
      children: [
        {
          type: "row",
          children: [
            { type: "icon", name: "calendar", size: 16 },
            { type: "text", style: "caption", value: "오늘 일정" },
          ],
        },
        { type: "text", value: "본문" },
      ],
    };
    const { container } = render(<DenebUi spec={spec} onSubmit={() => {}} />);
    expect(container.querySelector(".dui-card-hd-label")?.textContent).toBe("오늘 일정");
    // The header icon actually draws (icons used to fall through to null).
    expect(container.querySelector(".dui-card-hd .dui-icon svg")).not.toBeNull();
  });

  it("tints status-colored text, gates unknown tints", () => {
    const spec = {
      type: "column",
      children: [
        { type: "text", color: "success", value: "정상 가동" },
        { type: "text", color: "x); background:red", value: "주입 시도" },
      ],
    };
    const { container } = render(<DenebUi spec={spec} onSubmit={() => {}} />);
    const tinted = container.querySelector(".dui-text.success");
    expect(tinted?.textContent).toBe("정상 가동");
    // Unknown color values never reach the class attribute.
    const other = Array.from(container.querySelectorAll(".dui-text")).at(1);
    expect(other?.className).toBe("dui-text");
  });

  it("renders emoji icon names as text and unknown names as nothing", () => {
    const spec = {
      type: "row",
      children: [
        { type: "icon", name: "⚠️", size: 16 },
        { type: "icon", name: "no_such_glyph", size: 16 },
      ],
    };
    const { container } = render(<DenebUi spec={spec} onSubmit={() => {}} />);
    expect(container.textContent).toContain("⚠️");
    expect(container.querySelectorAll(".dui-icon")).toHaveLength(0);
  });

  it("renders a code node with the shared code-fence chrome (language + copy)", () => {
    const spec = { type: "code", language: "sql", code: "SELECT 1;" };
    const { container } = render(<DenebUi spec={spec} onSubmit={() => {}} />);
    expect(container.querySelector(".md-codeblock")).not.toBeNull();
    expect(container.querySelector(".md-codelang")?.textContent).toBe("sql");
    expect(screen.getByRole("button", { name: "코드 복사" })).toBeInTheDocument();
    expect(container.querySelector("code")?.textContent).toBe("SELECT 1;");
  });

  it("draws the line chart as a smooth path with an area wash and a newest-point halo", () => {
    const spec = { type: "chart", chartType: "line", labels: ["a", "b", "c"], values: [1, 3, 2] };
    const { container } = render(<DenebUi spec={spec} onSubmit={() => {}} />);
    expect(container.querySelector("path.dui-line-path")?.getAttribute("d")).toContain("C");
    expect(container.querySelector("path.dui-line-area")).not.toBeNull();
    expect(container.querySelectorAll("circle.dui-line-halo")).toHaveLength(1);
    // Values stay honest: every point keeps its label.
    expect(container.querySelectorAll("text.dui-line-val")).toHaveLength(3);
  });

  it("stat count-up frames preserves decimals/grouping and settle on the exact original", () => {
    // Settled frame IS the original string — exact metrics keep precision.
    expect(statCountUpFrame("12.45%", 1)).toBe("12.45%");
    // Zero frame keeps the decimal width and the prefix/suffix.
    expect(statCountUpFrame("12.45%", 0)).toBe("0.00%");
    expect(statCountUpFrame("381톤", 0)).toBe("0톤");
    // Grouped targets stay grouped mid-flight.
    expect(statCountUpFrame("1,386", 0.9999)).toContain(",");
    // Non-numeric values pass through untouched.
    expect(statCountUpFrame("—", 0.5)).toBe("—");
  });

  it("marks alerts with a severity glyph and gates unknown severities to info", () => {
    const { container } = render(
      <DenebUi spec={{ type: "alert", severity: 'x"); inject', message: "본문" }} onSubmit={() => {}} />,
    );
    const alert = container.querySelector(".dui-alert");
    expect(alert?.className).toBe("dui-alert info");
    expect(alert?.querySelector(".dui-alert-glyph")?.textContent).toBe("i");
    const warn = render(<DenebUi spec={{ type: "alert", severity: "success", message: "완료" }} onSubmit={() => {}} />);
    expect(warn.container.querySelector(".dui-alert-glyph")?.textContent).toBe("✓");
  });
});

// Rendering-quality parity with the native renderer (the desktop nodes that
// previously rendered barer than the phone).
describe("DenebUi node rendering polish", () => {
  it("when ticks a countdown node down each second as MM:SS", () => {
    vi.useFakeTimers();
    try {
      render(<DenebUi spec={{ type: "countdown", label: "마감", seconds: 90 }} onSubmit={() => {}} />);
      expect(screen.getByText("01:30")).toBeInTheDocument();
      act(() => vi.advanceTimersByTime(1000));
      expect(screen.getByText("01:29")).toBeInTheDocument();
    } finally {
      vi.useRealTimers();
    }
  });

  it("tints a countdown as expired at zero", () => {
    vi.useFakeTimers();
    try {
      render(<DenebUi spec={{ type: "countdown", seconds: 1 }} onSubmit={() => {}} />);
      act(() => vi.advanceTimersByTime(1000));
      const value = screen.getByText("00:00");
      expect(value).toHaveClass("expired");
    } finally {
      vi.useRealTimers();
    }
  });

  it("renders a value-less progress as an indeterminate bar, a valued one as a fill", () => {
    const { container } = render(<DenebUi spec={{ type: "progress" }} onSubmit={() => {}} />);
    expect(container.querySelector(".dui-progress-fill")).toHaveClass("indeterminate");
    const valued = render(<DenebUi spec={{ type: "progress", value: 0.5 }} onSubmit={() => {}} />);
    const fill = valued.container.querySelector(".dui-progress-fill");
    expect(fill).not.toHaveClass("indeterminate");
    expect(fill).toHaveStyle({ width: "50%" });
  });

  it("when gives a secondary badge its own class, distinct from primary", () => {
    const { container } = render(
      <DenebUi spec={{ type: "badge", value: "보조", color: "secondary" }} onSubmit={() => {}} />,
    );
    const badge = container.querySelector(".dui-badge");
    expect(badge).toHaveClass("secondary");
    expect(badge).not.toHaveClass("primary");
  });

  it("when builds up to two-word avatar initials and falls back to an icon, not a question mark", () => {
    render(<DenebUi spec={{ type: "avatar", name: "John Doe" }} onSubmit={() => {}} />);
    expect(screen.getByText("JD")).toBeInTheDocument();
    const empty = render(<DenebUi spec={{ type: "avatar" }} onSubmit={() => {}} />);
    const avatar = empty.container.querySelector(".dui-avatar");
    expect(avatar?.textContent).toBe(""); // an icon, never a literal "?"
    expect(avatar?.querySelector("svg")).toBeTruthy();
  });

  it("normalizes an inverted slider range (min>max)", () => {
    const { container } = render(<DenebUi spec={{ type: "slider", id: "s", min: 100, max: 0 }} onSubmit={() => {}} />);
    const input = container.querySelector('input[type="range"]') as HTMLInputElement;
    expect(Number(input.min)).toBe(0);
    expect(Number(input.max)).toBe(100);
  });

  it("renders an alt-text placeholder for a non-resolvable image url, an <img> for http", () => {
    const { container } = render(
      <DenebUi spec={{ type: "image", url: "/relative/x.png", alt: "견적서 미리보기" }} onSubmit={() => {}} />,
    );
    expect(container.querySelector(".dui-image-alt")?.textContent).toBe("견적서 미리보기");
    expect(container.querySelector("img.dui-image")).toBeNull();
    const ok = render(<DenebUi spec={{ type: "image", url: "https://x/y.png", alt: "z" }} onSubmit={() => {}} />);
    expect(ok.container.querySelector("img.dui-image")).not.toBeNull();
  });

  it("when aligns a box by contentAlignment", () => {
    const { container } = render(
      <DenebUi
        spec={{ type: "box", contentAlignment: "center", children: [{ type: "text", value: "가운데" }] }}
        onSubmit={() => {}}
      />,
    );
    const centered = Array.from(container.querySelectorAll(".dui-col")).find(
      (el) => (el as HTMLElement).style.alignItems === "center",
    ) as HTMLElement | undefined;
    expect(centered).toBeTruthy();
    expect(centered!.style.justifyContent).toBe("center");
  });

  it("maps a broadened icon name to a glyph, and still renders nothing for a truly unknown name", () => {
    const { container } = render(<DenebUi spec={{ type: "icon", name: "description" }} onSubmit={() => {}} />);
    expect(container.querySelector(".dui-icon svg")).not.toBeNull();
    const unknown = render(<DenebUi spec={{ type: "icon", name: "totally_unknown_xyz" }} onSubmit={() => {}} />);
    expect(unknown.container.querySelector(".dui-icon")).toBeNull();
  });

  it("splits a stat value into a hero number and a unit suffix, but preserves a bare number whole", () => {
    const withUnit = render(<DenebUi spec={{ type: "stat", value: "381톤", label: "생산" }} onSubmit={() => {}} />);
    expect(withUnit.container.querySelector(".dui-stat-value")?.textContent).toBe("381");
    expect(withUnit.container.querySelector(".dui-stat-unit")?.textContent).toBe("톤");
    const pct = render(<DenebUi spec={{ type: "stat", value: "68%", label: "공정" }} onSubmit={() => {}} />);
    expect(pct.container.querySelector(".dui-stat-unit")?.textContent).toBe("%");
    const bare = render(<DenebUi spec={{ type: "stat", value: "12", label: "미열람" }} onSubmit={() => {}} />);
    expect(bare.container.querySelector(".dui-stat-unit")).toBeNull();
    expect(bare.container.querySelector(".dui-stat-value")?.textContent).toBe("12");
  });

  it("fills bar-chart columns with a per-chart gradient", () => {
    const { container } = render(
      <DenebUi spec={{ type: "chart", chartType: "bar", labels: ["a", "b"], values: [1, 2] }} onSubmit={() => {}} />,
    );
    const grad = container.querySelector("linearGradient");
    expect(grad?.id).toBeTruthy();
    expect(container.querySelector(".dui-bar-rect")?.getAttribute("fill")).toBe(`url(#${grad!.id})`);
  });
});

describe("stale-card gating (interactive=false)", () => {
  const formSpec = {
    type: "column",
    children: [
      { type: "chip_group", id: "slot", chips: [{ value: "10:00", label: "오전" }] },
      { type: "text_input", id: "name", label: "이름" },
      { type: "button", label: "잡기", action: { type: "callback", event: "confirm", collectFrom: ["slot"] } },
      { type: "button", label: "복사", action: { type: "copy_to_clipboard", text: "npm run dev" } },
    ],
  };

  it("locks callbacks and form controls on a stale card, but keeps local actions live", async () => {
    const onSubmit = vi.fn();
    render(<DenebUi spec={formSpec} onSubmit={onSubmit} interactive={false} />);

    // callback button + form controls are disabled…
    expect(screen.getByRole("button", { name: "잡기" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "오전" })).toBeDisabled();
    expect(screen.getByRole("textbox")).toBeDisabled();
    // …but a local action (copy) stays usable.
    expect(screen.getByRole("button", { name: "복사" })).toBeEnabled();

    await userEvent.click(screen.getByRole("button", { name: "잡기" }));
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("keeps a stale accordion explorable", async () => {
    render(
      <DenebUi
        spec={{ type: "accordion", title: "상세", children: [{ type: "text", value: "본문" }] }}
        onSubmit={() => {}}
        interactive={false}
      />,
    );
    expect(screen.queryByText("본문")).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: /상세/ }));
    expect(screen.getByText("본문")).toBeInTheDocument();
  });

  it("stays fully interactive by default", async () => {
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
});
