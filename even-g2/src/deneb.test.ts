import { describe, expect, it, vi, afterEach } from "vitest";
import {
  fetchGlance,
  formatAlertDetail,
  formatGeneratedLabel,
  httpMessage,
  listLabel,
  normalizeItems,
  normalizePages,
  readNotice,
} from "./deneb";
import { REQUEST_TIMEOUT_MS } from "./refresh";

const settings = { baseUrl: "http://gw.test", token: "t0k" };

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe("fetchGlance deadline", () => {
  // Without a deadline a stalled private-network request pins the app's `busy`
  // flag forever and every input handler (`if (busy) return`) stops responding.
  it("aborts a hanging request and reports it as a timeout", async () => {
    vi.useFakeTimers();
    vi.stubGlobal(
      "fetch",
      (_url: string, init?: RequestInit) =>
        new Promise((_resolve, reject) => {
          init?.signal?.addEventListener("abort", () => {
            const err = new Error("aborted");
            err.name = "AbortError";
            reject(err);
          });
        }),
    );
    const pending = fetchGlance(settings);
    const assertion = expect(pending).rejects.toThrow("시간 초과");
    await vi.advanceTimersByTimeAsync(REQUEST_TIMEOUT_MS + 100);
    await assertion;
  });

  it("passes an abort signal on every request", async () => {
    let sawSignal = false;
    vi.stubGlobal("fetch", async (_url: string, init?: RequestInit) => {
      sawSignal = !!init?.signal;
      return new Response(JSON.stringify({ text: "알림 없음" }), {
        status: 200,
      });
    });
    await fetchGlance(settings);
    expect(sawSignal).toBe(true);
  });

  it("surfaces the gateway error message on a non-2xx", async () => {
    // 500 and not 401: an auth failure is now deliberately REPLACED with a
    // message that says where to fix it, because the gateway's own 401 body is
    // "unauthorized" — English, and useless on a 576x288 display.
    vi.stubGlobal(
      "fetch",
      async () =>
        new Response(JSON.stringify({ error: { message: "워크피드 없음" } }), {
          status: 500,
        }),
    );
    await expect(fetchGlance(settings)).rejects.toThrow("워크피드 없음");
  });

  it("replaces an auth failure with somewhere to go", async () => {
    vi.stubGlobal(
      "fetch",
      async () =>
        new Response(JSON.stringify({ error: { message: "unauthorized" } }), {
          status: 401,
        }),
    );
    await expect(fetchGlance(settings)).rejects.toThrow("폰에서 토큰");
  });
});

describe("normalizePages", () => {
  it("falls back to a single home page when the gateway sends none", () => {
    expect(normalizePages(undefined, "알림 2건")).toEqual([
      { id: "home", title: "알림", text: "알림 2건" },
    ]);
  });

  it("drops pages with no id or blank text", () => {
    const got = normalizePages(
      [
        { id: "cal", title: "일정", text: " 14:00 회의 " },
        { id: "", title: "x", text: "y" },
        { id: "todo", title: "할 일", text: "   " },
      ],
      "fallback",
    );
    expect(got).toEqual([
      { id: "cal", title: "일정", text: "14:00 회의", empty: false },
    ]);
  });
});

describe("normalizeItems", () => {
  it("bounds the list and every field so one long body cannot blow the HUD", () => {
    const many = Array.from({ length: 30 }, (_, i) => ({
      id: `a${i}`,
      title: "ㄱ".repeat(80),
      body: "ㄴ".repeat(500),
    }));
    const got = normalizeItems(many);
    expect(got).toHaveLength(12);
    expect(Array.from(got[0].title)).toHaveLength(48);
    expect(Array.from(got[0].body!).length).toBeLessThanOrEqual(280);
  });

  it("synthesizes an id when the gateway omits one", () => {
    expect(normalizeItems([{ id: "", title: "알림" } as never])[0].id).toBe(
      "alert-1",
    );
  });

  it("drops entries with no title", () => {
    expect(normalizeItems([{ id: "a", title: "  " } as never])).toHaveLength(0);
  });
});

