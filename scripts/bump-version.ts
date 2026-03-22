#!/usr/bin/env bun
// Centralized version bump script.
//
// Usage:
//   bun scripts/bump-version.ts <new-version>
//   bun scripts/bump-version.ts --check
//
// Updates every version location in one shot:
//   - package.json (source of truth)
//   - extensions/[name]/package.json (via sync-plugin-versions)
//   - README.md badge
//
// --check: verify all locations are in sync without writing.

import { readFileSync, readdirSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { syncPluginVersions } from "./sync-plugin-versions.ts";

const rootDir = resolve(import.meta.dirname, "..");

type PackageJson = { version?: string; [key: string]: unknown };

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function readJson<T>(path: string): T {
  return JSON.parse(readFileSync(path, "utf8")) as T;
}

function writeJson(path: string, data: unknown): void {
  writeFileSync(path, `${JSON.stringify(data, null, 2)}\n`);
}

function updateReadmeBadge(version: string): boolean {
  const readmePath = resolve(rootDir, "README.md");
  const content = readFileSync(readmePath, "utf8");
  const badgeRe = /img\.shields\.io\/badge\/version-[^-]+-blue/;
  const replacement = `img.shields.io/badge/version-${version}-blue`;
  if (!badgeRe.test(content)) {
    console.warn("⚠ README.md version badge not found; skipping.");
    return false;
  }
  const updated = content.replace(badgeRe, replacement);
  if (updated === content) {
    return false;
  }
  writeFileSync(readmePath, updated);
  return true;
}

function extractBadgeVersion(): string | null {
  const content = readFileSync(resolve(rootDir, "README.md"), "utf8");
  const m = content.match(/img\.shields\.io\/badge\/version-([^-]+)-blue/);
  return m ? m[1] : null;
}

// ---------------------------------------------------------------------------
// Check mode: verify sync
// ---------------------------------------------------------------------------

function checkSync(): boolean {
  const rootPkg = readJson<PackageJson>(resolve(rootDir, "package.json"));
  const version = rootPkg.version;
  if (!version) {
    console.error("✗ package.json has no version field.");
    return false;
  }

  let ok = true;

  // README badge
  const badgeVersion = extractBadgeVersion();
  if (badgeVersion !== version) {
    console.error(`✗ README.md badge version "${badgeVersion}" ≠ "${version}"`);
    ok = false;
  }

  // Extensions
  const extensionsDir = resolve(rootDir, "extensions");
  for (const entry of readdirSync(extensionsDir, { withFileTypes: true })) {
    if (!entry.isDirectory()) {
      continue;
    }
    const pkgPath = resolve(extensionsDir, entry.name, "package.json");
    try {
      const pkg = readJson<PackageJson>(pkgPath);
      if (pkg.version && pkg.version !== version) {
        console.error(`✗ extensions/${entry.name} version "${pkg.version}" ≠ "${version}"`);
        ok = false;
      }
    } catch {
      // No package.json — skip (e.g. shared/)
    }
  }

  if (ok) {
    console.log(`✓ All version locations in sync: ${version}`);
  }
  return ok;
}

// ---------------------------------------------------------------------------
// Bump mode
// ---------------------------------------------------------------------------

function bump(newVersion: string): void {
  // 1. Root package.json
  const rootPkgPath = resolve(rootDir, "package.json");
  const rootPkg = readJson<PackageJson>(rootPkgPath);
  const oldVersion = rootPkg.version;
  rootPkg.version = newVersion;
  writeJson(rootPkgPath, rootPkg);
  console.log(`package.json: ${oldVersion} → ${newVersion}`);

  // 2. Extensions (via existing sync-plugin-versions)
  const syncResult = syncPluginVersions(rootDir);
  if (syncResult.updated.length > 0) {
    console.log(`extensions synced: ${syncResult.updated.join(", ")}`);
  }
  if (syncResult.skipped.length > 0) {
    console.log(`extensions already up-to-date: ${syncResult.skipped.join(", ")}`);
  }

  // 3. README badge
  if (updateReadmeBadge(newVersion)) {
    console.log(`README.md badge: ${oldVersion} → ${newVersion}`);
  } else {
    console.log("README.md badge: already up-to-date");
  }

  console.log(`\nDone. Version bumped to ${newVersion}`);
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

const args = process.argv.slice(2);

if (args.includes("--check")) {
  const ok = checkSync();
  process.exit(ok ? 0 : 1);
}

if (args.length === 0 || args[0].startsWith("-")) {
  console.error("Usage: bun scripts/bump-version.ts <new-version>");
  console.error("       bun scripts/bump-version.ts --check");
  process.exit(2);
}

bump(args[0]);
