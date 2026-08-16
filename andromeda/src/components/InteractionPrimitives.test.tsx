import { describe, expect, it, vi } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { DayPager } from "./DayPager";
import { DenebStar } from "./DenebStar";
import { Icon, type IconName } from "./Icon";
import { ModelPicker } from "./ModelPicker";
import { SessionDrawer } from "./SessionDrawer";
import { depsEqual } from "@/depsEqual";
import { proactiveNav } from "./proactiveNav";
import type { ModelsList, SessionRow } from "@/gateway";
import type { ProactiveEvent } from "@/events";

describe("DayPager", () => {
  function renderPager(overrides: Partial<React.ComponentProps<typeof DayPager>> = {}) {
    const props: React.ComponentProps<typeof DayPager> = {
      label: "7월 11일 금요일",
      count: 4,
      canPrev: true,
      canNext: true,
      atToday: false,
      onPrev: vi.fn(),
      onNext: vi.fn(),
      onToday: vi.fn(),
      ...overrides,
    };
    render(<DayPager {...props} />);
    return props;
  }

  it("displays selected day and row count when pager renders", () => {
    renderPager();

    const live = screen.getByText("7월 11일 금요일").parentElement!;
    expect(live).toHaveAttribute("aria-live", "polite");
    expect(within(live).getByText("4건")).toBeInTheDocument();
  });

  it("invokes navigation callbacks when prev next today clicked", async () => {
    const props = renderPager();

    await userEvent.click(screen.getByRole("button", { name: "이전 날" }));
    await userEvent.click(screen.getByRole("button", { name: "다음 날" }));
    await userEvent.click(screen.getByRole("button", { name: "오늘로" }));

    expect(props.onPrev).toHaveBeenCalledTimes(1);
    expect(props.onNext).toHaveBeenCalledTimes(1);
    expect(props.onToday).toHaveBeenCalledTimes(1);
  });

  it("disables prev and next when boundary reached", () => {
    renderPager({ canPrev: false, canNext: false });

    expect(screen.getByRole("button", { name: "이전 날" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "다음 날" })).toBeDisabled();
  });

  it("hides the today action when already showing today", () => {
    renderPager({ atToday: true });

    expect(screen.queryByRole("button", { name: "오늘로" })).not.toBeInTheDocument();
  });

  it.each([
    [0, "0건"],
    [1, "1건"],
    [999, "999건"],
  ])("renders count %i without altering it", (count, expected) => {
    renderPager({ count });
    expect(screen.getByText(expected)).toBeInTheDocument();
  });
});

describe("DenebStar", () => {
  it("when is decorative and hidden from assistive technology", () => {
    const { container } = render(<DenebStar />);

    expect(container.firstElementChild).toHaveAttribute("aria-hidden", "true");
    expect(container.querySelector("svg")).toHaveAttribute("viewBox", "0 0 24 24");
  });

  it("when uses the default 20-pixel canvas", () => {
    const { container } = render(<DenebStar />);
    expect(container.querySelector("svg")).toHaveAttribute("width", "20");
    expect(container.querySelector("svg")).toHaveAttribute("height", "20");
  });

  it.each([8, 16, 24, 48])("when uses the requested %i-pixel canvas", (size) => {
    const { container } = render(<DenebStar size={size} />);
    expect(container.querySelector("svg")).toHaveAttribute("width", String(size));
    expect(container.querySelector("svg")).toHaveAttribute("height", String(size));
  });

  it("preserves one glow and one core path", () => {
    const { container } = render(<DenebStar />);
    expect(container.querySelectorAll(".deneb-star-glow")).toHaveLength(1);
    expect(container.querySelectorAll(".deneb-star-core")).toHaveLength(1);
    expect(container.querySelector(".deneb-star-core")).toHaveAttribute("fill", "url(#deneb-sheen)");
  });

  it("defines a rotating sheen without exposing animation controls", () => {
    const { container } = render(<DenebStar />);
    const animation = container.querySelector("animateTransform");
    expect(animation).toHaveAttribute("type", "rotate");
    expect(animation).toHaveAttribute("repeatCount", "indefinite");
    expect(animation).toHaveAttribute("dur", "11s");
  });
});

const iconNames: IconName[] = [
  "today",
  "chat",
  "attach",
  "projects",
  "progress",
  "todo",
  "notebook",
  "mail",
  "calendar",
  "wiki",
  "files",
  "search",
  "people",
  "crons",
  "fleet",
  "workfeed",
  "skills",
  "code",
  "send",
  "bell",
  "arrow-right",
  "close",
  "win-min",
  "win-max",
  "settings",
  "check",
  "copy",
  "stop",
  "refresh",
  "history",
  "plus",
  "trash",
  "chevron-down",
  "chevron-up",
  "expand-panel",
  "collapse-panel",
  "printer",
];

