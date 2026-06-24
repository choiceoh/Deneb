import { describe, expect, it } from "vitest";
import {
  calSpan,
  calStamp,
  dayKey,
  errText,
  eventDayKeys,
  eventEndMs,
  fmtDate,
  fmtMailDate,
  monthMatrix,
  senderName,
  text,
} from "./format";

describe("text", () => {
  it("returns strings as-is", () => {
    expect(text("hi")).toBe("hi");
  });
  it("falls back past an empty name to the email", () => {
    expect(text({ name: "", email: "person@example.com" })).toBe("person@example.com");
  });
  it("prefers a present name", () => {
    expect(text({ name: "Kim", email: "k@e.com" })).toBe("Kim");
  });
  it("is empty for null/empty objects", () => {
    expect(text(null)).toBe("");
    expect(text({})).toBe("");
  });
});

describe("senderName", () => {
  it('drops the address from a "Name <addr>" header', () => {
    expect(senderName("홍길동 <hong@x.com>")).toBe("홍길동");
    expect(senderName("Andromeda Team <team@andromeda.io>")).toBe("Andromeda Team");
  });
  it("unquotes a quoted display name", () => {
    expect(senderName('"Doe, John" <j@x.com>')).toBe("Doe, John");
  });
  it("falls back to the address when there is no display name", () => {
    expect(senderName("hong@x.com")).toBe("hong@x.com");
    expect(senderName("<hong@x.com>")).toBe("hong@x.com");
  });
  it("handles the legacy { name, email } object (name, else email)", () => {
    expect(senderName({ name: "Kim", email: "k@e.com" })).toBe("Kim");
    expect(senderName({ name: "", email: "k@e.com" })).toBe("k@e.com");
  });
  it("is empty for null/empty input", () => {
    expect(senderName(null)).toBe("");
    expect(senderName("")).toBe("");
  });
});

describe("fmtDate", () => {
  it("is empty for undefined", () => {
    expect(fmtDate(undefined)).toBe("");
  });
  it("passes through unparseable input", () => {
    expect(fmtDate("not a date")).toBe("not a date");
  });
});

describe("fmtMailDate", () => {
  const now = new Date("2026-06-23T12:00:00Z").getTime();
  it("shows '방금' under a minute", () => {
    expect(fmtMailDate(new Date(now - 30_000).toISOString(), now)).toBe("방금");
  });
  it("shows minutes under an hour", () => {
    expect(fmtMailDate(new Date(now - 45 * 60_000).toISOString(), now)).toBe("45분 전");
  });
  it("shows hours within six hours", () => {
    expect(fmtMailDate(new Date(now - 3 * 3_600_000).toISOString(), now)).toBe("3시간 전");
  });
  it("falls back to the absolute date at/after six hours", () => {
    const v = new Date(now - 6 * 3_600_000).toISOString();
    expect(fmtMailDate(v, now)).toBe(fmtDate(v));
  });
  it("falls back to the absolute date for future timestamps", () => {
    const v = new Date(now + 3_600_000).toISOString();
    expect(fmtMailDate(v, now)).toBe(fmtDate(v));
  });
  it("passes through empty and unparseable input", () => {
    expect(fmtMailDate(undefined, now)).toBe("");
    expect(fmtMailDate("not a date", now)).toBe("not a date");
  });
});

describe("calStamp", () => {
  it("flags { date } and bare YYYY-MM-DD as all-day", () => {
    expect(calStamp({ date: "2026-06-17" })).toEqual({ iso: "2026-06-17", allDay: true });
    expect(calStamp("2026-06-17")).toEqual({ iso: "2026-06-17", allDay: true });
  });
  it("flags { dateTime } and full ISO as timed", () => {
    expect(calStamp({ dateTime: "2026-06-17T10:00:00Z" })).toEqual({ iso: "2026-06-17T10:00:00Z", allDay: false });
    expect(calStamp("2026-06-17T10:00:00Z").allDay).toBe(false);
  });
});

describe("calSpan", () => {
  it("renders a single all-day event as one date (no range)", () => {
    const span = calSpan({ date: "2026-06-17" }, { date: "2026-06-18" });
    expect(span).not.toContain("~");
    expect(span).toContain("17");
  });
  it("steps back Google's exclusive all-day end.date", () => {
    const span = calSpan({ date: "2026-06-17" }, { date: "2026-06-20" });
    expect(span).toContain("~");
    expect(span).toContain("19"); // 20 is exclusive → last inclusive day is 19
    expect(span).not.toContain("20");
  });
  it("renders timed events as a range", () => {
    const span = calSpan("2026-06-17T10:00:00Z", "2026-06-17T11:00:00Z");
    expect(span).toContain("~");
  });
});

describe("monthMatrix", () => {
  it("returns Sunday-first weeks that cover the whole month", () => {
    const weeks = monthMatrix(2026, 5); // June 2026
    const flat = weeks.flat();
    expect(weeks[0]).toHaveLength(7);
    expect(flat[0].getDay()).toBe(0); // first cell is a Sunday
    expect(flat[flat.length - 1].getDay()).toBe(6); // last cell is a Saturday
    // every day of June is present
    const keys = new Set(flat.map(dayKey));
    for (let d = 1; d <= 30; d++) expect(keys.has(`2026-6-${d}`)).toBe(true);
  });
});

describe("eventDayKeys", () => {
  it("is a single day for a timed event without an end", () => {
    expect(eventDayKeys("2026-06-18T05:00:00", undefined)).toEqual(["2026-6-18"]);
  });
  it("spans all-day events, stepping back the exclusive end.date", () => {
    expect(eventDayKeys({ date: "2026-06-22" }, { date: "2026-06-24" })).toEqual(["2026-6-22", "2026-6-23"]);
  });
  it("is one day for a single all-day event (end is exclusive)", () => {
    expect(eventDayKeys({ date: "2026-06-22" }, { date: "2026-06-23" })).toEqual(["2026-6-22"]);
  });
  it("is empty when the start is missing", () => {
    expect(eventDayKeys(undefined, undefined)).toEqual([]);
  });
});

describe("eventEndMs", () => {
  it("returns the end instant for a timed event", () => {
    expect(eventEndMs("2026-06-17T10:00:00Z", "2026-06-17T11:00:00Z")).toBe(Date.parse("2026-06-17T11:00:00Z"));
  });
  it("treats the exclusive all-day end.date as the over-instant (local midnight)", () => {
    expect(eventEndMs({ date: "2026-06-22" }, { date: "2026-06-23" })).toBe(new Date(2026, 5, 23).getTime());
  });
  it("ends an all-day event with no end at the next local midnight", () => {
    expect(eventEndMs({ date: "2026-06-22" }, undefined)).toBe(new Date(2026, 5, 23).getTime());
  });
  it("is null when there is no usable time", () => {
    expect(eventEndMs(undefined, undefined)).toBeNull();
  });
});

describe("errText", () => {
  it("reads .message off error-shaped objects", () => {
    expect(errText({ message: "boom" })).toBe("boom");
  });
  it("passes strings through and defaults on nullish", () => {
    expect(errText("x")).toBe("x");
    expect(errText(null)).toBe("알 수 없는 오류");
  });
});
