import { createRef } from "react";
import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import type { ModelsList, SessionRow } from "@/gateway";
import type { CalEvent } from "@/types";
import { ChatComposer, ScrollToBottomButton } from "./ChatComposer";
import { DenebStar } from "./DenebStar";
import { Icon, type IconName } from "./Icon";
import { ModelPicker } from "./ModelPicker";
import { MonthGrid } from "./MonthGrid";
import { SessionDrawer } from "./SessionDrawer";

function renderComposer(overrides: Partial<React.ComponentProps<typeof ChatComposer>> = {}) {
  const props: React.ComponentProps<typeof ChatComposer> = {
    composeRef: createRef<HTMLTextAreaElement>(),
    fileRef: createRef<HTMLInputElement>(),
    busy: false,
    stoppable: false,
    connected: true,
    input: "",
    placeholder: "메시지를 입력하세요",
    onInput: vi.fn(),
    onSubmit: vi.fn(),
    onStop: vi.fn(),
    onPick: vi.fn(),
    onAttachFiles: vi.fn(),
    ...overrides,
  };
  const view = render(<ChatComposer {...props} />);
  return { props, ...view };
}

describe("ChatComposer", () => {
  it("wires supplied refs when composer renders", () => {
    const { props } = renderComposer();
    expect(props.composeRef.current).toBe(screen.getByRole("textbox", { name: "Deneb에게 메시지" }));
    expect(props.fileRef.current).toBe(document.querySelector('input[type="file"]'));
  });

  it("renders the idle placeholder and controlled value", () => {
    renderComposer({ input: "hello", placeholder: "custom placeholder" });
    expect(screen.getByRole("textbox", { name: "Deneb에게 메시지" })).toHaveValue("hello");
    expect(screen.getByPlaceholderText("custom placeholder")).toBeEnabled();
  });

  it("when routes controlled input changes", async () => {
    const { props } = renderComposer();
    await userEvent.type(screen.getByRole("textbox", { name: "Deneb에게 메시지" }), "abc");
    expect(props.onInput).toHaveBeenNthCalledWith(1, "a");
    expect(props.onInput).toHaveBeenLastCalledWith("c");
  });

  it("submits on Enter when input ready", async () => {
    const { props } = renderComposer({ input: "ready" });
    await userEvent.click(screen.getByRole("button", { name: "전송" }));
    expect(props.onSubmit).toHaveBeenCalledTimes(1);

    fireEvent.keyDown(screen.getByRole("textbox", { name: "Deneb에게 메시지" }), { key: "Enter" });
    expect(props.onSubmit).toHaveBeenCalledTimes(2);
  });

  it("preserves Shift+Enter inside textarea without submitting", () => {
    const { props } = renderComposer({ input: "ready" });
    const input = screen.getByRole("textbox", { name: "Deneb에게 메시지" });
    fireEvent.keyDown(input, { key: "Enter", shiftKey: true });
    fireEvent.keyDown(input, { key: "Enter", isComposing: true });
    fireEvent.keyDown(input, { key: "Escape" });
    expect(props.onSubmit).not.toHaveBeenCalled();
  });

  it.each([
    [false, "", true],
    [false, "hello", true],
    [true, "   ", true],
    [true, "hello", false],
  ])("when sets send disabled for connected=%s input=%j", (connected, input, disabled) => {
    renderComposer({ connected, input });
    expect(screen.getByRole("button", { name: "전송" })).toHaveProperty("disabled", disabled);
  });

  it("when opens the hidden file input from the attach affordance", async () => {
    const { props } = renderComposer();
    const fileInput = props.fileRef.current!;
    const click = vi.spyOn(fileInput, "click");
    await userEvent.click(screen.getByRole("button", { name: "파일 첨부" }));
    expect(click).toHaveBeenCalledOnce();
    expect(fileInput).toHaveAttribute("multiple");
    expect(fileInput.accept).toContain("image/*");
    expect(fileInput.accept).toContain(".pdf");
  });

  it("when disables attachment while disconnected or busy", () => {
    const { unmount } = renderComposer({ connected: false });
    expect(screen.getByRole("button", { name: "파일 첨부" })).toBeDisabled();
    unmount();
    renderComposer({ busy: true });
    expect(screen.getByRole("button", { name: "파일 첨부" })).toBeDisabled();
  });

  it("when routes the native file input change", () => {
    const { props } = renderComposer();
    const file = new File(["content"], "report.txt", { type: "text/plain" });
    fireEvent.change(props.fileRef.current!, { target: { files: [file] } });
    expect(props.onPick).toHaveBeenCalledOnce();
  });

  it("when turns pasted files into attachments", () => {
    const { props } = renderComposer();
    const file = new File(["image"], "shot.png", { type: "image/png" });
    const event = new Event("paste", { bubbles: true, cancelable: true });
    Object.defineProperty(event, "clipboardData", { value: { files: [file] } });
    screen.getByRole("textbox", { name: "Deneb에게 메시지" }).dispatchEvent(event);
    expect(event.defaultPrevented).toBe(true);
    expect(props.onAttachFiles).toHaveBeenCalledWith([file]);
  });

  it("preserves text-only paste to the browser", () => {
    const { props } = renderComposer();
    const event = new Event("paste", { bubbles: true, cancelable: true });
    Object.defineProperty(event, "clipboardData", { value: { files: [] } });
    screen.getByRole("textbox", { name: "Deneb에게 메시지" }).dispatchEvent(event);
    expect(event.defaultPrevented).toBe(false);
    expect(props.onAttachFiles).not.toHaveBeenCalled();
  });

  it("displays a live attachment notice", () => {
    renderComposer({ note: "대용량 파일은 건너뛰었습니다" });
    expect(screen.getByRole("status")).toHaveTextContent("대용량 파일은 건너뛰었습니다");
  });

  it("shows an honest non-stoppable capture state", () => {
    renderComposer({ busy: true, stoppable: false, input: "working" });
    expect(screen.getByRole("textbox", { name: "Deneb에게 메시지" })).toBeDisabled();
    expect(screen.getByPlaceholderText("응답 중…")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "첨부 분석 중" })).toBeDisabled();
    expect(screen.queryByRole("button", { name: "중단" })).not.toBeInTheDocument();
  });

  it("routes stop only when the stream is stoppable", async () => {
    const { props } = renderComposer({ busy: true, stoppable: true });
    await userEvent.click(screen.getByRole("button", { name: "중단" }));
    expect(props.onStop).toHaveBeenCalledOnce();
    expect(screen.queryByRole("button", { name: "전송" })).not.toBeInTheDocument();
  });
});

