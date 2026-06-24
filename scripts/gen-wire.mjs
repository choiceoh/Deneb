// Regenerate src/gen/miniappWire.ts from the Deneb gateway's //deneb:wire Go
// structs — the single source of truth, shared with the native client's Kotlin
// models (the gateway's ts-models-gen mirrors kotlin-models-gen).
//
// Requires Go and the Deneb repo checked out as a sibling (../Deneb); override
// the location with DENEB_DIR. Pass --check to verify the committed file is up to
// date (the CI drift gate) instead of writing it.
import { execFileSync } from "node:child_process";
import { existsSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const denebDir = process.env.DENEB_DIR ? resolve(process.env.DENEB_DIR) : resolve(repoRoot, "../Deneb");
const gatewayGo = resolve(denebDir, "gateway-go");

if (!existsSync(gatewayGo)) {
  console.error(`gateway-go not found at ${gatewayGo}`);
  console.error("Check out Deneb as a sibling of this repo, or set DENEB_DIR=/path/to/Deneb.");
  process.exit(1);
}

const out = resolve(repoRoot, "src/gen/miniappWire.ts");
const args = ["run", "./cmd/ts-models-gen", "-src", "internal/runtime/rpc/handler/handlerminiapp", "-out", out];
if (process.argv.includes("--check")) args.push("-check");

execFileSync("go", args, { cwd: gatewayGo, stdio: "inherit" });
