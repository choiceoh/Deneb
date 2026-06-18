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
    get: "miniapp.gmail.get",
    remove: "miniapp.gmail.trash",
  },
  {
    name: "calendar",
    label: "일정",
    list: "miniapp.calendar.list_upcoming",
    get: "miniapp.calendar.get",
    create: "miniapp.calendar.create",
    update: "miniapp.calendar.update",
    remove: "miniapp.calendar.delete",
  },
  // Read-mostly resources — parameterless lists flow straight into a grid.
  { name: "people", label: "연락처", list: "miniapp.people.list" },
  {
    name: "crons",
    label: "크론",
    list: "miniapp.crons.list",
    get: "miniapp.crons.get",
    update: "miniapp.crons.update",
    remove: "miniapp.crons.remove",
  },
  { name: "workfeed", label: "작업피드", list: "miniapp.workfeed.list" },
];

// memory(위키) and search are NOT in the CRUD registry: their reads are
// query-driven (memory.search/get_page, search.all) rather than parameterless
// lists, so dedicated panes call these RPCs directly (DESIGN §9).
export const MEMORY_RPC = {
  search: "miniapp.memory.search",
  getPage: "miniapp.memory.get_page",
  writePage: "miniapp.memory.write_page",
} as const;

export const SEARCH_RPC = "miniapp.search.all";

export const RESOURCE_MAP: Record<string, ResourceDef> = Object.fromEntries(RESOURCE_DEFS.map((r) => [r.name, r]));

export function resourceDef(name: string): ResourceDef {
  const r = RESOURCE_MAP[name];
  if (!r) throw new Error(`andromeda: unknown resource "${name}"`);
  return r;
}

// Metadata for <Refine resources={...}> — keeps Refine's resource awareness in
// sync with the registry without hand-maintaining a second list.
export const refineResources = RESOURCE_DEFS.map((r) => ({ name: r.name, meta: { label: r.label } }));