describe("ScrollToBottomButton", () => {
  it("renders scroll button only when detached from latest message", async () => {
    const onClick = vi.fn();
    const { rerender } = render(<ScrollToBottomButton visible={false} onClick={onClick} />);
    expect(screen.queryByRole("button", { name: "맨 아래로" })).not.toBeInTheDocument();
    rerender(<ScrollToBottomButton visible onClick={onClick} />);
    await userEvent.click(screen.getByRole("button", { name: "맨 아래로" }));
    expect(onClick).toHaveBeenCalledOnce();
  });
});

describe("MonthGrid", () => {
  const timed: CalEvent = {
    id: "timed",
    summary: "Review",
    start: "2026-07-15T09:00:00+09:00",
    end: "2026-07-15T10:00:00+09:00",
    category: "mine",
  };
  const deadline: CalEvent = {
    id: "deadline",
    summary: "Deadline",
    start: { date: "2026-07-15" },
    end: { date: "2026-07-16" },
    allDay: true,
    category: "deadline",
  };

  function renderMonth(overrides: Partial<React.ComponentProps<typeof MonthGrid>> = {}) {
    const props: React.ComponentProps<typeof MonthGrid> = {
      year: 2026,
      month0: 6,
      eventsByDay: new Map([["2026-7-15", [timed, deadline]]]),
      todayKey: "2026-7-11",
      selectedKey: "2026-7-15",
      onSelectDay: vi.fn(),
      onPrev: vi.fn(),
      onNext: vi.fn(),
      onToday: vi.fn(),
      ...overrides,
    };
    const view = render(<MonthGrid {...props} />);
    return { props, ...view };
  }

  it("renders a Sunday-first seven-column calendar", () => {
    const { container } = renderMonth();
    expect(container.querySelectorAll(".cal-dow")).toHaveLength(7);
    expect([...container.querySelectorAll(".cal-dow")].map((node) => node.textContent)).toEqual([
      "일",
      "월",
      "화",
      "수",
      "목",
      "금",
      "토",
    ]);
    expect(screen.getByText(/2026/)).toBeInTheDocument();
  });

  it("when makes only in-month days interactive", () => {
    const { container } = renderMonth();
    expect(screen.getAllByRole("button", { name: /7월 \d+일/ })).toHaveLength(31);
    expect(container.querySelectorAll(".cal-cell.out").length).toBeGreaterThan(0);
    for (const cell of container.querySelectorAll(".cal-cell.out")) {
      expect(cell).not.toHaveAttribute("role");
      expect(cell).not.toHaveAttribute("tabindex");
    }
  });

  it("when marks today, selection, and event count accessibly", () => {
    renderMonth();
    expect(screen.getByRole("button", { name: "7월 11일" })).toHaveClass("cal-today");
    const selected = screen.getByRole("button", { name: "7월 15일, 일정 2건" });
    expect(selected).toHaveClass("cal-selected");
    expect(selected).toHaveAttribute("aria-pressed", "true");
  });

  it("when styles timed and all-day deadline markers differently", () => {
    const { container } = renderMonth();
    const selected = screen.getByRole("button", { name: "7월 15일, 일정 2건" });
    expect(selected.querySelector(".cal-marker.dot.mine")).toBeInTheDocument();
    expect(selected.querySelector(".cal-marker.line.deadline")).toBeInTheDocument();
    expect(container.querySelectorAll(".cal-marker.overflow")).toHaveLength(0);
  });

  it("when caps visible markers and adds an overflow marker", () => {
    const events = Array.from({ length: 5 }, (_, index) => ({ ...timed, id: `event-${index}` }));
    const { container } = renderMonth({ eventsByDay: new Map([["2026-7-15", events]]) });
    const selected = screen.getByRole("button", { name: "7월 15일, 일정 5건" });
    expect(selected.querySelectorAll(".cal-marker.dot")).toHaveLength(3);
    expect(selected.querySelector(".cal-marker.overflow")).toBeInTheDocument();
    expect(container.querySelector('[title="일정 5건"]')).toBeInTheDocument();
  });

  it("when routes click, Enter, and Space selection", async () => {
    const { props } = renderMonth();
    const day = screen.getByRole("button", { name: "7월 20일" });
    await userEvent.click(day);
    fireEvent.keyDown(day, { key: "Enter" });
    fireEvent.keyDown(day, { key: " " });
    expect(props.onSelectDay).toHaveBeenCalledTimes(3);
    expect(props.onSelectDay).toHaveBeenNthCalledWith(1, "2026-7-20");
  });

  it("ignores unrelated key presses", () => {
    const { props } = renderMonth();
    fireEvent.keyDown(screen.getByRole("button", { name: "7월 20일" }), { key: "ArrowRight" });
    expect(props.onSelectDay).not.toHaveBeenCalled();
  });

  it("when routes previous, next, and today navigation", async () => {
    const { props } = renderMonth();
    await userEvent.click(screen.getByRole("button", { name: "이전 달" }));
    await userEvent.click(screen.getByRole("button", { name: "다음 달" }));
    await userEvent.click(screen.getByRole("button", { name: "오늘" }));
    expect(props.onPrev).toHaveBeenCalledOnce();
    expect(props.onNext).toHaveBeenCalledOnce();
    expect(props.onToday).toHaveBeenCalledOnce();
  });
});

