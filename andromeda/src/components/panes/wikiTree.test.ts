import { describe, expect, it } from "vitest";
import { ancestorsOf, buildWikiTree, fileLabel, folderLabel } from "./wikiTree";

describe("buildWikiTree", () => {
  it("folds flat paths into nested folders with recursive counts", () => {
    const root = buildWikiTree([
      { path: "프로젝트/영산고/대표.md", title: "영산고" },
      { path: "프로젝트/영산고/로그.md", title: "영산고 진행 로그" },
      { path: "프로젝트/영산고/메일분석/19e8717314b5c914.md", title: "RE: 견적" },
      { path: "프로젝트/부산8호/대표.md", title: "부산8호" },
      { path: "업무/구리값-동향.md", title: "구리값 동향" },
      { path: "daily mail analysis.md", title: "일일 메일 분석" }, // root-level file
    ]);

    expect(root.count).toBe(6);
    expect(root.files.map((f) => f.name)).toEqual(["daily mail analysis.md"]);
    expect(root.folders.map((f) => f.name)).toEqual(["업무", "프로젝트"]);

    const project = root.folders.find((f) => f.name === "프로젝트")!;
    expect(project.count).toBe(4);
    expect(project.folders.map((f) => f.name)).toEqual(["부산8호", "영산고"]);

    const yeongsan = project.folders.find((f) => f.name === "영산고")!;
    expect(yeongsan.count).toBe(3);
    expect(yeongsan.folders.map((f) => f.name)).toEqual(["메일분석"]);
    expect(yeongsan.files.map((f) => f.name)).toEqual(["대표.md", "로그.md"]);
  });

  it("when orders slot files (대표→로그→로그-보관) ahead of ordinary pages", () => {
    const root = buildWikiTree([
      { path: "프로젝트/영산고/구조검토.md", title: "구조 검토" },
      { path: "프로젝트/영산고/로그-보관.md", title: "영산고 로그 보관" },
      { path: "프로젝트/영산고/로그.md", title: "영산고 진행 로그" },
      { path: "프로젝트/영산고/대표.md", title: "영산고" },
    ]);
    const files = root.folders[0].folders[0].files;
    expect(files.map((f) => f.name)).toEqual(["대표.md", "로그.md", "로그-보관.md", "구조검토.md"]);
  });

  it("drops rows without a path and normalizes separators", () => {
    const root = buildWikiTree([
      { title: "경로 없음" },
      { path: "프로젝트\\영산고\\대표.md", title: "영산고" }, // windows separators
    ]);
    expect(root.count).toBe(1);
    expect(root.folders[0].folders[0].files[0].path).toBe("프로젝트/영산고/대표.md");
  });
});

describe("fileLabel", () => {
  it("displays slot names for fixed slots and titles for ordinary pages", () => {
    expect(fileLabel({ path: "p/대표.md", name: "대표.md", title: "영산고" })).toBe("대표");
    expect(fileLabel({ path: "p/로그.md", name: "로그.md", title: "영산고 진행 로그" })).toBe("로그");
    expect(fileLabel({ path: "p/x.md", name: "x.md", title: "기아 화성 모듈 RFX" })).toBe("기아 화성 모듈 RFX");
    expect(fileLabel({ path: "p/slug-name.md", name: "slug-name.md" })).toBe("slug-name");
  });
});

describe("folderLabel", () => {
  it("shows the 대표 title for code-named project folders, sorting by it", () => {
    const root = buildWikiTree([
      { path: "프로젝트/pl2-kia-epc-001/대표.md", title: "기아 화성 국유지 태양광" },
      { path: "프로젝트/pl2-kia-epc-001/메일분석/m1.md", title: "Re: FW: 설치계획도" },
      { path: "프로젝트/nde-ztt-cbl-001/대표.md", title: "비금도 154kV 해저케이블" },
    ]);
    const project = root.folders.find((f) => f.name === "프로젝트")!;

    // Codes stay the navigation keys; labels are what the reader sees, and the
    // ordering follows the labels (기 < 비 in ko collation) — by code it would
    // be the other way round (nde- < pl2-).
    expect(project.folders.map(folderLabel)).toEqual(["기아 화성 국유지 태양광", "비금도 154kV 해저케이블"]);
    expect(project.folders.map((f) => f.name)).toEqual(["pl2-kia-epc-001", "nde-ztt-cbl-001"]);

    // Slot folders under a project keep their own name — only the project
    // folder itself carries a 대표 page.
    const kia = project.folders.find((f) => f.name === "pl2-kia-epc-001")!;
    expect(folderLabel(kia.folders.find((f) => f.name === "메일분석")!)).toBe("메일분석");
  });

  it("falls back to the segment for descriptive folders and taxonomy buckets", () => {
    const root = buildWikiTree([
      { path: "프로젝트/영산고/대표.md", title: "영산고" }, // title == folder name
      { path: "업무/구리값-동향.md", title: "구리값 동향" }, // no 대표 page
    ]);
    expect(folderLabel(root.folders.find((f) => f.name === "업무")!)).toBe("업무");
    const project = root.folders.find((f) => f.name === "프로젝트")!;
    expect(folderLabel(project.folders.find((f) => f.name === "영산고")!)).toBe("영산고");
  });
});

describe("ancestorsOf", () => {
  it("when lists every folder above a page, outermost first", () => {
    expect(ancestorsOf("프로젝트/영산고/메일분석/abc.md")).toEqual([
      "프로젝트",
      "프로젝트/영산고",
      "프로젝트/영산고/메일분석",
    ]);
    expect(ancestorsOf("루트파일.md")).toEqual([]);
  });
});
