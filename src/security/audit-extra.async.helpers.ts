/**
 * Shared helpers for async security audit collectors.
 */
import fs from "node:fs/promises";
import path from "node:path";
const MANIFEST_KEY = "deneb" as const;
import { safeStat } from "./audit-fs.js";
import type { SkillScanFinding } from "./skill-scanner.js";
import * as skillScanner from "./skill-scanner.js";

export type CodeSafetySummaryCache = Map<string, Promise<unknown>>;
export const MAX_WORKSPACE_SKILL_SCAN_FILES_PER_WORKSPACE = 2_000;
export const MAX_WORKSPACE_SKILL_ESCAPE_DETAIL_ROWS = 12;

let skillsModulePromise: Promise<typeof import("../agents/skills.js")> | undefined;
let configModulePromise: Promise<typeof import("../config/config.js")> | undefined;

export function loadSkillsModule() {
  skillsModulePromise ??= import("../agents/skills.js");
  return skillsModulePromise;
}

export function loadConfigModule() {
  configModulePromise ??= import("../config/config.js");
  return configModulePromise;
}

export function expandTilde(p: string, env: NodeJS.ProcessEnv): string | null {
  if (!p.startsWith("~")) {
    return p;
  }
  const home = typeof env.HOME === "string" && env.HOME.trim() ? env.HOME.trim() : null;
  if (!home) {
    return null;
  }
  if (p === "~") {
    return home;
  }
  if (p.startsWith("~/") || p.startsWith("~\\")) {
    return path.join(home, p.slice(2));
  }
  return null;
}

export async function readPluginManifestExtensions(pluginPath: string): Promise<string[]> {
  const manifestPath = path.join(pluginPath, "package.json");
  const raw = await fs.readFile(manifestPath, "utf-8").catch(() => "");
  if (!raw.trim()) {
    return [];
  }

  const parsed = JSON.parse(raw) as Partial<
    Record<typeof MANIFEST_KEY, { extensions?: unknown }>
  > | null;
  const extensions = parsed?.[MANIFEST_KEY]?.extensions;
  if (!Array.isArray(extensions)) {
    return [];
  }
  return extensions.map((entry) => (typeof entry === "string" ? entry.trim() : "")).filter(Boolean);
}

export function formatCodeSafetyDetails(findings: SkillScanFinding[], rootDir: string): string {
  return findings
    .map((finding) => {
      const relPath = path.relative(rootDir, finding.file);
      const filePath =
        relPath && relPath !== "." && !relPath.startsWith("..")
          ? relPath
          : path.basename(finding.file);
      const normalizedPath = filePath.replaceAll("\\", "/");
      return `  - [${finding.ruleId}] ${finding.message} (${normalizedPath}:${finding.line})`;
    })
    .join("\n");
}

export async function listInstalledPluginDirs(params: {
  stateDir: string;
  onReadError?: (error: unknown) => void;
}): Promise<{ extensionsDir: string; pluginDirs: string[] }> {
  const extensionsDir = path.join(params.stateDir, "extensions");
  const st = await safeStat(extensionsDir);
  if (!st.ok || !st.isDir) {
    return { extensionsDir, pluginDirs: [] };
  }
  const entries = await fs.readdir(extensionsDir, { withFileTypes: true }).catch((err) => {
    params.onReadError?.(err);
    return [];
  });
  const pluginDirs = entries
    .filter((entry) => entry.isDirectory())
    .map((entry) => entry.name)
    .filter(Boolean);
  return { extensionsDir, pluginDirs };
}

export function isPinnedRegistrySpec(spec: string): boolean {
  const value = spec.trim();
  if (!value) {
    return false;
  }
  const at = value.lastIndexOf("@");
  if (at <= 0 || at >= value.length - 1) {
    return false;
  }
  const version = value.slice(at + 1).trim();
  return /^v?\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$/.test(version);
}

export async function readInstalledPackageVersion(dir: string): Promise<string | undefined> {
  try {
    const raw = await fs.readFile(path.join(dir, "package.json"), "utf-8");
    const parsed = JSON.parse(raw) as { version?: unknown };
    return typeof parsed.version === "string" ? parsed.version : undefined;
  } catch {
    return undefined;
  }
}

function buildCodeSafetySummaryCacheKey(params: {
  dirPath: string;
  includeFiles?: string[];
}): string {
  const includeFiles = (params.includeFiles ?? []).map((entry) => entry.trim()).filter(Boolean);
  const includeKey = includeFiles.length > 0 ? includeFiles.toSorted().join("\u0000") : "";
  return `${params.dirPath}\u0000${includeKey}`;
}

export async function getCodeSafetySummary(params: {
  dirPath: string;
  includeFiles?: string[];
  summaryCache?: CodeSafetySummaryCache;
}): Promise<Awaited<ReturnType<typeof skillScanner.scanDirectoryWithSummary>>> {
  const cacheKey = buildCodeSafetySummaryCacheKey({
    dirPath: params.dirPath,
    includeFiles: params.includeFiles,
  });
  const cache = params.summaryCache;
  if (cache) {
    const hit = cache.get(cacheKey);
    if (hit) {
      return (await hit) as Awaited<ReturnType<typeof skillScanner.scanDirectoryWithSummary>>;
    }
    const pending = skillScanner.scanDirectoryWithSummary(params.dirPath, {
      includeFiles: params.includeFiles,
    });
    cache.set(cacheKey, pending);
    return await pending;
  }
  return await skillScanner.scanDirectoryWithSummary(params.dirPath, {
    includeFiles: params.includeFiles,
  });
}

export async function listWorkspaceSkillMarkdownFiles(workspaceDir: string): Promise<string[]> {
  const skillsRoot = path.join(workspaceDir, "skills");
  const rootStat = await safeStat(skillsRoot);
  if (!rootStat.ok || !rootStat.isDir) {
    return [];
  }

  const skillFiles: string[] = [];
  const queue: string[] = [skillsRoot];
  const visitedDirs = new Set<string>();

  while (queue.length > 0 && skillFiles.length < MAX_WORKSPACE_SKILL_SCAN_FILES_PER_WORKSPACE) {
    const dir = queue.shift()!;
    const dirRealPath = await fs.realpath(dir).catch(() => path.resolve(dir));
    if (visitedDirs.has(dirRealPath)) {
      continue;
    }
    visitedDirs.add(dirRealPath);

    const entries = await fs.readdir(dir, { withFileTypes: true }).catch(() => []);
    for (const entry of entries) {
      if (entry.name.startsWith(".") || entry.name === "node_modules") {
        continue;
      }
      const fullPath = path.join(dir, entry.name);
      if (entry.isDirectory()) {
        queue.push(fullPath);
        continue;
      }
      if (entry.isSymbolicLink()) {
        const stat = await fs.stat(fullPath).catch(() => null);
        if (!stat) {
          continue;
        }
        if (stat.isDirectory()) {
          queue.push(fullPath);
          continue;
        }
        if (stat.isFile() && entry.name === "SKILL.md") {
          skillFiles.push(fullPath);
        }
        continue;
      }
      if (entry.isFile() && entry.name === "SKILL.md") {
        skillFiles.push(fullPath);
      }
    }
  }

  return skillFiles;
}
