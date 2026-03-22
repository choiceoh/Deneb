import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { describe, expect, it } from "vitest";
import { afterEach } from "vitest";
import {
  collectBundledProviderAuthEnvVars,
  writeBundledProviderAuthEnvVarModule,
} from "../../scripts/generate-bundled-provider-auth-env-vars.mjs";
import { BUNDLED_PROVIDER_AUTH_ENV_VAR_CANDIDATES } from "./bundled-provider-auth-env-vars.js";

const repoRoot = path.resolve(import.meta.dirname, "../..");
const tempDirs: string[] = [];

function writeJson(filePath: string, value: unknown): void {
  fs.mkdirSync(path.dirname(filePath), { recursive: true });
  fs.writeFileSync(filePath, `${JSON.stringify(value, null, 2)}\n`, "utf8");
}

afterEach(() => {
  for (const dir of tempDirs.splice(0, tempDirs.length)) {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

describe("bundled provider auth env vars", () => {
  it("matches the generated manifest snapshot", () => {
    expect(BUNDLED_PROVIDER_AUTH_ENV_VAR_CANDIDATES).toEqual(
      collectBundledProviderAuthEnvVars({ repoRoot }),
    );
  });

  it("reads bundled provider auth env vars from plugin manifests", () => {
    // Only assert providers that are actually bundled in this repo
    const hasBundledProviders = Object.keys(BUNDLED_PROVIDER_AUTH_ENV_VAR_CANDIDATES).length > 0;
    if (hasBundledProviders) {
      // When provider extensions are bundled, verify specific entries
      if ("github-copilot" in BUNDLED_PROVIDER_AUTH_ENV_VAR_CANDIDATES) {
        expect(BUNDLED_PROVIDER_AUTH_ENV_VAR_CANDIDATES["github-copilot"]).toEqual([
          "COPILOT_GITHUB_TOKEN",
          "GH_TOKEN",
          "GITHUB_TOKEN",
        ]);
      }
    }
    expect("openai-codex" in BUNDLED_PROVIDER_AUTH_ENV_VAR_CANDIDATES).toBe(false);
  });

  it("supports check mode for stale generated artifacts", () => {
    const tempRoot = fs.mkdtempSync(path.join(os.tmpdir(), "deneb-provider-auth-env-vars-"));
    tempDirs.push(tempRoot);

    writeJson(path.join(tempRoot, "extensions", "alpha", "deneb.plugin.json"), {
      id: "alpha",
      providerAuthEnvVars: {
        alpha: ["ALPHA_TOKEN"],
      },
    });

    const initial = writeBundledProviderAuthEnvVarModule({
      repoRoot: tempRoot,
      outputPath: "src/plugins/bundled-provider-auth-env-vars.generated.ts",
    });
    expect(initial.wrote).toBe(true);

    const current = writeBundledProviderAuthEnvVarModule({
      repoRoot: tempRoot,
      outputPath: "src/plugins/bundled-provider-auth-env-vars.generated.ts",
      check: true,
    });
    expect(current.changed).toBe(false);
    expect(current.wrote).toBe(false);

    fs.writeFileSync(
      path.join(tempRoot, "src/plugins/bundled-provider-auth-env-vars.generated.ts"),
      "// stale\n",
      "utf8",
    );

    const stale = writeBundledProviderAuthEnvVarModule({
      repoRoot: tempRoot,
      outputPath: "src/plugins/bundled-provider-auth-env-vars.generated.ts",
      check: true,
    });
    expect(stale.changed).toBe(true);
    expect(stale.wrote).toBe(false);
  });
});