describe("SessionDrawer", () => {
  const sessions: SessionRow[] = [
    { key: "client:main:a", label: "  첫 대화  ", model: "gpt-5", updatedAtMs: 1_783_730_000_000 },
    { key: "client:main:b", label: "   ", status: "idle" },
  ];

  function renderDrawer(overrides: Partial<React.ComponentProps<typeof SessionDrawer>> = {}) {
    const props: React.ComponentProps<typeof SessionDrawer> = {
      sessions,
      currentKey: "client:main:a",
      busy: false,
      error: "",
      onSelect: vi.fn(),
      onDelete: vi.fn(),
      onNew: vi.fn(),
      ...overrides,
    };
    const view = render(<SessionDrawer {...props} />);
    return { props, ...view };
  }

  it("shows a clear empty state", () => {
    renderDrawer({ sessions: [] });
    expect(screen.getByText("최근 대화가 없습니다.")).toBeInTheDocument();
    expect(screen.queryByRole("list")).not.toBeInTheDocument();
  });

  it("shows errors instead of the empty state", () => {
    renderDrawer({ sessions: [], error: "세션을 불러오지 못했습니다" });
    expect(screen.getByText("세션을 불러오지 못했습니다")).toHaveClass("error");
    expect(screen.queryByText("최근 대화가 없습니다.")).not.toBeInTheDocument();
  });

  it("when trims labels and falls back to the session key", () => {
    renderDrawer();
    expect(screen.getByText("첫 대화")).toBeInTheDocument();
    expect(screen.getByText("client:main:b")).toBeInTheDocument();
    expect(screen.getByText(/gpt-5/)).toBeInTheDocument();
  });

  it("when marks the current conversation", () => {
    const { container } = renderDrawer();
    const active = screen.getByText("첫 대화").closest("li");
    expect(active).toHaveClass("active");
    expect(container.querySelectorAll("li.active")).toHaveLength(1);
  });

  it("routes new, select, and delete actions", async () => {
    const { props } = renderDrawer();
    await userEvent.click(screen.getByRole("button", { name: /새 대화/ }));
    await userEvent.click(screen.getByText("첫 대화").closest("button")!);
    await userEvent.click(screen.getByRole("button", { name: "대화 삭제: 첫 대화" }));
    expect(props.onNew).toHaveBeenCalledOnce();
    expect(props.onSelect).toHaveBeenCalledWith("client:main:a");
    expect(props.onDelete).toHaveBeenCalledWith("client:main:a");
  });

  it("when disables every mutation while busy", () => {
    renderDrawer({ busy: true });
    expect(screen.getByRole("button", { name: /새 대화/ })).toBeDisabled();
    for (const button of screen.getAllByRole("button", { name: /첫 대화|client:main:b|대화 삭제/ })) {
      expect(button).toBeDisabled();
    }
  });

  it("marks the 업무 home and omits its delete control", () => {
    renderDrawer({
      sessions: [{ key: "client:main", label: "업무" }],
      currentKey: "client:main",
    });
    expect(screen.getByText("선제 보고가 모이는 업무 홈")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /대화 삭제/ })).not.toBeInTheDocument();
  });

  it("routes pin and model-reset actions", async () => {
    const onPin = vi.fn();
    const onResetModel = vi.fn();
    renderDrawer({
      onPin,
      onResetModel,
      sessions: [{ key: "client:main:a", label: "첫 대화", model: "gpt-5", pinned: false }],
    });
    await userEvent.click(screen.getByRole("button", { name: "위에 고정: 첫 대화" }));
    await userEvent.click(screen.getByRole("button", { name: "기본 모델로: 첫 대화" }));
    expect(onPin).toHaveBeenCalledWith("client:main:a", true);
    expect(onResetModel).toHaveBeenCalledWith("client:main:a");
  });

  it("merges remote search hits into the drawer list", async () => {
    const onSearch = vi
      .fn()
      .mockResolvedValue([{ sessionKey: "client:main:hit", label: "Remote hit", snippet: "body" }]);
    renderDrawer({ onSearch });
    await userEvent.type(screen.getByRole("textbox", { name: "대화 검색" }), "Remote");
    await waitFor(() => expect(onSearch).toHaveBeenCalledWith("Remote"));
    expect(screen.getByText("Remote hit")).toBeInTheDocument();
  });
});

