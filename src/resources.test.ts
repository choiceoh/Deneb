import { describe, expect, it } from "vitest";
import { RESOURCE_DEFS, RESOURCE_MAP, refineResources, resourceDef } from "./resources";

describe("resource registry", () => {
  it("maps every def by name", () => {
    for (const def of RESOURCE_DEFS) {
      expect(RESOURCE_MAP[def.name]).toBe(def);
    }
  });

  it("resolves known resources and throws on unknown", () => {
    expect(resourceDef("todo").list).toBe("miniapp.todo.list");
    expect(() => resourceDef("nope")).toThrow(/unknown resource/);
  });

  it("derives Refine resources with labels in sync with the registry", () => {
    expect(refineResources).toHaveLength(RESOURCE_DEFS.length);
    for (const r of refineResources) {
      expect(r.meta.label).toBeTruthy();
      expect(RESOURCE_MAP[r.name]).toBeDefined();
    }
  });

  it("wires todo CRUD and mail/calendar reads per DESIGN §5", () => {
    expect(resourceDef("todo")).toMatchObject({
      create: expect.any(String),
      update: expect.any(String),
      remove: expect.any(String),
    });
    expect(resourceDef("mail").get).toBe("miniapp.gmail.get");
    expect(resourceDef("calendar").list).toBe("miniapp.calendar.list_upcoming");
    expect(resourceDef("calendar-range").list).toBe("miniapp.calendar.list_range");
  });

  it("registers the read-mostly resources (people/crons/workfeed)", () => {
    for (const name of ["people", "crons", "workfeed"]) {
      expect(resourceDef(name).list).toMatch(/^miniapp\./);
    }
  });
});
