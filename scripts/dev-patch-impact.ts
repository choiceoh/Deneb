#!/usr/bin/env bun
/**
 * dev-patch-impact — Analyze the current diff and suggest what to check.
 *
 * Usage:
 *   bun scripts/dev-patch-impact.ts            # analyze current git diff
 *   bun scripts/dev-patch-impact.ts --staged   # staged changes only
 *
 * Outputs actionable suggestions: tests to run, docs to update, related files,
 * and which gates to run (check/test/build).
 */

import { execSync } from "node:child_process";
import path from "node:path";

const ROOT = path.resolve(import.meta.dirname, "..");

// -------------------------------------------------------------------------
// Categorize changed files
// -------------------------------------------------------------------------

type FileCategory =
  | "src"
  | "cli"
  | "commands"
  | "agents"
  | "tools"
  | "plugin-sdk"
  | "extension"
  | "channel"
  | "config"
  | "docs"
  | "test"
  | "script"
  | "build-config"
  | "other";

function categorize(file: string): FileCategory {
  if (file.endsWith(".test.ts") || file.endsWith(".e2e.test.ts")) {
    return "test";
  }
  if (file.startsWith("docs/")) {
    return "docs";
  }
  if (file.startsWith("scripts/")) {
    return "script";
  }
  if (file.startsWith("extensions/")) {
    return "extension";
  }
  if (file.startsWith("src/cli/")) {
    return "cli";
  }
  if (file.startsWith("src/commands/")) {
    return "commands";
  }
  if (file.startsWith("src/agents/tools/")) {
    return "tools";
  }
  if (file.startsWith("src/agents/")) {
    return "agents";
  }
  if (file.startsWith("src/plugin-sdk/")) {
    return "plugin-sdk";
  }
  if (file.startsWith("src/config/")) {
    return "config";
  }
  if (
    file.startsWith("src/telegram/") ||
    file.startsWith("src/discord/") ||
    file.startsWith("src/slack/") ||
    file.startsWith("src/signal/") ||
    file.startsWith("src/imessage/") ||
    file.startsWith("src/web/") ||
    file.startsWith("src/channels/")
  ) {
    return "channel";
  }
  if (
    file === "package.json" ||
    file === "tsconfig.json" ||
    file === "pnpm-lock.yaml" ||
    file.includes("esbuild") ||
    file.includes("vitest")
  ) {
    return "build-config";
  }
  if (file.startsWith("src/")) {
    return "src";
  }
  return "other";
}

// -------------------------------------------------------------------------
// Suggestions engine
// -------------------------------------------------------------------------

type Suggestion = {
  action: string;
  reason: string;
  command?: string;
};