describe("Icon", () => {
  it.each(iconNames)("renders the %s glyph in the shared SVG frame", (name) => {
    const { container } = render(<Icon name={name} />);
    const svg = container.querySelector("svg")!;

    expect(svg).toHaveAttribute("viewBox", "0 0 24 24");
    expect(svg).toHaveAttribute("fill", "none");
    expect(svg).toHaveAttribute("stroke", "currentColor");
    expect(svg.childElementCount).toBeGreaterThan(0);
  });

  it.each(iconNames)("when marks the %s glyph as decorative", (name) => {
    const { container } = render(<Icon name={name} />);
    const svg = container.querySelector("svg")!;

    expect(svg).toHaveAttribute("aria-hidden", "true");
    expect(svg).toHaveAttribute("focusable", "false");
  });

  it("when forwards size, class, and stroke width", () => {
    const { container } = render(<Icon name="settings" size={28} className="nav-glyph" strokeWidth={2.5} />);
    const svg = container.querySelector("svg")!;

    expect(svg).toHaveClass("nav-glyph");
    expect(svg).toHaveAttribute("width", "28");
    expect(svg).toHaveAttribute("height", "28");
    expect(svg).toHaveAttribute("stroke-width", "2.5");
  });

  it("when uses the compact defaults", () => {
    const { container } = render(<Icon name="today" />);
    const svg = container.querySelector("svg")!;

    expect(svg).toHaveAttribute("width", "16");
    expect(svg).toHaveAttribute("height", "16");
    expect(svg).toHaveAttribute("stroke-width", "1.85");
  });
});

const models: ModelsList = {
  current: "provider/fast",
  roles: [],
  sections: [
    {
      title: "General",
      models: [
        { id: "provider/fast", label: "Fast" },
        { id: "provider/smart", label: "Smart", unhealthy: true },
      ],
    },
    { title: "Local", models: [{ id: "local/tiny", label: "Tiny" }] },
  ],
};

describe("ModelPicker", () => {
  it.each([null, { current: "", roles: [], sections: [] } as ModelsList])(
    "stays hidden without section data",
    (value) => {
      const { container } = render(<ModelPicker models={value} value="" onChange={() => {}} />);
      expect(container).toBeEmptyDOMElement();
    },
  );

  it("groups models under sections when picker renders", () => {
    render(<ModelPicker models={models} value="provider/fast" onChange={() => {}} />);

    const picker = screen.getByRole("combobox", { name: "모델 선택" });
    const groups = picker.querySelectorAll("optgroup");
    expect(groups).toHaveLength(2);
    expect(groups[0]).toHaveAttribute("label", "General");
    expect(groups[1]).toHaveAttribute("label", "Local");
    expect(screen.getAllByRole("option").map((option) => option.textContent)).toEqual(["Fast", "Smart ⚠", "Tiny"]);
  });

  it("when selects the active model", () => {
    render(<ModelPicker models={models} value="local/tiny" onChange={() => {}} />);
    expect(screen.getByRole("combobox")).toHaveValue("local/tiny");
  });

  it("surfaces an orphaned custom model instead of rendering blank", () => {
    render(<ModelPicker models={models} value="custom/legacy" onChange={() => {}} />);

    expect(screen.getByRole("combobox")).toHaveValue("custom/legacy");
    expect(screen.getAllByRole("option")[0]).toHaveTextContent("custom/legacy");
  });

  it("without duplicate a known active model as an orphan", () => {
    render(<ModelPicker models={models} value="provider/fast" onChange={() => {}} />);
    expect(screen.getAllByRole("option", { name: "Fast" })).toHaveLength(1);
  });

  it("returns user selection by model id", async () => {
    const onChange = vi.fn();
    render(<ModelPicker models={models} value="provider/fast" onChange={onChange} />);

    await userEvent.selectOptions(screen.getByRole("combobox"), "provider/smart");

    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange).toHaveBeenCalledWith("provider/smart");
  });

  it("when forwards the busy state", () => {
    render(<ModelPicker models={models} value="provider/fast" onChange={() => {}} disabled />);
    expect(screen.getByRole("combobox")).toBeDisabled();
  });
});

const sessions: SessionRow[] = [
  {
    key: "client:main:alpha",
    label: "  첫 대화  ",
    model: "smart",
    updatedAtMs: Date.UTC(2025, 0, 2, 3, 4),
  },
  { key: "client:main:beta", label: "   " },
];

