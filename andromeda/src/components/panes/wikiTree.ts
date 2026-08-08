import type { WikiPage } from "@/types";

// wikiTree — pure helpers that fold the gateway's flat page list into the
// folder hierarchy the wiki actually has on disk (프로젝트/<이름>/{대표.md,
// 로그.md, 기자재/, 메일분석/…}). No React, fully unit-testable.

export interface WikiTreeFile {
  path: string; // full wiki path, the open/read key
  name: string; // filename segment (e.g. "대표.md")
  title?: string; // frontmatter title when the gateway enriched the row
}

export interface WikiTreeFolder {
  path: string; // folder path ("" for the root)
  name: string; // last segment ("프로젝트", "영산고", …) — the navigation key
  folders: WikiTreeFolder[];
  files: WikiTreeFile[];
  count: number; // pages anywhere beneath (shown as the folder badge)
  // Korean label to render instead of `name`, set when the folder holds a 대표
  // page whose title differs from the segment — i.e. exactly the code-named
  // project folders ("pl2-kia-epc-001" → "기아 화성 국유지 태양광"). Same rule
  // as the gateway's alias (wiki/display_alias.go): the alias IS the 대표 title.
  label?: string;
}

// Fixed per-project slots render with their slot name (instantly recognizable
// structure) and sort ahead of ordinary pages; everything else shows its
// title. Mirrors the gateway's project layout (wiki/project_layout.go).
const SLOT_ORDER: Record<string, number> = { "대표.md": 0, "로그.md": 1, "로그-보관.md": 2 };
const REP_PAGE = "대표.md";

/** What to render for a folder: its Korean alias when it has one, else the
 *  raw path segment. `path`/`name` stay the navigation keys either way. */
export function folderLabel(folder: WikiTreeFolder): string {
  return folder.label ?? folder.name;
}

export function fileLabel(file: WikiTreeFile): string {
  if (file.name in SLOT_ORDER) return stripMd(file.name);
  return file.title?.trim() || stripMd(file.name);
}

export function stripMd(name: string): string {
  return name.endsWith(".md") ? name.slice(0, -3) : name;
}

// buildWikiTree folds flat rows into a nested tree. Folders sort by Korean
// locale; files sort slots-first then by label. Rows without a path are
// dropped (nothing to open).
export function buildWikiTree(pages: WikiPage[]): WikiTreeFolder {
  const root: WikiTreeFolder = { path: "", name: "", folders: [], files: [], count: 0 };
  const folderIndex = new Map<string, WikiTreeFolder>([["", root]]);

  const folderFor = (path: string): WikiTreeFolder => {
    const hit = folderIndex.get(path);
    if (hit) return hit;
    const cut = path.lastIndexOf("/");
    const parent = folderFor(cut < 0 ? "" : path.slice(0, cut));
    const node: WikiTreeFolder = {
      path,
      name: cut < 0 ? path : path.slice(cut + 1),
      folders: [],
      files: [],
      count: 0,
    };
    parent.folders.push(node);
    folderIndex.set(path, node);
    return node;
  };

  for (const page of pages) {
    const path = (page.path ?? "").trim().replace(/\\/g, "/").replace(/^\/+/, "");
    if (!path) continue;
    const cut = path.lastIndexOf("/");
    const folder = folderFor(cut < 0 ? "" : path.slice(0, cut));
    folder.files.push({ path, name: cut < 0 ? path : path.slice(cut + 1), title: page.title });
    // Every ancestor's badge counts this page.
    for (let node: WikiTreeFolder | undefined = folder; node;) {
      node.count += 1;
      const at = node.path.lastIndexOf("/");
      node = node.path === "" ? undefined : folderIndex.get(at < 0 ? "" : node.path.slice(0, at));
    }
  }

  // Fold each folder's 대표 title in as its Korean label before sorting, so the
  // tree sorts by what the reader actually sees rather than by opaque codes.
  const labelDeep = (node: WikiTreeFolder) => {
    const rep = node.files.find((f) => f.name === REP_PAGE)?.title?.trim();
    if (rep && rep !== node.name) node.label = rep;
    node.folders.forEach(labelDeep);
  };
  labelDeep(root);

  const sortDeep = (node: WikiTreeFolder) => {
    node.folders.sort((a, b) => folderLabel(a).localeCompare(folderLabel(b), "ko"));
    node.files.sort((a, b) => {
      const ra = SLOT_ORDER[a.name] ?? 9;
      const rb = SLOT_ORDER[b.name] ?? 9;
      if (ra !== rb) return ra - rb;
      return fileLabel(a).localeCompare(fileLabel(b), "ko");
    });
    node.folders.forEach(sortDeep);
  };
  sortDeep(root);
  return root;
}

// ancestorsOf lists every folder path above a page path, outermost first —
// what the pane expands so the newly opened page is visible in the tree.
export function ancestorsOf(path: string): string[] {
  const out: string[] = [];
  let at = path.indexOf("/");
  while (at >= 0) {
    out.push(path.slice(0, at));
    at = path.indexOf("/", at + 1);
  }
  return out;
}