function generateSuggestions(files: string[], categories: Map<string, FileCategory>): Suggestion[] {
  const suggestions: Suggestion[] = [];
  const cats = new Set(categories.values());

  // Always suggest check if any source changed
  const hasSource =
    cats.has("src") ||
    cats.has("cli") ||
    cats.has("commands") ||
    cats.has("agents") ||
    cats.has("tools") ||
    cats.has("plugin-sdk") ||
    cats.has("extension") ||
    cats.has("channel") ||
    cats.has("config");

  if (hasSource) {
    suggestions.push({
      action: "Run lint/format/typecheck",
      reason: "Source files changed",
      command: "pnpm check",
    });
  }

  // Tests
  if (hasSource) {
    suggestions.push({
      action: "Run affected tests",
      reason: "Source files changed — verify correctness",
      command: `bun scripts/dev-affected.ts ${files.filter((f) => !f.endsWith(".test.ts")).join(" ")}`,
    });
  }

  // Build gate
  if (cats.has("plugin-sdk") || cats.has("build-config")) {
    suggestions.push({
      action: "Run build",
      reason: "Plugin SDK or build config changed — verify build output",
      command: "pnpm build",
    });
  }

  // Dynamic import check
  if (cats.has("agents") || cats.has("plugin-sdk") || cats.has("cli")) {
    suggestions.push({
      action: "Check dynamic imports",
      reason: "Module boundary files changed — watch for INEFFECTIVE_DYNAMIC_IMPORT",
      command: "pnpm build",
    });
  }

  // Extension boundary lint
  if (cats.has("extension")) {
    suggestions.push({
      action: "Check extension import boundaries",
      reason: "Extension files changed",
      command:
        "pnpm lint:extensions:no-src-outside-plugin-sdk && pnpm lint:extensions:no-plugin-sdk-internal && pnpm lint:extensions:no-relative-outside-package",
    });
  }

  // Channel-related: remind to check all channels
  if (cats.has("channel")) {
    const channelFiles = files.filter((f) => categories.get(f) === "channel");
    suggestions.push({
      action: "Verify all channels",
      reason: `Channel code changed (${channelFiles.length} files) — ensure consistent behavior across all built-in + extension channels`,
    });
  }

  // Docs
  if (cats.has("docs")) {
    suggestions.push({
      action: "Check docs links",
      reason: "Doc files changed — verify root-relative links, no .md/.mdx extensions",
    });
  }

  // Config changes
  if (cats.has("config")) {
    suggestions.push({
      action: "Run config-related tests",
      reason: "Config module changed — verify config loading/writing",
    });
  }

  // labeler.yml update
  if (cats.has("extension") || cats.has("channel")) {
    suggestions.push({
      action: "Update .github/labeler.yml",
      reason: "Channel/extension changed — ensure labeler rules match",
    });
  }

  // Package.json / deps
  const pkgChanged = files.some((f) => f === "package.json" || f === "pnpm-lock.yaml");
  if (pkgChanged) {
    suggestions.push({
      action: "Verify patched dependencies",
      reason: "package.json changed — patched deps must use exact versions (no ^/~)",
    });
  }

  return suggestions;
}

// -------------------------------------------------------------------------
// Main
// -------------------------------------------------------------------------

function main() {
  const args = new Set(process.argv.slice(2));
  const stagedOnly = args.has("--staged");

  let diffCmd = "git diff --name-only --diff-filter=ACMR";
  if (stagedOnly) {
    diffCmd = "git diff --cached --name-only --diff-filter=ACMR";
  } else {
    diffCmd =
      "git diff --name-only --diff-filter=ACMR HEAD 2>/dev/null || git diff --name-only --diff-filter=ACMR";
  }

  let changedFiles: string[];
  try {
    const raw = execSync(diffCmd, { cwd: ROOT, encoding: "utf8", timeout: 10_000 });
    changedFiles = raw
      .split("\n")
      .map((l) => l.trim())
      .filter(Boolean);
  } catch {
    changedFiles = [];
  }

  // Also include untracked files
  if (!stagedOnly) {
    try {
      const untracked = execSync("git ls-files --others --exclude-standard", {
        cwd: ROOT,
        encoding: "utf8",
        timeout: 10_000,
      });
      const untrackedFiles = untracked
        .split("\n")
        .map((l) => l.trim())
        .filter(Boolean);
      changedFiles = [...new Set([...changedFiles, ...untrackedFiles])];
    } catch {
      // ignore
    }
  }

  if (changedFiles.length === 0) {
    console.log(
      JSON.stringify(
        { files: [], categories: {}, suggestions: [], summary: "No changes detected." },
        null,
        2,
      ),
    );
    return;
  }

  const categories = new Map<string, FileCategory>();
  for (const f of changedFiles) {
    categories.set(f, categorize(f));
  }

  const suggestions = generateSuggestions(changedFiles, categories);

  // Group files by category
  const grouped: Record<string, string[]> = {};
  for (const [file, cat] of categories) {
    (grouped[cat] ??= []).push(file);
  }

  const output = {
    fileCount: changedFiles.length,
    categories: grouped,
    suggestions: suggestions.map((s) => ({
      action: s.action,
      reason: s.reason,
      ...(s.command ? { command: s.command } : {}),
    })),
    gates: {
      check: suggestions.some((s) => s.command?.includes("pnpm check")),
      test: suggestions.some((s) => s.action.toLowerCase().includes("test")),
      build: suggestions.some((s) => s.command?.includes("pnpm build")),
    },
    summary: `${changedFiles.length} files changed across ${Object.keys(grouped).length} categories → ${suggestions.length} suggestions`,
  };

  console.log(JSON.stringify(output, null, 2));
}

main();
