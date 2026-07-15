import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  addDays,
  asBool,
  asNum,
  asStr,
  calSpan,
  calStamp,
  dayKey,
  dayLabel,
  errText,
  eventDayKeys,
  eventEndMs,
  eventTitle,
  firstString,
  fmtDate,
  fmtMailDate,
  fmtTime,
  hhmm,
  monthLabel,
  monthMatrix,
  relativeTime,
  senderName,
  startOfDay,
  text,
} from "./format";

describe("format primitive boundary contracts", () => {
  describe("strict primitive narrowing", () => {
    it.each([
      ["value", "value"],
      ["", ""],
      [0, undefined],
      [false, undefined],
      [null, undefined],
      [{ toString: () => "value" }, undefined],
    ])("when asStr(%o) => %o", (input, expected) => {
      expect(asStr(input)).toBe(expected);
    });

    it.each([
      [0, 0],
      [-1.5, -1.5],
      [Number.NaN, Number.NaN],
      [Number.POSITIVE_INFINITY, Number.POSITIVE_INFINITY],
      ["1", undefined],
      [null, undefined],
    ])("asNum(%o) preserves only number-typed values", (input, expected) => {
      if (Number.isNaN(expected)) expect(asNum(input)).toBeNaN();
      else expect(asNum(input)).toBe(expected);
    });

    it.each([
      [true, true],
      [false, false],
      [1, false],
      ["true", false],
      [{ valueOf: () => true }, false],
      [null, false],
    ])("when asBool(%o) accepts literal true only", (input, expected) => {
      expect(asBool(input)).toBe(expected);
    });
  });

  describe("first useful string selection", () => {
    it("skips blank earlier keys but preserves the original nonblank value", () => {
      expect(firstString({ name: "  ", email: " person@example.com " }, ["name", "email"])).toBe(
        " person@example.com ",
      );
    });

    it("without coerce number, boolean or nested-object fields", () => {
      expect(firstString({ a: 42, b: true, c: { text: "nested" } }, ["a", "b", "c"])).toBe("");
    });

    it.each([null, undefined, "string", 42, true])("returns empty for non-object input %o", (input) => {
      expect(firstString(input, ["name"])).toBe("");
    });

    it("when respects caller key precedence", () => {
      const value = { title: "Title", name: "Name", email: "email@example.com" };
      expect(firstString(value, ["email", "name", "title"])).toBe("email@example.com");
      expect(firstString(value, ["title", "name", "email"])).toBe("Title");
    });
  });

  describe("loose display text", () => {
    it.each([
      [false, "false"],
      [0, "0"],
      [42, "42"],
      [BigInt(7), "7"],
      [null, ""],
      [undefined, ""],
    ])("when text(%o) => %s", (input, expected) => {
      expect(text(input)).toBe(expected);
    });

    it("when uses title only after blank name and email", () => {
      expect(text({ name: "", email: " ", title: "Fallback title" })).toBe("Fallback title");
    });

    it("without leak generic object serialization", () => {
      expect(text({ unknown: "secret" })).toBe("");
      expect(text({ nested: { name: "secret" } })).toBe("");
    });
  });
});

describe("sender header edge cases", () => {
  it("when trims whitespace around a bare address", () => {
    expect(senderName("  person@example.com  ")).toBe("person@example.com");
  });

  it("uses the address when the display-name segment is whitespace", () => {
    expect(senderName("   <person@example.com>  ")).toBe("person@example.com");
  });

  it("when removes exactly one matching quote pair", () => {
    expect(senderName('"Kim, Lead" <kim@example.com>')).toBe("Kim, Lead");
    expect(senderName("'Kim' <kim@example.com>")).toBe("'Kim'");
  });

  it("preserves angle brackets that do not form a trailing address header", () => {
    expect(senderName("Team <ops@example.com> trailing")).toBe("Team <ops@example.com> trailing");
  });

  it("when supports legacy object title after blank name/email", () => {
    expect(senderName({ name: "", email: "", title: "Operations" })).toBe("Operations");
  });
});

