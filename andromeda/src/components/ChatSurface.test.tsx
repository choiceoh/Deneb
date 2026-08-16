import { createRef } from "react";
import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { AssistantBody, AssistantTurnActions } from "./AssistantBody";
import { ChatComposer, ScrollToBottomButton } from "./ChatComposer";
import type { AttachmentPart, ChatTurn, ToolPart } from "@/hooks";

function renderComposer(overrides: Partial<React.ComponentProps<typeof ChatComposer>> = {}) {
  const composeRef = createRef<HTMLTextAreaElement>();
  const fileRef = createRef<HTMLInputElement>();
  const props: React.ComponentProps<typeof ChatComposer> = {
    composeRef,
    fileRef,
    busy: false,
    stoppable: false,
    connected: true,
    input: "질문",
    placeholder: "무엇을 도와드릴까요?",
    onInput: vi.fn(),
    onSubmit: vi.fn(),
    onStop: vi.fn(),
    onPick: vi.fn(),
    onAttachFiles: vi.fn(),
    ...overrides,
  };
  const view = render(<ChatComposer {...props} />);
  return { props, composeRef, fileRef, ...view };
}

describe("ChatComposer", () => {
  it("renders the text input and hidden multi-file picker", () => {
    const { container, composeRef, fileRef } = renderComposer();

    expect(composeRef.current).toBe(screen.getByRole("textbox", { name: "Deneb에게 메시지" }));
    expect(composeRef.current).toHaveValue("질문");
    expect(composeRef.current).toHaveAttribute("placeholder", "무엇을 도와드릴까요?");
    expect(fileRef.current).toBe(container.querySelector('input[type="file"]'));
    expect(fileRef.current).toHaveAttribute("multiple");
    expect(fileRef.current).toHaveAttribute("accept", expect.stringContaining(".pdf"));
  });

  it("returns textarea changes", async () => {
    const { props } = renderComposer({ input: "" });
    await userEvent.type(screen.getByRole("textbox"), "답");
    expect(props.onInput).toHaveBeenCalledWith("답");
  });

  it("when submits a nonblank connected message", async () => {
    const { props } = renderComposer();

    await userEvent.click(screen.getByRole("button", { name: "전송" }));

    expect(props.onSubmit).toHaveBeenCalledTimes(1);
  });

  it.each([
    [false, "질문"],
    [true, ""],
    [true, "   \n\t"],
  ])("without send for connected=%s input=%j", (connected, input) => {
    renderComposer({ connected, input });
    expect(screen.getByRole("button", { name: "전송" })).toBeDisabled();
  });

  it.each(["질문", "  질문  "])("allows send for meaningful input %j", (input) => {
    renderComposer({ connected: true, input });
    expect(screen.getByRole("button", { name: "전송" })).toBeEnabled();
  });

  it("submits Enter without adding a newline", () => {
    const { props } = renderComposer();
    const editor = screen.getByRole("textbox");

    fireEvent.keyDown(editor, { key: "Enter" });

    expect(props.onSubmit).toHaveBeenCalledTimes(1);
  });

  it("preserves Shift+Enter for multiline composition", () => {
    const { props } = renderComposer();
    fireEvent.keyDown(screen.getByRole("textbox"), { key: "Enter", shiftKey: true });
    expect(props.onSubmit).not.toHaveBeenCalled();
  });

  it("without submit an IME composition confirmation", () => {
    const { props } = renderComposer();
    fireEvent.keyDown(screen.getByRole("textbox"), { key: "Enter", isComposing: true });
    expect(props.onSubmit).not.toHaveBeenCalled();
  });

  it("ignores ordinary keyboard input", () => {
    const { props } = renderComposer();
    fireEvent.keyDown(screen.getByRole("textbox"), { key: "a" });
    fireEvent.keyDown(screen.getByRole("textbox"), { key: "Escape" });
    expect(props.onSubmit).not.toHaveBeenCalled();
  });

  it("when opens the hidden file picker through the attachment affordance", async () => {
    const { fileRef } = renderComposer();
    const click = vi.spyOn(fileRef.current!, "click");

    await userEvent.click(screen.getByRole("button", { name: "파일 첨부" }));

    expect(click).toHaveBeenCalledTimes(1);
  });

  it.each([
    [false, false],
    [true, true],
  ])("without attachment for connected=%s busy=%s", (connected, busy) => {
    renderComposer({ connected, busy });
    expect(screen.getByRole("button", { name: "파일 첨부" })).toBeDisabled();
  });

  it("when forwards file-picker changes", () => {
    const { props, fileRef } = renderComposer();
    const file = new File(["hello"], "hello.txt", { type: "text/plain" });

    fireEvent.change(fileRef.current!, { target: { files: [file] } });

    expect(props.onPick).toHaveBeenCalledTimes(1);
    expect((props.onPick as ReturnType<typeof vi.fn>).mock.calls[0][0].target.files[0]).toBe(file);
  });

  it("when turns pasted files into attachments and suppresses content paste", () => {
    const { props } = renderComposer();
    const file = new File(["image"], "shot.png", { type: "image/png" });
    const event = new Event("paste", { bubbles: true, cancelable: true });
    Object.defineProperty(event, "clipboardData", { value: { files: [file] } });

    screen.getByRole("textbox").dispatchEvent(event);

    expect(event.defaultPrevented).toBe(true);
    expect(props.onAttachFiles).toHaveBeenCalledWith([file]);
  });

  it("preserves ordinary text paste untouched", () => {
    const { props } = renderComposer();
    const event = new Event("paste", { bubbles: true, cancelable: true });
    Object.defineProperty(event, "clipboardData", { value: { files: [] } });

    screen.getByRole("textbox").dispatchEvent(event);

    expect(event.defaultPrevented).toBe(false);
    expect(props.onAttachFiles).not.toHaveBeenCalled();
  });

  it("shows attachment ignores feedback as a live status", () => {
    renderComposer({ note: "big.pdf — 크기 초과" });
    expect(screen.getByRole("status")).toHaveTextContent("big.pdf — 크기 초과");
  });

  it("omits an empty attachment note", () => {
    renderComposer({ note: "" });
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });

  it("shows an honest stop control during a streamed response", async () => {
    const { props } = renderComposer({ busy: true, stoppable: true });

    expect(screen.getByRole("textbox")).toBeDisabled();
    expect(screen.getByRole("textbox")).toHaveAttribute("placeholder", "응답 중…");
    await userEvent.click(screen.getByRole("button", { name: "중단" }));
    expect(props.onStop).toHaveBeenCalledTimes(1);
  });

  it("displays a disabled progress control for non-abortable capture", () => {
    renderComposer({ busy: true, stoppable: false });

    const progress = screen.getByRole("button", { name: "첨부 분석 중" });
    expect(progress).toBeDisabled();
    expect(progress).toHaveAttribute("title", "첨부 분석 중에는 중단할 수 없습니다");
    expect(screen.queryByRole("button", { name: "중단" })).not.toBeInTheDocument();
  });
});

