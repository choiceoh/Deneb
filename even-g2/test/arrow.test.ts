import { describe, expect, it } from "vitest";
import { maneuverArrow } from "../src/arrow";

// The classifier is the whole safety-relevant decision: a wrong arrow points a
// wearer the wrong way at a junction. The cases below are the ACTUAL strings the
// gateway produced against the live TMap API on 2026-07-26 (서울시청 → 강남역
// 도보, → 코엑스 자동차), not invented examples — the previous round of this
// integration shipped three defects precisely because the fixtures were guesses.
describe("maneuverArrow", () => {
  const cases: Array<[string, ReturnType<typeof maneuverArrow>]> = [
    ["68m 앞 좌회전", "left"],
    ["34m 앞 우회전", "right"],
    ["310m 직진", "straight"],
    ["560m 앞 유턴", "uturn"],
    ["목적지", "goal"],
    ["도착했습니다", "goal"],
    // Folding crossings onto a direction is the point: on foot these are the
    // most common maneuver, and "which side" is what the wearer acts on.
    ["171m 앞 우측 횡단보도", "right"],
    ["13m 앞 좌측 횡단보도", "left"],
    // No direction to draw — an arrow here would be invented.
    ["출발", null],
    ["경유지", null],
    ["250m 이동", null],
    ["", null],
    ["   ", null],
  ];
  for (const [input, want] of cases) {
    it(`${JSON.stringify(input)} → ${want}`, () => {
      expect(maneuverArrow(input)).toBe(want);
    });
  }

  it("prefers u-turn over a side mentioned alongside it", () => {
    // "좌측 U턴" contains 좌; turning back is still the maneuver, and drawing a
    // plain left arrow would send the wearer into the cross street.
    expect(maneuverArrow("좌측 U턴")).toBe("uturn");
  });

  it("prefers the destination over any direction in the same line", () => {
    expect(maneuverArrow("우회전 후 목적지")).toBe("goal");
  });
});
