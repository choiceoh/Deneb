import { describe, expect, it } from "vitest";
import { matchEntities } from "./entityMatch";

const source = {
  projects: [
    { name: "완도 관산포 태양광", path: "프로젝트/완도-관산포" },
    { name: "부산8호 RPS 사업", path: "프로젝트/부산8호" },
    { name: "짧음", path: "프로젝트/짧음" },
  ],
  people: [
    { name: "김세미", path: "인물/김세미" },
    { name: "공명한", path: "인물/공명한" },
  ],
};

describe("matchEntities", () => {
  it("finds the project named inside an approval title", () => {
    const hits = matchEntities("완도 관산포 태양광 프로젝트 관련 금전대여의 건", source);
    expect(hits[0]).toMatchObject({ kind: "project", path: "프로젝트/완도-관산포" });
  });

  it("prefers longer names and dedupes by path", () => {
    const hits = matchEntities("완도 관산포 태양광 완도 관산포 태양광 김세미 기안", source, 3);
    expect(hits.map((h) => h.path)).toEqual(["프로젝트/완도-관산포", "인물/김세미"]);
  });

  it("ignores too-short names and non-matching text", () => {
    expect(matchEntities("짧음 언급", source)).toEqual([]);
    expect(matchEntities("무관한 제목", source)).toEqual([]);
  });

  it("caps results at the limit", () => {
    const hits = matchEntities("완도 관산포 태양광 부산8호 RPS 사업 김세미 공명한", source, 2);
    expect(hits).toHaveLength(2);
  });
});
