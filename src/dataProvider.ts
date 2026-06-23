import type { DataProvider } from "@refinedev/core";
import { type GatewayConfig, callRpc } from "./gateway";
import { resourceDef } from "./resources";

// Gateway list RPCs wrap the rows in a payload object keyed by the resource
// (e.g. { people: [...] }, { todos: [...] }, { items: [...] }) alongside
// pagination/meta fields — NOT a bare array. Unwrap by the registry's listKey,
// falling back to the sole array-valued property so a resource works before its
// key is declared, and still accepting a bare array (the mock gateway shape).
function unwrapRows(payload: unknown, listKey?: string): any[] {
  if (Array.isArray(payload)) return payload;
  if (payload && typeof payload === "object") {
    const obj = payload as Record<string, unknown>;
    if (listKey && Array.isArray(obj[listKey])) return obj[listKey] as any[];
    for (const v of Object.values(obj)) if (Array.isArray(v)) return v as any[];
  }
  return [];
}

function rpcParams(meta: unknown): Record<string, unknown> {
  if (!meta || typeof meta !== "object") return {};
  const params = (meta as { rpcParams?: unknown }).rpcParams;
  return params && typeof params === "object" && !Array.isArray(params) ? (params as Record<string, unknown>) : {};
}

// Deneb-backed Refine data provider. Resource↔RPC wiring lives in resources.ts;
// this file is just the generic glue from Refine's CRUD contract to callRpc().
//
// RPC payloads are dynamic, so the provider works in `any` at this boundary and
// Refine re-applies the caller's TData on the way out (its CRUD methods are
// generic over caller-supplied TData).
export function denebDataProvider(cfg: GatewayConfig): DataProvider {
  return {
    getApiUrl: () => cfg.url,

    getList: async ({ resource, meta }) => {
      const def = resourceDef(resource);
      const payload = await callRpc<unknown>(cfg, def.list, rpcParams(meta));
      const data = unwrapRows(payload, def.listKey);
      const payloadMeta = payload as Record<string, unknown> | null;
      const total = payloadMeta && typeof payloadMeta.total === "number" ? payloadMeta.total : data.length;
      return { data, total };
    },

    getOne: async ({ resource, id }) => {
      const m = resourceDef(resource);
      // mail/calendar expose a dedicated get; todo has none, so fall back to list+find.
      if (m.get) {
        const data = await callRpc<any>(cfg, m.get, { id });
        return { data: data ?? { id } };
      }
      const payload = await callRpc<unknown>(cfg, m.list, {});
      const found = unwrapRows(payload, m.listKey).find((r) => String(r.id) === String(id));
      return { data: found ?? { id } };
    },

    create: async ({ resource, variables }) => {
      const m = resourceDef(resource);
      if (!m.create) throw new Error(`${resource}: create unsupported`);
      const data = await callRpc<any>(cfg, m.create, variables as Record<string, unknown>);
      return { data };
    },

    update: async ({ resource, id, variables }) => {
      const v = variables as Record<string, unknown>;
      // A todo `done` toggle maps to the dedicated set_done RPC; other fields to update.
      if (resource === "todo" && "done" in v) {
        const data = await callRpc<any>(cfg, "miniapp.todo.set_done", { id, done: v.done });
        return { data };
      }
      const m = resourceDef(resource);
      if (!m.update) throw new Error(`${resource}: update unsupported`);
      const data = await callRpc<any>(cfg, m.update, { id, ...v });
      return { data };
    },

    deleteOne: async ({ resource, id }) => {
      const m = resourceDef(resource);
      if (!m.remove) throw new Error(`${resource}: delete unsupported`);
      const data = await callRpc<any>(cfg, m.remove, { id });
      return { data };
    },
  };
}
