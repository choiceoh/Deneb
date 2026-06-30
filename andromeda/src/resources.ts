// Resource registry — the single source of truth for resource↔RPC wiring.
//
// Each entry maps a Refine resource to its Deneb miniapp.* methods. The data
// provider (dataProvider.ts) and Refine's <Refine resources> list are both
// DERIVED from this array, so adding a Phase 2 resource (memory/wiki, people,
// crons, workfeed, search — DESIGN §5) is a one-line change here.
export interface ResourceDef {
  name: string;
  label: string;
  list: string;
  // Payload key wrapping the row array: gateway list RPCs return
  // { <listKey>: [...] } (+ pagination/meta fields), not a bare array. The data
  // provider unwraps by this key. Omit only when the RPC returns a bare array.
  listKey?: string;
  get?: string; // dedicated single-record read (else getOne falls back to list+find)
  create?: string;
  update?: string;
  remove?: string;
}

export const RESOURCE_DEFS: ResourceDef[] = [
  {
    name: "todo",
    label: "할일",
    list: "miniapp.todo.list",
    listKey: "todos",
    create: "miniapp.todo.create",
    update: "miniapp.todo.update",
    remove: "miniapp.todo.delete",
  },
  // Mail is read-mostly here; archive/trash/analyze are dedicated AI-driven
  // actions rather than generic CRUD, so the grid only wires list + get + trash.
  {
    name: "mail",
    label: "메일",
    list: "miniapp.gmail.list_recent",
    listKey: "messages",
    get: "miniapp.gmail.get",
    remove: "miniapp.gmail.trash",
  },
  {
    name: "calendar",
    label: "일정",
    list: "miniapp.calendar.list_upcoming",
    listKey: "events",
    get: "miniapp.calendar.get",
    create: "miniapp.calendar.create",
    update: "miniapp.calendar.update",
    remove: "miniapp.calendar.delete",
  },
  {
    name: "calendar-range",
    label: "일정 범위",
    list: "miniapp.calendar.list_range",
    listKey: "events",
  },
  // Read-mostly resources — parameterless lists flow straight into a grid.
  { name: "people", label: "연락처", list: "miniapp.people.list", listKey: "people" },
  {
    name: "crons",
    label: "크론",
    list: "miniapp.crons.list",
    listKey: "jobs",
    get: "miniapp.crons.get",
    update: "miniapp.crons.update",
    remove: "miniapp.crons.remove",
  },
  { name: "workfeed", label: "작업피드", list: "miniapp.workfeed.list", listKey: "items" },
  // Project progress digests (Deneb #2834) — a parameterless read, so it flows
  // straight into a grid/card pane like people/workfeed. Rows carry no id; the
  // pane keys on `path`/`project`.
  { name: "progress", label: "진행", list: "miniapp.project.digests", listKey: "digests" },
  // Skill catalog (miniapp.skills.list) — a parameterless list of the skills the
  // agent can use. detail/lifecycle/update/delete are query-driven actions below.
  { name: "skills", label: "스킬", list: "miniapp.skills.list", listKey: "skills" },
];

// memory(위키) and search are NOT in the CRUD registry: their reads are
// query-driven (memory.search/get_page, search.all) rather than parameterless
// lists, so dedicated panes call these RPCs directly (DESIGN §9).
export const MEMORY_RPC = {
  search: "miniapp.memory.search",
  getPage: "miniapp.memory.get_page",
  writePage: "miniapp.memory.write_page",
  createPage: "miniapp.memory.create_page",
  categories: "miniapp.memory.categories",
  listInCategory: "miniapp.memory.list_in_category",
  diaryRecent: "miniapp.memory.diary_recent",
  movePage: "miniapp.memory.move_page",
  merge: "miniapp.memory.merge",
  deletePages: "miniapp.memory.delete_pages",
} as const;

export const FILES_RPC = {
  list: "miniapp.files.list",
  search: "miniapp.files.search",
  share: "miniapp.files.share",
  upload: "miniapp.files.upload",
  delete: "miniapp.files.delete",
  mkdir: "miniapp.files.mkdir",
  move: "miniapp.files.move",
} as const;

export const SEARCH_RPC = "miniapp.search.all";

// Server-side project↔item matching: given a project 대표페이지 path, returns the
// IDs of linked items per type. The ProjectHomePane filters its already-fetched
// lists by these IDs instead of running a local heuristic.
export const PROJECT_LINKED_RPC = "miniapp.project.linked";

// Deneb 노트북 — the NotebookPane browses (list/get) and writes (create/delete a
// notebook, pin/unpin a citation source) directly via these miniapp RPCs.
export const NOTEBOOK_RPC = {
  list: "miniapp.notebook.list",
  get: "miniapp.notebook.get",
  create: "miniapp.notebook.create",
  delete: "miniapp.notebook.delete",
  addSource: "miniapp.notebook.add_source",
  removeSource: "miniapp.notebook.remove_source",
} as const;

// Action RPCs that don't fit generic CRUD (no id+fields update / delete shape).
// Panes call these directly via useAction → callRpc, mirroring the native client.
export const MAIL_RPC = {
  markRead: "miniapp.gmail.mark_read",
  archive: "miniapp.gmail.archive",
  trash: "miniapp.gmail.trash",
  analyze: "miniapp.gmail.analyze",
  analysisCached: "miniapp.gmail.analysis_cached",
  senderContext: "miniapp.gmail.sender_context",
  ask: "miniapp.gmail.ask",
} as const;

export const CRON_RPC = {
  run: "miniapp.crons.run",
  update: "miniapp.crons.update",
  remove: "miniapp.crons.remove",
} as const;

// Skill detail/lifecycle reads + guarded local-skill mutations. The list flows
// through the resource registry (parameterless); these take params, so the pane
// calls them directly via callRpc — mirroring the native skills tab.
export const SKILLS_RPC = {
  detail: "miniapp.skills.detail",
  lifecycle: "miniapp.skills.lifecycle",
  update: "miniapp.skills.update",
  delete: "miniapp.skills.delete",
} as const;

export const WORKFEED_RPC = {
  ack: "miniapp.workfeed.ack",
  read: "miniapp.workfeed.read",
  actionRun: "miniapp.workfeed.action.run",
  answer: "miniapp.workfeed.answer",
  feedback: "miniapp.workfeed.feedback",
  rewrite: "miniapp.workfeed.rewrite",
} as const;

export const RESOURCE_MAP: Record<string, ResourceDef> = Object.fromEntries(RESOURCE_DEFS.map((r) => [r.name, r]));

export function resourceDef(name: string): ResourceDef {
  const r = RESOURCE_MAP[name];
  if (!r) throw new Error(`andromeda: unknown resource "${name}"`);
  return r;
}

// Metadata for <Refine resources={...}> — keeps Refine's resource awareness in
// sync with the registry without hand-maintaining a second list.
export const refineResources = RESOURCE_DEFS.map((r) => ({ name: r.name, meta: { label: r.label } }));
