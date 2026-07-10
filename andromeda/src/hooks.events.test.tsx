import { act, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { GatewayConfig } from "@/gateway";
import { EVENTS_RETRY_BASE_MS, EVENTS_RETRY_MAX_MS, eventsRetryDelay, useEvents } from "@/hooks";
import { renderWithProviders } from "@/test/util";

const CFG: GatewayConfig = { url: "http://test", token: "tok" };

function EventsProbe() {
  const { events, status } = useEvents(CFG, true);
  return (
    <>
      <output data-testid="events-status">{status}</output>
      <output data-testid="events-count">{events.length}</output>
    </>
  );
}

function closedResponse(body = ""): Response {
  const encoder = new TextEncoder();
  return new Response(
    new ReadableStream<Uint8Array>({
      start(controller) {
        if (body) controller.enqueue(encoder.encode(body));
        controller.close();
      },
    }),
    { status: 200, headers: { "Content-Type": "text/event-stream" } },
  );
}

function controlledResponse() {
  let close = () => {};
  let cancelled = 0;
  const response = new Response(
    new ReadableStream<Uint8Array>({
      start(controller) {
        close = () => controller.close();
      },
      cancel() {
        cancelled += 1;
      },
    }),
    { status: 200, headers: { "Content-Type": "text/event-stream" } },
  );
  return { response, close: () => close(), cancelled: () => cancelled };
}

async function flushAsyncWork() {
  await Promise.resolve();
  await Promise.resolve();
  await Promise.resolve();
}

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  localStorage.clear();
});

describe("useEvents reconnect loop", () => {
  it("caps exponential retry delays", () => {
    expect(eventsRetryDelay(0)).toBe(EVENTS_RETRY_BASE_MS);
    expect(eventsRetryDelay(1)).toBe(EVENTS_RETRY_BASE_MS * 2);
    expect(eventsRetryDelay(99)).toBe(EVENTS_RETRY_MAX_MS);
  });

  it("re-subscribes after an unexpected EOF and receives the next stream", async () => {
    vi.useFakeTimers();
    const live = controlledResponse();
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(closedResponse())
      .mockResolvedValueOnce(closedResponse('data: {"id":"e2","kind":"workfeed","title":"복구됨"}\n\n'))
      .mockResolvedValueOnce(live.response);
    vi.stubGlobal("fetch", fetchMock);

    const view = renderWithProviders(<EventsProbe />, { connected: true, cfg: CFG });
    await act(flushAsyncWork);

    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId("events-status")).toHaveTextContent(/1초 후 재연결/);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(EVENTS_RETRY_BASE_MS);
      await flushAsyncWork();
    });

    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(screen.getByTestId("events-count")).toHaveTextContent("1");

    view.unmount();
  });

  it("backs off pre-connect failures and resets to the base delay after a successful open", async () => {
    vi.useFakeTimers();
    const recovered = controlledResponse();
    const finalStream = controlledResponse();
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(new Response("nope", { status: 500 }))
      .mockResolvedValueOnce(new Response("still nope", { status: 503 }))
      .mockResolvedValueOnce(recovered.response)
      .mockResolvedValueOnce(finalStream.response);
    vi.stubGlobal("fetch", fetchMock);

    const view = renderWithProviders(<EventsProbe />, { connected: true, cfg: CFG });
    await act(flushAsyncWork);
    expect(fetchMock).toHaveBeenCalledTimes(1);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(EVENTS_RETRY_BASE_MS);
      await flushAsyncWork();
    });
    expect(fetchMock).toHaveBeenCalledTimes(2);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(EVENTS_RETRY_BASE_MS);
      await flushAsyncWork();
    });
    expect(fetchMock).toHaveBeenCalledTimes(2);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(EVENTS_RETRY_BASE_MS);
      await flushAsyncWork();
    });
    expect(fetchMock).toHaveBeenCalledTimes(3);
    expect(screen.getByTestId("events-status")).toHaveTextContent("수신 중");

    await act(async () => {
      recovered.close();
      await flushAsyncWork();
    });
    expect(screen.getByTestId("events-status")).toHaveTextContent(/1초 후 재연결/);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(EVENTS_RETRY_BASE_MS);
      await flushAsyncWork();
    });
    expect(fetchMock).toHaveBeenCalledTimes(4);

    view.unmount();
  });

  it("aborts the active reader on unmount without scheduling another subscription", async () => {
    vi.useFakeTimers();
    const live = controlledResponse();
    const fetchMock = vi.fn<typeof fetch>().mockResolvedValue(live.response);
    vi.stubGlobal("fetch", fetchMock);

    const view = renderWithProviders(<EventsProbe />, { connected: true, cfg: CFG });
    await act(flushAsyncWork);
    expect(screen.getByTestId("events-status")).toHaveTextContent("수신 중");
    expect(fetchMock).toHaveBeenCalledTimes(1);

    view.unmount();
    await act(flushAsyncWork);
    await vi.advanceTimersByTimeAsync(EVENTS_RETRY_MAX_MS * 2);

    expect(live.cancelled()).toBe(1);
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });
});
