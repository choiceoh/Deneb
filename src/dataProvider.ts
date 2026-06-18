import type { DataProvider } from "@refinedev/core";
import { type GatewayConfig, callRpc } from "./gateway";
import { resourceDef } from "./resources";

// Deneb-backed Refine data provider. Resource↔RPC wiring lives in resources.ts;
// this file is just the generic glue from Refine's CRUD contract to callRpc().
//
// RPC payloads are dynamic, so the provider works in `any` at this boundary and
// Refine re-applies the caller's TData on the way out (its CRUD methods are
// generic over caller-supplied TData).
export function denebDataProvider(cfg: GatewayConfig): DataProvider {
  return {
    getApiUrl: () => cfg.url,

    getList: async ({ resource }) => {
      const rows = await callRpc<any[]>(cfg, resourceDef(resource).list, {});
      const data = Array.isArray(rows) ? rows : [];
      return { data, total: data.length };
    },

    getOne: async ({ resource, id }) => {
      const m = resourceDef(resource);
      // mail/calendar expose a dedicated get; todo has none, so fall back to list+find.
      if (m.get) {
        const data = await callRpc<any>(cfg, m.get, { id });
        return { data: data ?? { id } };
      }
      const rows = await callRpc<any[]>(cfg, m.list, {});
      const found = (rows ?? []).find((r) => String(r.id) === String(id));
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
