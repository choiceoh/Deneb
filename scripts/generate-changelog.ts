#!/usr/bin/env bun
/**
 * Generates CHANGELOG.md from individual release note files in releases/.
 *
 * Each release file is a markdown file with YAML frontmatter:
 *   ---
 *   version: "3.5.2"
 *   date: 2026-03-22
 *   title: Short summary
 *   ---
 *   ### Changes
 *   - ...
 *
 * Files starting with _ (e.g. _template.md) are ignored.
 *
 * Usage:
 *   bun scripts/generate-changelog.ts          # write CHANGELOG.md
 *   bun scripts/generate-changelog.ts --check  # verify CHANGELOG.md is up-to-date
 */

import { readFileSync, readdirSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";

const rootDir = resolve(import.meta.dirname, "..");
const releasesDir = resolve(rootDir, "releases");
const changelogPath = resolve(rootDir, "CHANGELOG.md");

interface ReleaseMeta {
  version: string;
  date: string;
  title: string;
  body: string;
  fileName: string;
}

function parseFrontmatter(raw: string): { meta: Record<string, string>; body: string } {
  const match = raw.match(/^---\n([\s\S]*?)\n---\n?([\s\S]*)$/);
  if (!match) {
    return { meta: {}, body: raw };
  }
  const meta: Record<string, string> = {};
  for (const line of match[1].split("\n")) {
    const idx = line.indexOf(":");
    if (idx === -1) {
      continue;
    }
    const key = line.slice(0, idx).trim();
    let value = line.slice(idx + 1).trim();
    // Strip surrounding quotes
    if (
      (value.startsWith('"') && value.endsWith('"')) ||
      (value.startsWith("'") && value.endsWith("'"))
    ) {
      value = value.slice(1, -1);
    }
    meta[key] = value;
  }
  return { meta, body: match[2].trim() };
}

function compareReleases(a: ReleaseMeta, b: ReleaseMeta): number {
  // Sort by date descending (newest first), then filename as tiebreaker
  if (a.date !== b.date) {
    return b.date.localeCompare(a.date);
  }
  return b.fileName.localeCompare(a.fileName);
}

function loadReleases(): ReleaseMeta[] {
  const files = readdirSync(releasesDir).filter((f) => f.endsWith(".md") && !f.startsWith("_"));
  const releases: ReleaseMeta[] = [];

  for (const fileName of files) {
    const raw = readFileSync(resolve(releasesDir, fileName), "utf8");
    const { meta, body } = parseFrontmatter(raw);
    if (!meta.version || !meta.date) {
      console.warn(`⚠ Skipping ${fileName}: missing version or date in frontmatter`);
      continue;
    }
    releases.push({
      version: meta.version,
      date: meta.date,
      title: meta.title ?? "",
      body,
      fileName,
    });
  }

  releases.sort(compareReleases);
  return releases;
}

function generateChangelog(releases: ReleaseMeta[]): string {
  const lines: string[] = ["# Deneb Changelog", ""];

  for (const release of releases) {
    const titleSuffix = release.title ? `\n\n${release.title}` : "";
    lines.push(`## v${release.version} — ${release.date}${titleSuffix}`);
    lines.push("");
    if (release.body) {
      lines.push(release.body);
      lines.push("");
    }
  }

  return lines.join("\n");
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

const releases = loadReleases();
const generated = generateChangelog(releases);

if (process.argv.includes("--check")) {
  let existing = "";
  try {
    existing = readFileSync(changelogPath, "utf8");
  } catch {
    // file doesn't exist
  }
  if (existing.trimEnd() !== generated.trimEnd()) {
    console.error("✗ CHANGELOG.md is out of date. Run: bun scripts/generate-changelog.ts");
    process.exit(1);
  }
  console.log("✓ CHANGELOG.md is up-to-date");
  process.exit(0);
}

writeFileSync(changelogPath, generated);
console.log(`Generated CHANGELOG.md with ${releases.length} releases.`);