describe("ModelPicker", () => {
  const models: ModelsList = {
    current: "gpt-5",
    roles: [],
    sections: [
      {
        title: "Cloud",
        models: [
          { id: "gpt-5", label: "GPT-5" },
          { id: "remote-down", label: "Remote", unhealthy: true },
        ],
      },
      { title: "Local", models: [{ id: "local", label: "Local Model" }] },
    ],
  };

  it("when stays hidden until valid sections arrive", () => {
    const { rerender } = render(<ModelPicker models={null} value="" onChange={() => {}} />);
    expect(screen.queryByRole("combobox", { name: "모델 선택" })).not.toBeInTheDocument();
    rerender(<ModelPicker models={{ current: "", roles: [], sections: [] }} value="" onChange={() => {}} />);
    expect(screen.queryByRole("combobox", { name: "모델 선택" })).not.toBeInTheDocument();
  });

  it("when groups models and marks unhealthy entries", () => {
    render(<ModelPicker models={models} value="gpt-5" onChange={() => {}} />);
    const picker = screen.getByRole("combobox", { name: "모델 선택" });
    expect(picker).toHaveValue("gpt-5");
    expect(picker.querySelectorAll("optgroup")).toHaveLength(2);
    expect(within(picker).getByRole("option", { name: "Remote ⚠" })).toBeInTheDocument();
  });

  it("surfaces an orphan active model instead of rendering blank", () => {
    render(<ModelPicker models={models} value="legacy-custom" onChange={() => {}} />);
    expect(screen.getByRole("combobox", { name: "모델 선택" })).toHaveValue("legacy-custom");
    expect(screen.getByRole("option", { name: "legacy-custom" })).toBeInTheDocument();
  });

  it("when routes changes and respects disabled state", async () => {
    const onChange = vi.fn();
    const { rerender } = render(<ModelPicker models={models} value="gpt-5" onChange={onChange} />);
    await userEvent.selectOptions(screen.getByRole("combobox", { name: "모델 선택" }), "local");
    expect(onChange).toHaveBeenCalledWith("local");
    rerender(<ModelPicker models={models} value="gpt-5" onChange={onChange} disabled />);
    expect(screen.getByRole("combobox", { name: "모델 선택" })).toBeDisabled();
  });
});

