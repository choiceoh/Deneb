import { describe, expect, it } from "vitest";
import { findViolations } from "../../scripts/check-no-pairing-store-group-auth.mjs";

describe("check-no-pairing-store-group-auth", () => {
  it("detects group variable referencing pairing-store identifier", () => {
    const source = `const groupAllowFrom = storeAllowFrom;`;
    const violations = findViolations(source, "test.ts");

    expect(violations).toHaveLength(1);
    expect(violations[0].reason).toContain("group-scoped variable");
  });

  it("detects group property referencing pairing-store source", () => {
    const source = `
      const config = {
        groupAllowFrom: storedAllowFrom,
      };
    `;
    const violations = findViolations(source, "test.ts");

    expect(violations).toHaveLength(1);
    expect(violations[0].reason).toContain("group-scoped property");
  });

  it("detects normalizeAllowFromWithStore with group + store combo", () => {
    const source = `
      normalizeAllowFromWithStore({
        storeAllowFrom: storeAllowFrom,
        allowFrom: groupAllowFrom,
      });
    `;
    const violations = findViolations(source, "test.ts");

    expect(violations).toHaveLength(1);
    expect(violations[0].reason).toContain("normalizeAllowFromWithStore");
  });

  it("passes allowed resolver calls", () => {
    const source = `const groupAllowFrom = resolveEffectiveAllowFromLists(params);`;
    const violations = findViolations(source, "test.ts");

    expect(violations).toEqual([]);
  });

  it("passes resolveDmGroupAccessWithLists resolver", () => {
    const source = `const groupAllowFrom = resolveDmGroupAccessWithLists(params);`;
    const violations = findViolations(source, "test.ts");

    expect(violations).toEqual([]);
  });

  it("passes unrelated variable assignments", () => {
    const source = `const allowFrom = readConfig();`;
    const violations = findViolations(source, "test.ts");

    expect(violations).toEqual([]);
  });

  it("passes group variable without pairing-store reference", () => {
    const source = `const groupAllowFrom = computeGroupAccess();`;
    const violations = findViolations(source, "test.ts");

    expect(violations).toEqual([]);
  });
});
