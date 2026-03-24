import { describe, expect, it } from "vitest";
import { CronJobStateSchema } from "../gateway/protocol/schema.js";

type SchemaLike = {
  anyOf?: Array<SchemaLike>;
  properties?: Record<string, unknown>;
  const?: unknown;
};

function extractConstUnionValues(schema: SchemaLike): string[] {
  return Array.from(
    new Set(
      (schema.anyOf ?? [])
        .map((entry) => entry?.const)
        .filter((value): value is string => typeof value === "string"),
    ),
  );
}

describe("cron protocol conformance", () => {
  it("cron job state schema keeps the full failover reason set", () => {
    const properties = (CronJobStateSchema as SchemaLike).properties ?? {};
    const lastErrorReason = properties.lastErrorReason as SchemaLike | undefined;
    expect(lastErrorReason).toBeDefined();
    expect(extractConstUnionValues(lastErrorReason ?? {})).toEqual([
      "auth",
      "format",
      "rate_limit",
      "billing",
      "timeout",
      "model_not_found",
      "unknown",
    ]);
  });
});
