import { afterEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { EventAnalysis } from "./EventAnalysis";
import type { CalEvent } from "@/types";

const cfg = { url: "http://gateway.test", token: "secret" };

function calendarEvent(overrides: Partial<CalEvent> = {}): CalEvent {
  return {
    id: "event-1",
    summary: "설계 리뷰 회의",
    description: "변경안을 검토하고 결정한다",
    start: "2099-07-11T09:00:00+09:00",
    end: "2099-07-11T10:00:00+09:00",
    location: "회의실 A",
    ...overrides,
  };
}

function sse(...frames: Array<[event: string, data: Record<string, unknown>]>): Response {
  const body = frames.map(([event, data]) => `event: ${event}\ndata: ${JSON.stringify(data)}\n\n`).join("");
  return new Response(body, {
    status: 200,
    headers: { "Content-Type": "text/event-stream" },
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("EventAnalysis fallback", () => {
  it("renders a meeting-oriented preparation summary before AI output", () => {
    render(<EventAnalysis event={calendarEvent()} connected={false} cfg={cfg} />);

    expect(screen.getByText("설계 리뷰 회의", { selector: ".calendar-analysis-title" })).toBeInTheDocument();
    expect(screen.getByText(/안건, 결정할 항목, 공유 자료를 미리 정리하세요/)).toBeInTheDocument();
    expect(screen.getByText(/회의 후 결정사항과 담당자를 남기는 것이 좋습니다/)).toBeInTheDocument();
    expect(screen.getByText(/앞뒤 일정과 이동 시간이 겹치지 않는지 확인하세요/)).toBeInTheDocument();
  });

  it("renders a deadline-oriented fallback", () => {
    render(
      <EventAnalysis
        event={calendarEvent({
          summary: "제안서 제출 마감",
          description: "고객 포털에 최종본 제출",
          location: undefined,
        })}
        connected={false}
        cfg={cfg}
      />,
    );

    expect(screen.getByText(/마감 산출물과 제출 경로를 먼저 확인하세요/)).toBeInTheDocument();
    expect(screen.getByText(/완료 여부를 체크하고 관련 할일을 닫으세요/)).toBeInTheDocument();
    expect(screen.getByText(/장소 정보가 없으면 직전에 확인 비용이 생길 수 있습니다/)).toBeInTheDocument();
  });

  it("when describes a multi-day schedule", () => {
    render(
      <EventAnalysis
        event={calendarEvent({
          summary: "워크숍",
          description: undefined,
          start: { date: "2099-07-11" },
          end: { date: "2099-07-14" },
          allDay: true,
        })}
        connected={false}
        cfg={cfg}
      />,
    );
    expect(screen.getByText(/3일에 걸친 일정입니다/)).toBeInTheDocument();
  });

  it("when describes a single all-day schedule", () => {
    render(
      <EventAnalysis
        event={calendarEvent({
          summary: "휴무",
          description: undefined,
          start: { date: "2099-07-11" },
          end: { date: "2099-07-12" },
          allDay: true,
        })}
        connected={false}
        cfg={cfg}
      />,
    );
    expect(screen.getByText(/종일 일정입니다/)).toBeInTheDocument();
  });

  it("when marks a past schedule and focuses on follow-up", () => {
    render(
      <EventAnalysis
        event={calendarEvent({
          summary: "지난 작업",
          description: undefined,
          start: "2020-01-01T09:00:00Z",
          end: "2020-01-01T10:00:00Z",
        })}
        connected={false}
        cfg={cfg}
      />,
    );
    expect(screen.getByText(/이미 끝난 일정입니다/)).toBeInTheDocument();
    expect(screen.getByText(/후속 기록이나 액션 아이템만 남기면 됩니다/)).toBeInTheDocument();
  });

  it("when disables AI refresh while disconnected", () => {
    render(<EventAnalysis event={calendarEvent({ description: undefined })} connected={false} cfg={cfg} />);
    expect(screen.getByRole("button", { name: "AI 분석" })).toBeDisabled();
  });

  it("does not auto-request analysis without a description", async () => {
    const fetch = vi.fn();
    vi.stubGlobal("fetch", fetch);
    render(<EventAnalysis event={calendarEvent({ description: "   " })} connected cfg={cfg} />);
    await Promise.resolve();
    expect(fetch).not.toHaveBeenCalled();
  });
});

describe("EventAnalysis streaming", () => {
  it("auto-streams analysis for an event with title and description", async () => {
    const fetch = vi.fn(async () =>
      sse(
        ["delta", { delta: "준비 자료를 " }],
        ["delta", { delta: "확인하세요." }],
        ["done", { text: "준비 자료를 확인하세요." }],
      ),
    );
    vi.stubGlobal("fetch", fetch);

    render(<EventAnalysis event={calendarEvent()} connected cfg={cfg} />);

    expect(await screen.findByText("준비 자료를 확인하세요.")).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByText("AI 분석 중…")).not.toBeInTheDocument());
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it("when sends a scoped session and semantic event context", async () => {
    const fetch = vi.fn(async (_url: string, _init: RequestInit) => sse(["done", { text: "분석 완료" }]));
    vi.stubGlobal("fetch", fetch);
    const event = calendarEvent();

    render(<EventAnalysis event={event} connected cfg={cfg} />);
    await screen.findByText("분석 완료");

    const [url, init] = fetch.mock.calls[0];
    expect(url).toBe("http://gateway.test/api/v1/miniapp/chat/stream");
    expect(init.headers).toEqual(expect.objectContaining({ "X-Deneb-Client-Token": "secret" }));
    const body = JSON.parse(String(init.body));
    expect(body.sessionKey).toBe("calendar:inline:event-1");
    expect(body.message).toContain("[선택한 일정]");
    expect(body.message).toContain("제목: 설계 리뷰 회의");
    expect(body.message).toContain("장소: 회의실 A");
    expect(body.message).toContain("설명:\n변경안을 검토하고 결정한다");
    expect(body.message).toContain("[요청]");
  });

  it("uses final text when the stream has no deltas", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => sse(["done", { text: "최종 분석만 도착" }])),
    );
    render(<EventAnalysis event={calendarEvent()} connected cfg={cfg} />);
    expect(await screen.findByText("최종 분석만 도착")).toBeInTheDocument();
  });

  it("keeps streamed deltas when done carries a different aggregate", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => sse(["delta", { delta: "스트림 본문" }], ["done", { text: "다른 최종 본문" }])),
    );
    render(<EventAnalysis event={calendarEvent()} connected cfg={cfg} />);
    expect(await screen.findByText("스트림 본문")).toBeInTheDocument();
    expect(screen.queryByText("다른 최종 본문")).not.toBeInTheDocument();
  });

  it("surfaces an SSE error frame without discarding the fallback", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => sse(["error", { error: "model overloaded" }])),
    );
    render(<EventAnalysis event={calendarEvent()} connected cfg={cfg} />);

    expect(await screen.findByText("AI 분석 실패: model overloaded")).toBeInTheDocument();
    expect(screen.getByText(/안건, 결정할 항목, 공유 자료를 미리 정리하세요/)).toBeInTheDocument();
  });

  it("surfaces transport failures", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response("unavailable", { status: 503 })),
    );
    render(<EventAnalysis event={calendarEvent()} connected cfg={cfg} />);
    expect(await screen.findByText("AI 분석 실패: chat/stream: HTTP 503")).toBeInTheDocument();
  });

  it("allows manual analysis for a descriptionless event", async () => {
    const fetch = vi.fn(async () => sse(["done", { text: "수동 분석" }]));
    vi.stubGlobal("fetch", fetch);
    render(<EventAnalysis event={calendarEvent({ description: undefined })} connected cfg={cfg} />);

    await userEvent.click(screen.getByRole("button", { name: "AI 분석" }));

    expect(await screen.findByText("수동 분석")).toBeInTheDocument();
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it("when changes the action label while and after analysis", async () => {
    let resolve!: (response: Response) => void;
    vi.stubGlobal(
      "fetch",
      vi.fn(() => new Promise<Response>((done) => (resolve = done))),
    );
    render(<EventAnalysis event={calendarEvent({ description: undefined })} connected cfg={cfg} />);

    await userEvent.click(screen.getByRole("button", { name: "AI 분석" }));
    expect(screen.getByRole("button", { name: "다시 분석" })).toBeDisabled();
    resolve(sse(["done", { text: "분석됨" }]));

    expect(await screen.findByText("분석됨")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "다시 분석" })).toBeEnabled();
  });

  it("resets the previous answer when the selected event changes", async () => {
    const fetch = vi
      .fn()
      .mockResolvedValueOnce(sse(["done", { text: "첫 분석" }]))
      .mockResolvedValueOnce(sse(["done", { text: "둘째 분석" }]));
    vi.stubGlobal("fetch", fetch);
    const { rerender } = render(<EventAnalysis event={calendarEvent()} connected cfg={cfg} />);
    await screen.findByText("첫 분석");

    rerender(<EventAnalysis event={calendarEvent({ id: "event-2", summary: "새 회의" })} connected cfg={cfg} />);

    expect(await screen.findByText("둘째 분석")).toBeInTheDocument();
    expect(screen.queryByText("첫 분석")).not.toBeInTheDocument();
  });

  it("aborts an in-flight request when the component unmounts", async () => {
    let capturedSignal: AbortSignal | undefined;
    vi.stubGlobal(
      "fetch",
      vi.fn((_url: string, init?: RequestInit) => {
        capturedSignal = init?.signal ?? undefined;
        return new Promise<Response>(() => {});
      }),
    );
    const { unmount } = render(<EventAnalysis event={calendarEvent()} connected cfg={cfg} />);
    await waitFor(() => expect(capturedSignal).toBeDefined());

    unmount();

    expect(capturedSignal?.aborted).toBe(true);
  });

  it("renders a card-leading analysis as a deneb-ui card, not a raw code block", async () => {
    // Regression: the analysis is a STREAMED agent answer and the agent may lead
    // with a ```deneb-ui card. Rendering it through plain <Markdown> leaked the
    // fence as a copyable code block — the same defect #4088 fixed on the mail
    // analysis, still open on this surface.
    const card =
      "```deneb-ui\n" +
      '<column><card><text style="title">준비 상태</text>' +
      '<text style="body">계약서 검토 미완</text></card></column>\n' +
      "```\n\n## 다음 할 일\n\n법무 회신을 기다립니다.";
    const fetch = vi.fn(async () => sse(["delta", { delta: card }], ["done", { text: card }]));
    vi.stubGlobal("fetch", fetch);

    const { container } = render(<EventAnalysis event={calendarEvent()} connected cfg={cfg} />);

    // The card renders through the deneb-ui renderer (.dui root) and the prose
    // tail survives; the literal fence must not reach the user.
    await waitFor(() => expect(container.querySelector(".dui")).toBeTruthy());
    expect(screen.getByText("계약서 검토 미완")).toBeInTheDocument();
    expect(screen.getByText(/다음 할 일/)).toBeInTheDocument();
    expect(container.querySelector("pre code")).toBeNull();
    expect(container.textContent).not.toContain("```deneb-ui");
  });
});