describe("SessionDrawer", () => {
  it("shows accessible empty state when session list is empty", () => {
    render(
      <SessionDrawer
        sessions={[]}
        currentKey="client:main"
        busy={false}
        error=""
        onSelect={() => {}}
        onDelete={() => {}}
        onNew={() => {}}
      />,
    );

    expect(screen.getByRole("group", { name: "대화 기록" })).toBeInTheDocument();
    expect(screen.getByText("최근 대화가 없습니다.")).toBeInTheDocument();
  });

  it("shows the error instead of the empty state", () => {
    render(
      <SessionDrawer
        sessions={[]}
        currentKey="client:main"
        busy={false}
        error="기록을 불러오지 못했습니다"
        onSelect={() => {}}
        onDelete={() => {}}
        onNew={() => {}}
      />,
    );

    expect(screen.getByText("기록을 불러오지 못했습니다")).toHaveClass("error");
    expect(screen.queryByText("최근 대화가 없습니다.")).not.toBeInTheDocument();
  });

  it("when uses a trimmed label and falls back to the session key", () => {
    render(
      <SessionDrawer
        sessions={sessions}
        currentKey="client:main"
        busy={false}
        error=""
        onSelect={() => {}}
        onDelete={() => {}}
        onNew={() => {}}
      />,
    );

    expect(screen.getByText("첫 대화")).toBeInTheDocument();
    expect(screen.getByText("client:main:beta")).toBeInTheDocument();
  });

  it("when marks only the current conversation active", () => {
    render(
      <SessionDrawer
        sessions={sessions}
        currentKey="client:main:beta"
        busy={false}
        error=""
        onSelect={() => {}}
        onDelete={() => {}}
        onNew={() => {}}
      />,
    );

    expect(screen.getByText("첫 대화").closest("li")).not.toHaveClass("active");
    expect(screen.getByText("client:main:beta").closest("li")).toHaveClass("active");
  });

  it("renders model and timestamp metadata without empty separators", () => {
    render(
      <SessionDrawer
        sessions={sessions}
        currentKey=""
        busy={false}
        error=""
        onSelect={() => {}}
        onDelete={() => {}}
        onNew={() => {}}
      />,
    );

    expect(screen.getByText(/smart ·/)).toBeInTheDocument();
    expect(screen.getByText("client:main:beta").parentElement).not.toHaveTextContent("·");
  });

  it("routes select, delete, and new-conversation actions", async () => {
    const onSelect = vi.fn();
    const onDelete = vi.fn();
    const onNew = vi.fn();
    render(
      <SessionDrawer
        sessions={sessions}
        currentKey=""
        busy={false}
        error=""
        onSelect={onSelect}
        onDelete={onDelete}
        onNew={onNew}
      />,
    );

    await userEvent.click(screen.getByText("첫 대화").closest("button")!);
    await userEvent.click(screen.getByRole("button", { name: "대화 삭제: 첫 대화" }));
    await userEvent.click(screen.getByRole("button", { name: /새 대화/ }));

    expect(onSelect).toHaveBeenCalledWith("client:main:alpha");
    expect(onDelete).toHaveBeenCalledWith("client:main:alpha");
    expect(onNew).toHaveBeenCalledTimes(1);
  });

  it("when disables every action while busy", () => {
    render(
      <SessionDrawer
        sessions={sessions}
        currentKey=""
        busy
        error=""
        onSelect={() => {}}
        onDelete={() => {}}
        onNew={() => {}}
      />,
    );

    for (const button of screen.getAllByRole("button")) expect(button).toBeDisabled();
  });
});

describe("depsEqual", () => {
  const ref = {};
  const fn = () => undefined;

  it.each([
    [[], [], true],
    [[1, "a", true], [1, "a", true], true],
    [[ref, fn], [ref, fn], true],
    [[1], [1, 2], false],
    [[1, 2], [1], false],
    [[1, 2], [1, 3], false],
    [[{}], [{}], false],
    [[0], [-0], false],
    [[Number.NaN], [Number.NaN], true],
  ])("compares %j with %j", (a, b, expected) => {
    expect(depsEqual(a, b)).toBe(expected);
  });
});

describe("proactiveNav", () => {
  const event = (kind?: string, ref?: string): ProactiveEvent => ({ id: "event-1", kind, ref, raw: {} });

  it.each([
    "mail",
    "calendar",
    "todo",
    "workfeed",
    "fleet",
    "wiki",
    "people",
    "crons",
    "files",
    "search",
    "notebook",
    "today",
  ])("when maps the %s event to its pane", (kind) => {
    expect(proactiveNav(event(kind, "resource-7"))).toEqual({ view: kind, ref: "resource-7" });
  });

  it.each([undefined, "", "message", "chat", "settings", "unknown"])("rejects non-navigable kind %s", (kind) => {
    expect(proactiveNav(event(kind, "resource-7"))).toBeNull();
  });

  it("preserves an absent resource reference", () => {
    expect(proactiveNav(event("today"))).toEqual({ view: "today", ref: undefined });
  });
});