describe("HUD labels", () => {
  it("marks high priority so the wearer sees urgency in one glance", () => {
    expect(listLabel({ id: "a", title: "급건", priority: 4 })).toMatch(/^! /);
    expect(listLabel({ id: "b", title: "일반", priority: 1 })).toMatch(/^· /);
  });

  it("stays silent while fresh, speaks when stale or cached", () => {
    // Fresh is the normal state and saying so ("방금") was dead weight after
    // the clock — operator's call, 2026-07-27. Age earns a line only once it
    // could change how the wearer reads the screen; cache always does.
    expect(formatGeneratedLabel(new Date().toISOString(), false)).toBe("");
    const tenMinAgo = new Date(Date.now() - 10 * 60000).toISOString();
    expect(formatGeneratedLabel(tenMinAgo, false)).toBe("10분 전");
    expect(formatGeneratedLabel(new Date().toISOString(), true)).toBe(
      "방금·캐시",
    );
    expect(formatGeneratedLabel(undefined, true)).toBe("캐시");
    expect(formatGeneratedLabel("not-a-date", false)).toBe("");
  });
});

describe("formatAlertDetail", () => {
  const item = {
    id: "a1",
    title: "견적 회신 필요",
    body: "금호타이어 곡성",
    priority: 5,
    age: "10분",
  };

  it("promises the next ALERT while one is left", () => {
    const out = formatAlertDetail(item, { index: 0, total: 3 });
    expect(out).toContain("(1/3)");
    expect(out).toContain("↓다음알림");
  });

  it("promises the list on the last alert", () => {
    const out = formatAlertDetail(item, { index: 2, total: 3 });
    expect(out).toContain("(3/3)");
    expect(out).toContain("↓목록으로");
    expect(out).not.toContain("↓다음알림");
  });

  it("drops the counter when there is only one alert", () => {
    const out = formatAlertDetail(item, { index: 0, total: 1 });
    expect(out).not.toContain("(1/1)");
    expect(out).toContain("↓목록으로");
  });

  it("never claims a next PAGE — a swipe here has never done that", () => {
    expect(formatAlertDetail(item, { index: 0, total: 3 })).not.toContain(
      "다음페이지",
    );
    expect(formatAlertDetail(item)).not.toContain("다음페이지");
  });

  it("marks urgency and keeps the body", () => {
    const out = formatAlertDetail(item, { index: 0, total: 2 });
    expect(out).toContain("! 견적 회신 필요");
    expect(out).toContain("긴급");
    expect(out).toContain("금호타이어 곡성");
  });
});

describe("httpMessage", () => {
  it("turns an auth failure into somewhere to go", () => {
    // "HTTP 401" on a 576x288 display is not actionable; the phone has a form.
    expect(httpMessage(401)).toContain("폰에서 토큰");
    expect(httpMessage(403)).toContain("폰에서 토큰");
  });

  it("does not let a server message override the auth hint", () => {
    expect(httpMessage(401, "unauthorized")).toContain("폰에서 토큰");
  });

  it("keeps the gateway wording otherwise", () => {
    expect(httpMessage(500, "stub: induced failure")).toBe(
      "stub: induced failure",
    );
    expect(httpMessage(418)).toBe("HTTP 418");
  });
});

describe("readNotice", () => {
  it("keeps a usable notice", () => {
    expect(readNotice({ id: "17", text: " 견적 회신 보냈어요 " })).toEqual({
      id: "17",
      text: "견적 회신 보냈어요",
    });
  });

  it("refuses a notice with no id — it would re-show on every poll", () => {
    // The gateway keeps offering the same notice for a while so a dropped poll
    // cannot lose it; without an id the plugin cannot tell repeats apart.
    expect(readNotice({ text: "답변" })).toBeUndefined();
  });

  it("refuses a notice with nothing to say", () => {
    expect(readNotice({ id: "17", text: "   " })).toBeUndefined();
    expect(readNotice(undefined)).toBeUndefined();
  });

  it("bounds the text to what a HUD can hold", () => {
    const out = readNotice({ id: "1", text: "ㄱ".repeat(400) })!;
    expect(Array.from(out.text).length).toBeLessThanOrEqual(220);
    expect(out.text.endsWith("…")).toBe(true);
  });
});
