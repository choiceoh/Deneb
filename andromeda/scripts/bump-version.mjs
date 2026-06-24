// Bump the app version in all three places Tauri reads it from, so bundle
// filenames, the release tag, and the updater's reported version stay in lockstep.
// A drift between these is the classic "the .exe says 0.0.1 but the tag is v0.0.2"
// bug. Usage:  pnpm bump 0.0.2
//
// Pure rewrite helpers are exported for unit tests; the disk I/O only runs when
// this file is executed directly (see the bottom guard).
import { readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const SEMVER = /^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/;

// package.json / tauri.conf.json — set the top-level "version" key, preserving
// Prettier's 2-space + trailing-newline JSON shape.
export function bumpJsonVersion(content, version) {
  const obj = JSON.parse(content);
  obj.version = version;
  return JSON.stringify(obj, null, 2) + "\n";
}

// Cargo.toml — replace only the [package] version, which is the first line-start
// `version = "..."`. Inline dep specs (`tauri = { version = "2" }`) aren't at the
// start of a line, so they're left untouched.
export function bumpCargoVersion(content, version) {
  let replaced = false;
  const out = content.replace(/^version = "[^"]*"$/m, () => {
    replaced = true;
    return `version = "${version}"`;
  });
  if (!replaced) throw new Error("bump: could not find [package] version in Cargo.toml");
  return out;
}

const TARGETS = [
  { file: "package.json", fn: bumpJsonVersion },
  { file: "src-tauri/tauri.conf.json", fn: bumpJsonVersion },
  { file: "src-tauri/Cargo.toml", fn: bumpCargoVersion },
];

function main() {
  const version = process.argv[2];
  if (!version || !SEMVER.test(version)) {
    console.error(`usage: pnpm bump <version>   e.g. pnpm bump 0.0.2   (got: ${version ?? "<none>"})`);
    process.exit(1);
  }

  const root = join(dirname(fileURLToPath(import.meta.url)), "..");
  for (const { file, fn } of TARGETS) {
    const path = join(root, file);
    writeFileSync(path, fn(readFileSync(path, "utf8"), version));
    console.info(`  ${file} → ${version}`);
  }
  console.info(`\nBumped to ${version}. Next: commit, then \`git tag v${version} && git push --tags\` to release.`);
}

// Run only when invoked as a script, not when imported by tests.
if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) {
  main();
}