describe("icon primitives", () => {
  it.each<IconName>(["today", "chat", "attach", "calendar", "fleet", "refresh", "printer"])(
    "renders the %s path set as a decorative SVG",
    (name) => {
      const { container, unmount } = render(<Icon name={name} />);
      const svg = container.querySelector("svg")!;
      expect(svg).toHaveAttribute("aria-hidden", "true");
      expect(svg).toHaveAttribute("focusable", "false");
      expect(svg.querySelector("path, circle, rect")).toBeInTheDocument();
      unmount();
    },
  );

  it("when applies custom size, class, and stroke width", () => {
    const { container } = render(<Icon name="send" size={24} className="custom-icon" strokeWidth={2.5} />);
    expect(container.querySelector("svg")).toMatchObject({
      className: expect.objectContaining({ baseVal: "custom-icon" }),
    });
    expect(container.querySelector("svg")).toHaveAttribute("width", "24");
    expect(container.querySelector("svg")).toHaveAttribute("stroke-width", "2.5");
  });

  it("renders Deneb's decorative star with the requested geometry", () => {
    const { container } = render(<DenebStar size={32} />);
    expect(container.querySelector(".deneb-star")).toHaveAttribute("aria-hidden", "true");
    expect(container.querySelector("svg")).toHaveAttribute("width", "32");
    expect(container.querySelector("#deneb-sheen")).toBeInTheDocument();
    expect(container.querySelector(".deneb-star-core")).toBeInTheDocument();
    expect(container.querySelector("animateTransform")).toHaveAttribute("repeatCount", "indefinite");
  });
});
