// Regenerate src/gen/miniappWire.ts from the Deneb gateway's //deneb:wire Go
// structs — the single source of truth, shared with the native client's Kotlin
// models (the gateway's ts-models-gen mirrors kotlin-models-gen).
//
// Requires Go and the Deneb gateway source — found at a sibling checkout (../Deneb),
// the monorepo parent (when this repo lives at Deneb/andromeda), or DENEB_DIR. Pass
// --check to verify the committed file is up to date (the CI drift gate) instead of
// writing it.
import { execFileSync } from "node:child_process";
import { existsSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
// Locate the gateway source across layouts: an explicit DENEB_DIR, the monorepo
// parent (when this repo lives at Deneb/andromeda), or a sibling Deneb checkout.
// This keeps `pnpm gen:wire` working before and after the planned monorepo move
// (docs/MONOREPO-MIGRATION.md) with no change.
const candidates = [
  process.env.DENEB_DIR && resolve(process.env.DENEB_DIR, "gateway-go"),
  resolve(repoRoot, "../gateway-go"), // monorepo: andromeda/ inside Deneb
  resolve(repoRoot, "../Deneb/gateway-go"), // sibling Deneb checkout
].filter(Boolean);
const gatewayGo = candidates.find((p) => existsSync(p));

if (!gatewayGo) {
  console.error("gateway-go not found. Looked in:");
  for (const c of candidates) console.error(`  - ${c}`);
  console.error("Check out Deneb (sibling or monorepo parent), or set DENEB_DIR=/path/to/Deneb.");
  process.exit(1);
}

const out = resolve(repoRoot, "src/gen/miniappWire.ts");
const args = ["run", "./cmd/ts-models-gen", "-src", "internal/runtime/rpc/handler/handlerminiapp", "-out", out];
if (process.argv.includes("--check")) args.push("-check");

execFileSync("go", args, { cwd: gatewayGo, stdio: "inherit" });
