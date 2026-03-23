import { describe, expect, it } from "vitest";
import { findViolations } from "../../scripts/check-pairing-account-scope.mjs";

describe("check-pairing-account-scope", () => {
  it("detects readChannelAllowFromStore without accountId", () => {
    const source = `readChannelAllowFromStore(channel, store);`;
    const violations = findViolations(source, "test.ts");

    expect(violations).toHaveLength(1);
    expect(violations[0].reason).toContain("readChannelAllowFromStore");
  });

  it("detects readChannelAllowFromStore with undefined accountId", () => {
    const source = `readChannelAllowFromStore(channel, store, undefined);`;
    const violations = findViolations(source, "test.ts");

    expect(violations).toHaveLength(1);
  });

  it("detects readChannelAllowFromStore with null accountId", () => {
    const source = `readChannelAllowFromStore(channel, store, null);`;
    const violations = findViolations(source, "test.ts");

    expect(violations).toHaveLength(1);
  });

  it("passes readChannelAllowFromStore with explicit accountId", () => {
    const source = `readChannelAllowFromStore(channel, store, accountId);`;
    const violations = findViolations(source, "test.ts");

    expect(violations).toEqual([]);
  });

  it("detects readLegacyChannelAllowFromStore as legacy", () => {
    const source = `readLegacyChannelAllowFromStore(channel, store);`;
    const violations = findViolations(source, "test.ts");

    expect(violations).toHaveLength(1);
    expect(violations[0].reason).toContain("legacy-only");
  });

  it("detects readLegacyChannelAllowFromStoreSync as legacy", () => {
    const source = `readLegacyChannelAllowFromStoreSync(channel, store);`;
    const violations = findViolations(source, "test.ts");

    expect(violations).toHaveLength(1);
    expect(violations[0].reason).toContain("legacy-only");
  });

  it("detects upsertChannelPairingRequest without accountId", () => {
    const source = `upsertChannelPairingRequest({ channel: "telegram" });`;
    const violations = findViolations(source, "test.ts");

    expect(violations).toHaveLength(1);
    expect(violations[0].reason).toContain("upsertChannelPairingRequest");
  });

  it("passes upsertChannelPairingRequest with accountId", () => {
    const source = `upsertChannelPairingRequest({ channel: "telegram", accountId });`;
    const violations = findViolations(source, "test.ts");

    expect(violations).toEqual([]);
  });

  it("passes unrelated function calls", () => {
    const source = `doSomething(channel, store);`;
    const violations = findViolations(source, "test.ts");

    expect(violations).toEqual([]);
  });
});
