import { spawn } from "node:child_process";
import fs from "node:fs/promises";
import path from "node:path";
import { Type } from "@sinclair/typebox";
import { jsonResult, readStringParam, stringEnum } from "deneb/plugin-sdk/agent-runtime";
import type { AnyAgentTool } from "deneb/plugin-sdk/core";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

type RunResult = {
  exitCode: number;
  stdout: string;
  stderr: string;
  durationMs: number;
};

/** Run a command in the workspace directory and capture output. */
function runCommand(
  cmd: string,
  args: string[],
  opts: { cwd: string; timeoutMs?: number; signal?: AbortSignal },
): Promise<RunResult> {
  return new Promise((resolve) => {
    const start = Date.now();
    const child = spawn(cmd, args, {
      cwd: opts.cwd,
      stdio: ["ignore", "pipe", "pipe"],
      timeout: opts.timeoutMs ?? 120_000,
      signal: opts.signal,
      env: { ...process.env, FORCE_COLOR: "0", NO_COLOR: "1" },
    });

    const stdoutChunks: Buffer[] = [];
    const stderrChunks: Buffer[] = [];
    const MAX_OUTPUT = 64 * 1024; // 64 KB cap per stream
    let stdoutLen = 0;
    let stderrLen = 0;

    child.stdout?.on("data", (chunk: Buffer) => {
      if (stdoutLen < MAX_OUTPUT) {
        stdoutChunks.push(chunk);
        stdoutLen += chunk.length;
      }
    });
    child.stderr?.on("data", (chunk: Buffer) => {
      if (stderrLen < MAX_OUTPUT) {
        stderrChunks.push(chunk);
        stderrLen += chunk.length;
      }
    });

    child.on("close", (code) => {
      resolve({
        exitCode: code ?? 1,
        stdout: Buffer.concat(stdoutChunks).toString("utf8").slice(0, MAX_OUTPUT),
        stderr: Buffer.concat(stderrChunks).toString("utf8").slice(0, MAX_OUTPUT),
        durationMs: Date.now() - start,
      });
    });
    child.on("error", () => {
      resolve({
        exitCode: 1,
        stdout: Buffer.concat(stdoutChunks).toString("utf8"),
        stderr: Buffer.concat(stderrChunks).toString("utf8"),
        durationMs: Date.now() - start,
      });
    });
  });
}

function resolveWorkspaceDir(workspaceDir?: string): string {
  return workspaceDir ?? process.cwd();
}

function truncateOutput(text: string, maxLines: number): string {
  const lines = text.split("\n");
  if (lines.length <= maxLines) return text;
  const half = Math.floor(maxLines / 2);
  return [
    ...lines.slice(0, half),
    `\n... (${lines.length - maxLines} lines truncated) ...\n`,
    ...lines.slice(-half),
  ].join("\n");
}

// ---------------------------------------------------------------------------
// dev_check — lint, format, typecheck
// ---------------------------------------------------------------------------

const DEV_CHECK_ACTIONS = ["all", "lint", "format", "typecheck"] as const;

const DevCheckSchema = Type.Object({
  action: Type.Optional(
    stringEnum(DEV_CHECK_ACTIONS, {
      description: 'Which check to run. Default "all" runs pnpm check (lint+format+types).',
      default: "all",
    }),
  ),
  fix: Type.Optional(
    Type.Boolean({
      description: "If true, auto-fix format issues (format:fix). Only applies to format action.",
    }),
  ),
});

