import { describe, expect, it } from "vitest";
import { MAX_SECRET_FILE_BYTES } from "./secret-file.js";

describe("readSecretFromFile", () => {
  it("keeps the shared secret-file limit", () => {
    expect(MAX_SECRET_FILE_BYTES).toBe(16 * 1024);
  });
});