describe("absolute and relative time boundaries", () => {
  const now = new Date(2026, 6, 11, 12, 0, 0).getTime();

  beforeEach(() => {
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(now);
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("formats epoch zero instead of treating it as missing", () => {
    expect(fmtDate(0)).not.toBe("");
    expect(fmtTime(0)).toMatch(/^\d{2}:\d{2}$/);
  });

  it("preserves numeric NaN and Infinity inspectable", () => {
    expect(fmtDate(Number.NaN)).toBe("NaN");
    expect(fmtTime(Number.POSITIVE_INFINITY)).toBe("Infinity");
  });

  it.each([
    [0, "방금"],
    [59_999, "방금"],
    [60_000, "1분 전"],
    [59 * 60_000 + 59_999, "59분 전"],
    [60 * 60_000, "1시간 전"],
    [5 * 60 * 60_000 + 59 * 60_000, "5시간 전"],
  ])("when fmtMailDate uses relative copy at age %i", (age, expected) => {
    expect(fmtMailDate(now - age, now)).toBe(expected);
  });

  it("when switches to absolute mail date at exactly six hours", () => {
    const stamp = now - 6 * 60 * 60_000;
    expect(fmtMailDate(stamp, now)).toBe(fmtDate(stamp));
  });

  it.each([
    [now + 1, "방금"],
    [now - 59_999, "방금"],
    [now - 60_000, "1분 전"],
    [now - 3_599_999, "59분 전"],
    [now - 3_600_000, "1시간 전"],
    [now - 86_399_999, "23시간 전"],
    [now - 86_400_000, "1일 전"],
    [now - 3 * 86_400_000, "3일 전"],
  ])("when relativeTime(%i) => %s", (stamp, expected) => {
    expect(relativeTime(stamp)).toBe(expected);
  });

  it.each([undefined, 0, -1, Number.NaN, Number.POSITIVE_INFINITY])(
    "relativeTime rejects invalid stamp %o",
    (stamp) => {
      expect(relativeTime(stamp)).toBe("");
    },
  );

  it("when labels local-day boundaries independently from elapsed hours", () => {
    expect(dayLabel(new Date(2026, 6, 10, 23, 59).getTime(), now)).toBe("어제");
    expect(dayLabel(new Date(2026, 6, 12, 0, 1).getTime(), now)).toBe("내일");
  });

  it("when floors dates before the Unix epoch to their local midnight", () => {
    const stamp = new Date(1969, 11, 31, 23, 59, 59).getTime();
    expect(startOfDay(stamp)).toBe(new Date(1969, 11, 31).getTime());
  });

  it.each([
    [new Date(2026, 0, 31).getTime(), 1, new Date(2026, 1, 1).getTime()],
    [new Date(2024, 1, 28).getTime(), 1, new Date(2024, 1, 29).getTime()],
    [new Date(2024, 1, 29).getTime(), 1, new Date(2024, 2, 1).getTime()],
    [new Date(2026, 0, 1).getTime(), -1, new Date(2025, 11, 31).getTime()],
  ])("addDays crosses calendar boundary from %i by %i", (start, delta, expected) => {
    expect(addDays(start, delta)).toBe(expected);
  });
});

describe("calendar stamp and display boundaries", () => {
  it("prefers object dateTime over date when both are supplied", () => {
    expect(calStamp({ dateTime: "2026-07-11T10:00:00Z", date: "2026-07-12" })).toEqual({
      iso: "2026-07-11T10:00:00Z",
      allDay: false,
    });
  });

  it("treats malformed timestamp objects as absent", () => {
    expect(calStamp({ dateTime: 42, date: true } as never)).toEqual({ allDay: false });
    expect(calStamp({})).toEqual({ allDay: false });
  });

  it.each([
    ["2026-7-1", false],
    ["2026-07-01 ", false],
    ["2026-07-01T00:00:00", false],
    ["2026-07-01", true],
  ])("when uses strict YYYY-MM-DD all-day detection for %s", (value, allDay) => {
    expect(calStamp(value).allDay).toBe(allDay);
  });

  it("uses summary even when it is an intentionally empty string", () => {
    expect(eventTitle({ id: "empty", summary: "", title: "legacy" })).toBe("");
  });

  it("when falls back from undefined summary to legacy title and then placeholder", () => {
    expect(eventTitle({ id: "legacy", title: "legacy" })).toBe("legacy");
    expect(eventTitle({ id: "missing" })).toBe("(제목 없음)");
  });

  it("returns no HH:MM for all-day, absent or malformed values", () => {
    expect(hhmm({ date: "2026-07-11" })).toBe("");
    expect(hhmm(undefined)).toBe("");
    expect(hhmm("not-a-time")).toBe("");
  });

  it("formats a timed local stamp without a date suffix", () => {
    expect(hhmm(new Date(2026, 6, 11, 7, 5).toISOString())).toBe("07:05");
  });

  it("normalizes overflowing month indexes through Date semantics", () => {
    expect(monthLabel(2026, 12)).toBe(monthLabel(2027, 0));
    expect(monthLabel(2026, -1)).toBe(monthLabel(2025, 11));
  });

  it("when uses non-padded local components in day keys", () => {
    expect(dayKey(new Date(2026, 0, 2))).toBe("2026-1-2");
    expect(dayKey(new Date(1999, 11, 31))).toBe("1999-12-31");
  });

  it("formats an all-day end earlier than start as a bounded display range", () => {
    const value = calSpan({ date: "2026-07-11" }, { date: "2026-07-10" });
    expect(value).toMatch(/(7월|Jul)/);
    expect(value).toContain("~");
  });

  it("formats a timed start-only event without a trailing range separator", () => {
    const value = calSpan("2026-07-11T10:00:00Z", undefined);
    expect(value).not.toMatch(/~\s*$/);
    expect(value).not.toBe("");
  });

  it("returns empty span when both stamps are missing", () => {
    expect(calSpan(undefined, undefined)).toBe("");
  });
});

describe("event coverage and termination safety", () => {
  it("clamps an all-day end before start to the start day", () => {
    expect(eventDayKeys({ date: "2026-07-11" }, { date: "2026-07-10" })).toEqual(["2026-7-11"]);
  });

  it("clamps a timed end before start to the start day", () => {
    expect(eventDayKeys("2026-07-11T10:00:00", "2026-07-09T10:00:00")).toEqual(["2026-7-11"]);
  });

  it("caps a malformed multi-year range to 62 local days", () => {
    const keys = eventDayKeys({ date: "2020-01-01" }, { date: "2030-01-01" });
    expect(keys).toHaveLength(62);
    expect(keys[0]).toBe("2020-1-1");
    expect(new Set(keys).size).toBe(62);
  });

  it("when covers timed events crossing local midnight on both days", () => {
    expect(eventDayKeys("2026-07-11T23:55:00", "2026-07-12T00:05:00")).toEqual(["2026-7-11", "2026-7-12"]);
  });

  it("when treats same-instant timed event as a single day", () => {
    expect(eventDayKeys("2026-07-11T10:00:00", "2026-07-11T10:00:00")).toEqual(["2026-7-11"]);
  });

  it("falls back to timed start when the end is malformed", () => {
    expect(eventEndMs("2026-07-11T10:00:00Z", "invalid")).toBe(Date.parse("2026-07-11T10:00:00Z"));
  });

  it("uses end instant even when it precedes start", () => {
    expect(eventEndMs("2026-07-11T10:00:00Z", "2026-07-10T10:00:00Z")).toBe(Date.parse("2026-07-10T10:00:00Z"));
  });

  it("uses next local midnight for a start-only all-day date at month end", () => {
    expect(eventEndMs({ date: "2026-01-31" }, undefined)).toBe(new Date(2026, 1, 1).getTime());
  });
});

describe("month matrix invariants", () => {
  it.each([
    [2026, 1, 28],
    [2024, 1, 29],
    [2026, 3, 30],
    [2026, 6, 31],
  ])("when covers every date in %i-%i (%i days)", (year, month0, days) => {
    const cells = monthMatrix(year, month0).flat();
    const keys = new Set(cells.map(dayKey));
    for (let day = 1; day <= days; day++) expect(keys).toContain(`${year}-${month0 + 1}-${day}`);
  });

  it.each([
    [2026, 1],
    [2024, 1],
    [2026, 4],
    [2026, 7],
  ])("always returns complete Sunday-to-Saturday weeks for %i-%i", (year, month0) => {
    const weeks = monthMatrix(year, month0);
    expect(weeks.length).toBeGreaterThanOrEqual(4);
    expect(weeks.length).toBeLessThanOrEqual(6);
    expect(weeks.every((week) => week.length === 7)).toBe(true);
    expect(weeks[0][0].getDay()).toBe(0);
    expect(weeks.at(-1)?.at(-1)?.getDay()).toBe(6);
  });

  it("returns fresh Date objects rather than aliasing cells", () => {
    const weeks = monthMatrix(2026, 6);
    const first = weeks[0][0];
    const second = weeks[0][1];
    const originalSecond = second.getTime();
    first.setDate(first.getDate() + 10);
    expect(second.getTime()).toBe(originalSecond);
  });
});

describe("error text normalization", () => {
  it.each([
    [new Error("when boom"), "when boom"],
    [{ message: 42 }, "42"],
    [{ message: null }, "null"],
    [404, "404"],
    [true, "true"],
    [undefined, "알 수 없는 오류"],
    [false, "알 수 없는 오류"],
    ["", "알 수 없는 오류"],
  ])("normalizes %o to %s", (input, expected) => {
    expect(errText(input)).toBe(expected);
  });

  it("does not throw for a message getter with a primitive value", () => {
    expect(
      errText({
        get message() {
          return "getter message";
        },
      }),
    ).toBe("getter message");
  });
});