export function createDevCheckTool(opts?: { workspaceDir?: string }): AnyAgentTool {
  return {
    label: "Dev Check",
    name: "dev_check",
    ownerOnly: true,
    description:
      "Run lint, format, and typecheck checks on the Deneb codebase. " +
      'Actions: "all" runs pnpm check (lint+format+types). "lint" runs oxlint only. ' +
      '"format" checks formatting (pass fix=true to auto-fix). "typecheck" runs pnpm tsgo. ' +
      "Use before committing to ensure code quality gates pass.",
    parameters: DevCheckSchema,
    execute: async (_toolCallId, args, signal) => {
      const params = args as Record<string, unknown>;
      const action = (
        typeof params.action === "string" ? params.action : "all"
      ) as (typeof DEV_CHECK_ACTIONS)[number];
      const fix = params.fix === true;
      const cwd = resolveWorkspaceDir(opts?.workspaceDir);

      let cmd: string;
      let cmdArgs: string[];

      switch (action) {
        case "all":
          cmd = "pnpm";
          cmdArgs = ["check"];
          break;
        case "lint":
          cmd = "pnpm";
          cmdArgs = ["lint"];
          break;
        case "format":
          cmd = "pnpm";
          cmdArgs = fix ? ["format:fix"] : ["format"];
          break;
        case "typecheck":
          cmd = "pnpm";
          cmdArgs = ["tsgo"];
          break;
        default:
          throw new Error(`Unknown dev_check action: ${action}`);
      }

      const result = await runCommand(cmd, cmdArgs, { cwd, signal });
      const passed = result.exitCode === 0;
      const output = truncateOutput([result.stdout, result.stderr].filter(Boolean).join("\n"), 200);

      return jsonResult({
        passed,
        action,
        exitCode: result.exitCode,
        durationMs: result.durationMs,
        output,
      });
    },
  };
}

// ---------------------------------------------------------------------------
// dev_test — run tests
// ---------------------------------------------------------------------------

const DevTestSchema = Type.Object({
  filter: Type.Optional(
    Type.String({
      description:
        "File path or vitest filter pattern (e.g. src/commands/onboard.test.ts or a test name pattern).",
    }),
  ),
  testName: Type.Optional(
    Type.String({ description: "Run only tests matching this name pattern (-t flag)." }),
  ),
  coverage: Type.Optional(Type.Boolean({ description: "If true, run with coverage reporting." })),
});

export function createDevTestTool(opts?: { workspaceDir?: string }): AnyAgentTool {
  return {
    label: "Dev Test",
    name: "dev_test",
    ownerOnly: true,
    description:
      "Run Vitest tests on the Deneb codebase. " +
      "Optionally filter by file path or test name. " +
      "Returns structured pass/fail results with failure details. " +
      "Use after making code changes to verify correctness.",
    parameters: DevTestSchema,
    execute: async (_toolCallId, args, signal) => {
      const params = args as Record<string, unknown>;
      const filter = readStringParam(params, "filter");
      const testName = readStringParam(params, "testName");
      const coverage = params.coverage === true;
      const cwd = resolveWorkspaceDir(opts?.workspaceDir);

      const cmdArgs = coverage ? ["test:coverage", "--"] : ["test", "--"];
      if (filter) cmdArgs.push(filter);
      if (testName) cmdArgs.push("-t", testName);

      const result = await runCommand("pnpm", cmdArgs, {
        cwd,
        timeoutMs: 300_000, // 5 min for tests
        signal,
      });
      const passed = result.exitCode === 0;
      const output = truncateOutput([result.stdout, result.stderr].filter(Boolean).join("\n"), 300);

      return jsonResult({
        passed,
        exitCode: result.exitCode,
        durationMs: result.durationMs,
        output,
      });
    },
  };
}

// ---------------------------------------------------------------------------
// dev_build — build verification
// ---------------------------------------------------------------------------

const DevBuildSchema = Type.Object({});