describe("ScrollToBottomButton", () => {
  it("renders nothing while the transcript is pinned", () => {
    const { container } = render(<ScrollToBottomButton visible={false} onClick={() => {}} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("returns to the latest message", async () => {
    const onClick = vi.fn();
    render(<ScrollToBottomButton visible onClick={onClick} />);
    const button = screen.getByRole("button", { name: "맨 아래로" });
    expect(button).toHaveAttribute("title", "맨 아래로");
    await userEvent.click(button);
    expect(onClick).toHaveBeenCalledTimes(1);
  });
});

function turn(overrides: Partial<ChatTurn> = {}): ChatTurn {
  return {
    id: "assistant-1",
    role: "assistant",
    text: "답변",
    status: "done",
    ...overrides,
  };
}

describe("AssistantBody", () => {
  it("renders the thinking indicator before stream content arrives", () => {
    render(
      <AssistantBody
        turn={turn({ text: "", parts: [], status: "streaming" })}
        thinking="자료 확인 중"
        onUiSubmit={() => {}}
        busy
      />,
    );
    expect(screen.getByText("생각 중…")).toBeInTheDocument();
    expect(screen.getByText(/자료 확인 중/)).toBeInTheDocument();
  });

  it("trims empty thinking previews out of the status", () => {
    const { container } = render(
      <AssistantBody
        turn={turn({ text: "", parts: [], status: "streaming" })}
        thinking="   "
        onUiSubmit={() => {}}
        busy
      />,
    );
    expect(container.querySelector(".deneb-status-summary")).toBeNull();
  });

  it("renders a transcript-loaded turn from canonical text", () => {
    render(<AssistantBody turn={turn({ text: "**완료**", parts: undefined })} onUiSubmit={() => {}} busy={false} />);
    expect(screen.getByText("완료").tagName).toBe("STRONG");
  });

  it("keeps streamed text parts in order", () => {
    const { container } = render(
      <AssistantBody
        turn={turn({
          parts: [
            { kind: "text", text: "첫 문장" },
            { kind: "text", text: "둘째 문장" },
          ],
        })}
        onUiSubmit={() => {}}
        busy={false}
      />,
    );
    expect(Array.from(container.querySelectorAll("p"), (node) => node.textContent)).toEqual(["첫 문장", "둘째 문장"]);
  });

  it("keeps the progress row visible after answer text starts streaming", () => {
    render(
      <AssistantBody
        turn={turn({
          text: "부분",
          parts: [{ kind: "text", text: "부분" }],
          status: "streaming",
          startedAt: Date.now(),
        })}
        thinking="답변을 작성하고 있습니다"
        onUiSubmit={() => {}}
        busy
      />,
    );
    expect(screen.getByText(/답변을 작성하고 있습니다/)).toHaveClass("deneb-status-summary");
  });

  it("renders tool activity inline with text", () => {
    const tool: ToolPart = {
      kind: "tool",
      id: "tool-1",
      tool: "calendar.list",
      state: "completed",
      detail: "3건",
    };
    render(
      <AssistantBody
        turn={turn({ parts: [{ kind: "text", text: "조회" }, tool, { kind: "text", text: "완료" }] })}
        onUiSubmit={() => {}}
        busy={false}
      />,
    );
    expect(screen.getByText("calendar list")).toBeInTheDocument();
    expect(screen.getByText("3건")).toBeInTheDocument();
    expect(screen.getByRole("img", { name: "완료" })).toBeInTheDocument();
  });

  it.each([
    ["image", "이미지 분석"],
    ["audio", "녹음 전사"],
    ["document", "문서 추출"],
  ] as const)("when labels %s attachment results", (captureKind, label) => {
    const attachment: AttachmentPart = {
      kind: "attachment",
      id: "attachment-1",
      filename: "input.bin",
      mimeType: "application/octet-stream",
      captureKind,
      caption: "이것을 읽어줘",
      text: "추출 결과",
      state: "completed",
    };
    render(<AssistantBody turn={turn({ parts: [attachment] })} onUiSubmit={() => {}} busy={false} />);

    const group = screen.getByRole("group", { name: "첨부 분석 결과" });
    expect(group).toHaveTextContent(label);
    expect(group).toHaveTextContent("input.bin");
    expect(group).toHaveTextContent("application/octet-stream");
    expect(group).toHaveTextContent("이것을 읽어줘");
    expect(group).toHaveTextContent("추출 결과");
    expect(group).toHaveTextContent("완료");
  });

  it("marks a failed attachment visibly and accessibly", () => {
    const attachment: AttachmentPart = {
      kind: "attachment",
      id: "attachment-1",
      filename: "broken.pdf",
      mimeType: "application/pdf",
      captureKind: "document",
      text: "파싱 실패",
      state: "error",
      isError: true,
    };
    render(<AssistantBody turn={turn({ parts: [attachment] })} onUiSubmit={() => {}} busy={false} />);
    const group = screen.getByRole("group", { name: "첨부 분석 결과" });
    expect(group).toHaveClass("error");
    expect(group).toHaveTextContent("실패");
    expect(group).not.toHaveTextContent("설명");
  });
});

describe("AssistantTurnActions", () => {
  it("renders no actions for a user turn", () => {
    const { container } = render(
      <AssistantTurnActions turn={turn({ role: "user" })} lastId="assistant-1" busy={false} onRegenerate={() => {}} />,
    );
    expect(container).toBeEmptyDOMElement();
  });

  it("offers regeneration only for the last completed streamed answer", async () => {
    const onRegenerate = vi.fn();
    render(
      <AssistantTurnActions
        turn={turn({ parts: [{ kind: "text", text: "답" }] })}
        lastId="assistant-1"
        busy={false}
        onRegenerate={onRegenerate}
      />,
    );
    await userEvent.click(screen.getByRole("button", { name: /다시 생성/ }));
    expect(onRegenerate).toHaveBeenCalledTimes(1);
  });

  it.each([
    ["another", false, "done", undefined],
    ["assistant-1", true, "done", undefined],
    ["assistant-1", false, "streaming", undefined],
    ["assistant-1", false, "done", false],
  ] as const)("hides regeneration outside its eligibility contract", (lastId, busy, status, canRegenerate) => {
    render(
      <AssistantTurnActions
        turn={turn({ parts: [{ kind: "text", text: "답" }], status, canRegenerate })}
        lastId={lastId}
        busy={busy}
        onRegenerate={() => {}}
      />,
    );
    expect(screen.queryByRole("button", { name: /다시 생성/ })).not.toBeInTheDocument();
  });

  it("does not regenerate a transcript-loaded turn without streamed parts", () => {
    render(
      <AssistantTurnActions
        turn={turn({ parts: undefined })}
        lastId="assistant-1"
        busy={false}
        onRegenerate={() => {}}
      />,
    );
    expect(screen.queryByRole("button", { name: /다시 생성/ })).not.toBeInTheDocument();
  });

  it.each([
    [turn({ text: "인쇄할 답", parts: undefined }), true],
    [turn({ text: "", parts: [{ kind: "text", text: "부분" }] }), true],
    [turn({ text: "  ", parts: undefined }), false],
    [turn({ text: "답", status: "streaming" }), false],
  ] as const)("displays print only for completed visible content", (value, expected) => {
    render(<AssistantTurnActions turn={value} busy={false} onRegenerate={() => {}} />);
    expect(Boolean(screen.queryByRole("button", { name: /인쇄/ }))).toBe(expected);
  });

  it("when prints the containing assistant turn", async () => {
    window.print = vi.fn();
    const { container } = render(
      <article className="ai-turn">
        <AssistantTurnActions turn={turn({ text: "답" })} busy={false} onRegenerate={() => {}} />
      </article>,
    );

    await userEvent.click(screen.getByRole("button", { name: /인쇄/ }));

    expect(window.print).toHaveBeenCalledTimes(1);
    expect(container.querySelector(".ai-turn")).toHaveClass("deneb-print-region");
  });

  it("when appends surface-specific actions", () => {
    render(
      <AssistantTurnActions turn={turn({ text: "답" })} busy={false} onRegenerate={() => {}}>
        <button>노트에 저장</button>
      </AssistantTurnActions>,
    );
    expect(screen.getByRole("button", { name: "노트에 저장" })).toBeInTheDocument();
  });
});