export function createDevBuildTool(opts?: { workspaceDir?: string }): AnyAgentTool {
  return {
    label: "Dev Build",
    name: "dev_build",
    ownerOnly: true,
    description:
      "Run pnpm build to verify the Deneb codebase compiles without errors. " +
      "Use after changes that affect build output, packaging, lazy-loading, or module boundaries. " +
      "Reports structured build errors including INEFFECTIVE_DYNAMIC_IMPORT warnings.",
    parameters: DevBuildSchema,
    execute: async (_toolCallId, _args, signal) => {
      const cwd = resolveWorkspaceDir(opts?.workspaceDir);
      const result = await runCommand("pnpm", ["build"], {
        cwd,
        timeoutMs: 180_000, // 3 min
        signal,
      });
      const passed = result.exitCode === 0;
      const combined = [result.stdout, result.stderr].filter(Boolean).join("\n");
      const hasIneffectiveDynamicImport = combined.includes("INEFFECTIVE_DYNAMIC_IMPORT");
      const output = truncateOutput(combined, 200);

      return jsonResult({
        passed,
        exitCode: result.exitCode,
        durationMs: result.durationMs,
        hasIneffectiveDynamicImport,
        output,
      });
    },
  };
}

// ---------------------------------------------------------------------------
// dev_find_related — find related files (tests, imports, dependents)
// ---------------------------------------------------------------------------

const DEV_FIND_RELATED_ACTIONS = ["tests", "imports", "dependents", "all"] as const;

const DevFindRelatedSchema = Type.Object({
  file: Type.String({ description: "Repo-root-relative file path (e.g. src/commands/setup.ts)." }),
  action: Type.Optional(
    stringEnum(DEV_FIND_RELATED_ACTIONS, {
      description:
        '"tests" finds test files, "imports" lists what this file imports, "dependents" finds files that import this file, "all" runs everything.',
      default: "all",
    }),
  ),
});

async function findTestFiles(cwd: string, filePath: string): Promise<string[]> {
  const parsed = path.parse(filePath);
  const testPatterns = [
    // Colocated test
    path.join(parsed.dir, `${parsed.name}.test${parsed.ext}`),
    path.join(parsed.dir, `${parsed.name}.test.ts`),
    // E2E test
    path.join(parsed.dir, `${parsed.name}.e2e.test${parsed.ext}`),
    path.join(parsed.dir, `${parsed.name}.e2e.test.ts`),
  ];
  const results: string[] = [];
  for (const p of new Set(testPatterns)) {
    try {
      await fs.access(path.resolve(cwd, p));
      results.push(p);
    } catch {
      // not found
    }
  }
  return results;
}

async function findImports(cwd: string, filePath: string): Promise<string[]> {
  try {
    const content = await fs.readFile(path.resolve(cwd, filePath), "utf8");
    const importRegex = /(?:import|from)\s+["']([^"']+)["']/g;
    const imports: string[] = [];
    let match: RegExpExecArray | null;
    while ((match = importRegex.exec(content)) !== null) {
      imports.push(match[1]);
    }
    return imports;
  } catch {
    return [];
  }
}

async function findDependents(cwd: string, filePath: string): Promise<string[]> {
  // Use grep to find files that import the target file
  const basename = path.parse(filePath).name;
  const result = await runCommand(
    "grep",
    ["-rl", "--include=*.ts", "--include=*.tsx", basename, "src/", "extensions/"],
    { cwd, timeoutMs: 15_000 },
  );

  if (result.exitCode !== 0 && result.exitCode !== 1) return [];

  return result.stdout
    .split("\n")
    .map((l) => l.trim())
    .filter((l) => l && l !== filePath && !l.endsWith(".test.ts") && !l.endsWith(".e2e.test.ts"));
}

export function createDevFindRelatedTool(opts?: { workspaceDir?: string }): AnyAgentTool {
  return {
    label: "Dev Find Related",
    name: "dev_find_related",
    ownerOnly: true,
    description:
      "Find files related to a given source file. " +
      '"tests" finds colocated test files. "imports" lists what the file imports. ' +
      '"dependents" finds files that import this file. "all" runs everything. ' +
      "Useful for understanding impact of changes and finding relevant tests.",
    parameters: DevFindRelatedSchema,
    execute: async (_toolCallId, args) => {
      const params = args as Record<string, unknown>;
      const file = readStringParam(params, "file", { required: true });
      const action = (
        typeof params.action === "string" ? params.action : "all"
      ) as (typeof DEV_FIND_RELATED_ACTIONS)[number];
      const cwd = resolveWorkspaceDir(opts?.workspaceDir);

      const result: Record<string, unknown> = { file };

      if (action === "tests" || action === "all") {
        result.tests = await findTestFiles(cwd, file);
      }
      if (action === "imports" || action === "all") {
        result.imports = await findImports(cwd, file);
      }
      if (action === "dependents" || action === "all") {
        result.dependents = await findDependents(cwd, file);
      }

      return jsonResult(result);
    },
  };
}

// ---------------------------------------------------------------------------
// dev_git_summary — comprehensive git state
// ---------------------------------------------------------------------------

const DEV_GIT_SUMMARY_ACTIONS = ["full", "status", "diff", "log", "diff_file"] as const;

const DevGitSummarySchema = Type.Object({
  action: Type.Optional(
    stringEnum(DEV_GIT_SUMMARY_ACTIONS, {
      description:
        '"full" shows status+diff+recent log. "status" shows working tree status. ' +
        '"diff" shows staged+unstaged changes. "log" shows recent commits. ' +
        '"diff_file" shows diff for a specific file.',
      default: "full",
    }),
  ),
  file: Type.Optional(Type.String({ description: "File path for diff_file action." })),
  count: Type.Optional(Type.Number({ description: "Number of log entries (default 10)." })),
});

export function createDevGitSummaryTool(opts?: { workspaceDir?: string }): AnyAgentTool {
  return {
    label: "Dev Git Summary",
    name: "dev_git_summary",
    ownerOnly: true,
    description:
      "Get a comprehensive summary of current git state for the Deneb repository. " +
      '"full" shows status, diff, and recent log. "status" shows working tree. ' +
      '"diff" shows staged+unstaged changes. "log" shows recent commits. ' +
      '"diff_file" diffs a specific file. Useful for reviewing changes before committing.',
    parameters: DevGitSummarySchema,
    execute: async (_toolCallId, args, signal) => {
      const params = args as Record<string, unknown>;
      const action = (
        typeof params.action === "string" ? params.action : "full"
      ) as (typeof DEV_GIT_SUMMARY_ACTIONS)[number];
      const file = readStringParam(params, "file");
      const count = typeof params.count === "number" ? Math.min(Math.max(1, params.count), 50) : 10;
      const cwd = resolveWorkspaceDir(opts?.workspaceDir);

      const sections: Record<string, string> = {};

      if (action === "status" || action === "full") {
        const r = await runCommand("git", ["status", "--short"], { cwd, signal });
        sections.status = r.stdout || "(clean)";
      }

      if (action === "diff" || action === "full") {
        const staged = await runCommand("git", ["diff", "--cached", "--stat"], { cwd, signal });
        const unstaged = await runCommand("git", ["diff", "--stat"], { cwd, signal });
        const stagedFull = await runCommand("git", ["diff", "--cached"], { cwd, signal });
        const unstagedFull = await runCommand("git", ["diff"], { cwd, signal });
        sections.staged_summary = staged.stdout || "(none)";
        sections.unstaged_summary = unstaged.stdout || "(none)";
        sections.staged_diff = truncateOutput(stagedFull.stdout, 100);
        sections.unstaged_diff = truncateOutput(unstagedFull.stdout, 100);
      }

      if (action === "log" || action === "full") {
        const r = await runCommand("git", ["log", "--oneline", `--max-count=${count}`], {
          cwd,
          signal,
        });
        sections.log = r.stdout;
      }

      if (action === "diff_file") {
        if (!file) {
          throw new Error("file parameter required for diff_file action");
        }
        const r = await runCommand("git", ["diff", "HEAD", "--", file], { cwd, signal });
        sections.file_diff = truncateOutput(r.stdout || "(no changes)", 200);
      }

      return jsonResult(sections);
    },
  };
}
