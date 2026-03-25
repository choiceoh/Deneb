/**
 * VegaMemoryManager — subprocess-based memory backend for Vega.
 *
 * Vega handles all indexing, embedding, and search internally.
 * This manager just calls the Vega CLI.
 */

import fs from "node:fs/promises";
import path from "node:path";
import { createSubsystemLogger } from "../logging/subsystem.js";
import type { ResolvedVegaConfig } from "./backend-config.js";
import { resolveCliSpawnInvocation, runCliCommand } from "./cli-process.js";
import { isFileMissingError, statRegularFile } from "./fs-utils.js";
import { deriveScopeChannel, deriveScopeChatType, isScopeAllowed } from "./session-scope.js";
import type {
  MemoryEmbeddingProbeResult,
  MemoryProviderStatus,
  MemorySearchManager,
  MemorySearchResult,
  MemorySyncProgressUpdate,
} from "./types.js";

const log = createSubsystemLogger("memory");

const MAX_VEGA_OUTPUT_CHARS = 200_000;

/** Vega capabilities reported by memory-version / memory-status (v1.48+). */
export type VegaCapabilities = {
  semanticSearch: boolean;
  reranking: boolean;
  rerankMode?: string;
  inferenceBackend?: string;
  searchModes: string[];
  schemaVersion?: number;
};

/** Version info from a Vega v1.48+ instance. */
export type VegaVersionInfo = {
  version: string;
  protocolVersion: number;
  capabilities: VegaCapabilities;
};

export class VegaMemoryManager implements MemorySearchManager {
  static async create(params: {
    cfg: { workspaceDir: string };
    agentId: string;
    resolved: ResolvedVegaConfig;
  }): Promise<VegaMemoryManager> {
    const manager = new VegaMemoryManager({
      workspaceDir: params.cfg.workspaceDir,
      agentId: params.agentId,
      resolved: params.resolved,
    });
    await manager.initialize();
    return manager;
  }

  private readonly workspaceDir: string;
  private readonly agentId: string;
  private readonly vega: ResolvedVegaConfig;
  private readonly env: NodeJS.ProcessEnv;
  private updateTimer: ReturnType<typeof setInterval> | null = null;
  private lastUpdateAt: number | null = null;
  private lastEmbedAt: number | null = null;
  private closed = false;

  /** Cached Vega version info (populated on first status refresh). */
  private vegaVersion: VegaVersionInfo | null = null;

  private constructor(params: {
    workspaceDir: string;
    agentId: string;
    resolved: ResolvedVegaConfig;
  }) {
    this.workspaceDir = params.workspaceDir;
    this.agentId = params.agentId;
    this.vega = params.resolved;
    this.env = {
      ...process.env,
      NO_COLOR: "1",
      // Pass through user-configured env vars for Vega subprocess
      ...params.resolved.env,
    };
  }

  private async initialize(): Promise<void> {
    // Probe Vega version in background (non-blocking)
    void this.probeVersion().catch((err) => {
      log.warn(`vega version probe failed: ${String(err)}`);
    });

    if (this.vega.update.onBoot) {
      void this.runUpdate("boot").catch((err) => {
        log.warn(`vega boot update failed: ${String(err)}`);
      });
    }
    if (this.vega.update.intervalMs > 0) {
      this.updateTimer = setInterval(() => {
        void this.runUpdate("interval").catch((err) => {
          log.warn(`vega update failed (${String(err)})`);
        });
      }, this.vega.update.intervalMs);
      // Don't keep the Node.js process alive just for update polling
      this.updateTimer.unref();
    }
  }

  async search(
    query: string,
    opts?: { maxResults?: number; minScore?: number; sessionKey?: string },
  ): Promise<MemorySearchResult[]> {
    if (!this.isScopeAllowed(opts?.sessionKey)) {
      this.logScopeDenied(opts?.sessionKey);
      return [];
    }

    const trimmed = query.trim();
    if (!trimmed) {
      return [];
    }

    const limit = Math.min(
      this.vega.limits.maxResults,
      opts?.maxResults ?? this.vega.limits.maxResults,
    );

    try {
      const args = ["memory-search", trimmed, "--json", "--limit", String(limit)];

      // Pass searchMode if Vega supports it (v1.48+ or always — older Vega ignores unknown flags)
      if (this.vega.searchMode) {
        args.push("--mode", this.vega.searchMode);
      }

      const result = await this.runVega(args, { timeoutMs: this.vega.limits.timeoutMs });

      const parsed = this.parseSearchResults(result.stdout);
      const minScore = opts?.minScore ?? 0;
      const filtered = parsed.filter((r) => r.score >= minScore);
      return this.clampByInjectedChars(filtered, limit);
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      log.warn(`vega search failed: ${message}`);
      throw err;
    }
  }

  async readFile(params: {
    relPath: string;
    from?: number;
    lines?: number;
  }): Promise<{ text: string; path: string }> {
    const relPath = params.relPath?.trim();
    if (!relPath) {
      throw new Error("path required");
    }

    const absPath = this.resolveReadPath(relPath);
    if (!absPath.endsWith(".md")) {
      throw new Error("path must be .md");
    }

    const statResult = await statRegularFile(absPath);
    if (statResult.missing) {
      return { text: "", path: relPath };
    }

    if (params.from !== undefined || params.lines !== undefined) {
      const partial = await this.readPartialText(absPath, params.from, params.lines);
      if (partial.missing) {
        return { text: "", path: relPath };
      }
      return { text: partial.text ?? "", path: relPath };
    }

    try {
      const text = await fs.readFile(absPath, "utf-8");
      return { text, path: relPath };
    } catch (err) {
      if (isFileMissingError(err)) {
        return { text: "", path: relPath };
      }
      throw err;
    }
  }

  private cachedStatus: MemoryProviderStatus | null = null;

  private ensureStatus(): MemoryProviderStatus {
    if (this.cachedStatus) {
      return this.cachedStatus;
    }
    // Return static status while first async refresh is pending
    const base: MemoryProviderStatus = {
      backend: "vega",
      provider: "vega",
      model: "vega",
      files: 0,
      chunks: 0,
      workspaceDir: this.workspaceDir,
      vector: { enabled: true, available: false },
      sources: ["memory"],
      dirty: false,
      custom: {
        vega: {
          lastUpdateAt: this.lastUpdateAt,
        },
      },
    };
    this.cachedStatus = base;
    // Fire-and-forget async refresh
    void this.refreshStatus().catch(() => {});
    return base;
  }

  private async refreshStatus(): Promise<void> {
    try {
      const result = await this.runVega(["memory-status", "--json"], { timeoutMs: 10_000 });
      const parsed = this.parseStatusResponse(result.stdout);

      // Extract version info if present (v1.48+)
      if (parsed.version) {
        this.vegaVersion = {
          version: parsed.version,
          protocolVersion: parsed.protocolVersion ?? 0,
          capabilities: parsed.capabilities ?? {
            semanticSearch: false,
            reranking: false,
            searchModes: ["search"],
          },
        };
      }

      const semanticAvailable =
        this.vegaVersion?.capabilities.semanticSearch ?? parsed.embedded !== undefined;

      this.cachedStatus = {
        backend: "vega",
        provider: "vega",
        model: parsed.model ?? "vega",
        files: parsed.files ?? 0,
        chunks: parsed.chunks ?? 0,
        workspaceDir: this.workspaceDir,
        vector: { enabled: true, available: semanticAvailable },
        sources: ["memory"],
        dirty: false,
        custom: {
          vega: {
            lastUpdateAt: this.lastUpdateAt,
            dbPath: parsed.dbPath,
            embedded: parsed.embedded,
            // v1.48+: extended info
            version: this.vegaVersion?.version,
            protocolVersion: this.vegaVersion?.protocolVersion,
            capabilities: this.vegaVersion?.capabilities,
            models: parsed.models,
            counts: parsed.counts,
          },
        },
      };
    } catch {
      // Keep last known status
    }
  }

  status(): MemoryProviderStatus {
    return this.ensureStatus();
  }

  /** Return cached Vega version info, or null if not yet probed. */
  getVegaVersion(): VegaVersionInfo | null {
    return this.vegaVersion;
  }

  async sync(params?: {
    reason?: string;
    force?: boolean;
    sessionFiles?: string[];
    progress?: (update: MemorySyncProgressUpdate) => void;
  }): Promise<void> {
    if (params?.progress) {
      params.progress({ completed: 0, total: 1, label: "Updating Vega index…" });
    }
    await this.runUpdate(params?.reason ?? "manual", params?.force);
    if (params?.progress) {
      params.progress({ completed: 1, total: 1, label: "Vega index updated" });
    }
  }

  async probeEmbeddingAvailability(): Promise<MemoryEmbeddingProbeResult> {
    // Use cached capabilities if available
    if (this.vegaVersion?.capabilities.semanticSearch !== undefined) {
      return { ok: this.vegaVersion.capabilities.semanticSearch };
    }
    return { ok: true };
  }

  async probeVectorAvailability(): Promise<boolean> {
    if (this.vegaVersion?.capabilities.semanticSearch !== undefined) {
      return this.vegaVersion.capabilities.semanticSearch;
    }
    return true;
  }

  async close(): Promise<void> {
    if (this.closed) {
      return;
    }
    this.closed = true;
    if (this.updateTimer) {
      clearInterval(this.updateTimer);
      this.updateTimer = null;
    }
  }

  // ── Private helpers ──

  /**
   * Probe Vega version via the lightweight memory-version command.
   * Falls back to memory-status if memory-version is not available (pre-v1.48).
   */
  private async probeVersion(): Promise<void> {
    try {
      const result = await this.runVega(["memory-version", "--json"], { timeoutMs: 5_000 });
      const parsed = this.parseVersionResponse(result.stdout);
      if (parsed) {
        this.vegaVersion = parsed;
        log.info(
          `vega version: ${parsed.version} (protocol=${parsed.protocolVersion}, semantic=${String(parsed.capabilities.semanticSearch)})`,
        );
        return;
      }
    } catch {
      // memory-version not available — fall through
    }

    // Fallback: extract version from memory-status (v1.48+)
    try {
      const result = await this.runVega(["memory-status", "--json"], { timeoutMs: 10_000 });
      const parsed = this.parseStatusResponse(result.stdout);
      if (parsed.version) {
        this.vegaVersion = {
          version: parsed.version,
          protocolVersion: parsed.protocolVersion ?? 0,
          capabilities: parsed.capabilities ?? {
            semanticSearch: false,
            reranking: false,
            searchModes: ["search"],
          },
        };
        log.info(`vega version (via status): ${parsed.version}`);
      }
    } catch {
      log.warn("vega version probe failed (both memory-version and memory-status)");
    }
  }

  private async runVega(
    args: string[],
    opts?: { timeoutMs?: number; discardStdout?: boolean },
  ): Promise<{ stdout: string; stderr: string }> {
    return await runCliCommand({
      commandSummary: `vega ${args.join(" ")}`,
      spawnInvocation: resolveCliSpawnInvocation({
        command: this.vega.command,
        args,
      }),
      env: this.env,
      cwd: this.workspaceDir,
      timeoutMs: opts?.timeoutMs,
      maxOutputChars: MAX_VEGA_OUTPUT_CHARS,
      discardStdout: opts?.discardStdout,
    });
  }

  private async runUpdate(reason: string, force?: boolean): Promise<void> {
    if (this.closed) {
      return;
    }
    if (!force && this.lastUpdateAt) {
      const elapsed = Date.now() - this.lastUpdateAt;
      // Debounce: skip if updated within last 15 seconds
      if (elapsed < 15_000) {
        return;
      }
    }

    try {
      await this.runVega(force ? ["memory-update", "--force"] : ["memory-update"], {
        timeoutMs: this.vega.update.commandTimeoutMs,
      });
      this.lastUpdateAt = Date.now();
    } catch (err) {
      log.warn(`vega update failed (${reason}): ${String(err)}`);
      throw err;
    }

    // Embed after update if needed
    if (this.shouldRunEmbed(force)) {
      try {
        await this.runVega(force ? ["memory-embed", "--force"] : ["memory-embed"], {
          timeoutMs: this.vega.update.commandTimeoutMs,
        });
        this.lastEmbedAt = Date.now();
      } catch (err) {
        log.warn(`vega embed failed (${reason}): ${String(err)}`);
      }
    }
  }

  private shouldRunEmbed(force?: boolean): boolean {
    if (force) {
      return true;
    }
    if (this.lastEmbedAt === null) {
      return true;
    }
    const interval = this.vega.update.embedIntervalMs;
    if (interval <= 0) {
      return false;
    }
    return Date.now() - this.lastEmbedAt > interval;
  }

  private parseSearchResults(stdout: string): MemorySearchResult[] {
    const trimmed = stdout.trim();
    if (!trimmed || trimmed === "[]") {
      return [];
    }

    try {
      const parsed = JSON.parse(trimmed);
      if (!Array.isArray(parsed)) {
        return [];
      }

      const results: MemorySearchResult[] = [];
      for (const item of parsed) {
        if (!item || typeof item !== "object") {
          continue;
        }
        const path = typeof item.path === "string" ? item.path.trim() : "";
        if (!path) {
          continue;
        }
        results.push({
          path,
          startLine: typeof item.startLine === "number" ? item.startLine : 1,
          endLine: typeof item.endLine === "number" ? item.endLine : 1,
          score: typeof item.score === "number" ? item.score : 0,
          snippet: typeof item.snippet === "string" ? item.snippet : "",
          source: item.source === "sessions" ? "sessions" : "memory",
        });
      }
      return results;
    } catch {
      log.warn("vega search returned invalid JSON");
      return [];
    }
  }

  private clampByInjectedChars(results: MemorySearchResult[], limit: number): MemorySearchResult[] {
    const budget = this.vega.limits.maxInjectedChars;
    if (!budget || budget <= 0) {
      return results.slice(0, limit);
    }

    let remaining = budget;
    const clamped: MemorySearchResult[] = [];
    for (const entry of results) {
      if (remaining <= 0 || clamped.length >= limit) {
        break;
      }
      const snippet = entry.snippet ?? "";
      if (snippet.length <= remaining) {
        clamped.push(entry);
        remaining -= snippet.length;
      } else {
        clamped.push({ ...entry, snippet: snippet.slice(0, Math.max(0, remaining)) });
        break;
      }
    }
    return clamped;
  }

  private parseVersionResponse(stdout: string): VegaVersionInfo | null {
    const trimmed = stdout.trim();
    if (!trimmed) {
      return null;
    }
    try {
      const parsed = JSON.parse(trimmed);
      if (!parsed || typeof parsed !== "object" || typeof parsed.version !== "string") {
        return null;
      }
      return {
        version: parsed.version,
        protocolVersion: typeof parsed.protocolVersion === "number" ? parsed.protocolVersion : 0,
        capabilities: this.parseCapabilities(parsed.capabilities),
      };
    } catch {
      return null;
    }
  }

  private parseCapabilities(raw: unknown): VegaCapabilities {
    const defaults: VegaCapabilities = {
      semanticSearch: false,
      reranking: false,
      searchModes: ["search"],
    };
    if (!raw || typeof raw !== "object") {
      return defaults;
    }
    const obj = raw as Record<string, unknown>;
    return {
      semanticSearch: typeof obj.semanticSearch === "boolean" ? obj.semanticSearch : false,
      reranking: typeof obj.reranking === "boolean" ? obj.reranking : false,
      rerankMode: typeof obj.rerankMode === "string" ? obj.rerankMode : undefined,
      inferenceBackend: typeof obj.inferenceBackend === "string" ? obj.inferenceBackend : undefined,
      searchModes: Array.isArray(obj.searchModes)
        ? obj.searchModes.filter((s): s is string => typeof s === "string")
        : ["search"],
      schemaVersion: typeof obj.schemaVersion === "number" ? obj.schemaVersion : undefined,
    };
  }

  private parseStatusResponse(stdout: string): {
    files?: number;
    chunks?: number;
    embedded?: number;
    model?: string;
    dbPath?: string;
    // v1.48+ fields
    version?: string;
    protocolVersion?: number;
    capabilities?: VegaCapabilities;
    models?: Record<string, string | null>;
    counts?: Record<string, number>;
  } {
    const trimmed = stdout.trim();
    if (!trimmed) {
      return {};
    }
    try {
      const parsed = JSON.parse(trimmed);
      if (!parsed || typeof parsed !== "object") {
        return {};
      }
      return {
        files: typeof parsed.files === "number" ? parsed.files : undefined,
        chunks: typeof parsed.chunks === "number" ? parsed.chunks : undefined,
        embedded: typeof parsed.embedded === "number" ? parsed.embedded : undefined,
        model: typeof parsed.model === "string" ? parsed.model : undefined,
        dbPath: typeof parsed.dbPath === "string" ? parsed.dbPath : undefined,
        // v1.48+ extended fields
        version: typeof parsed.version === "string" ? parsed.version : undefined,
        protocolVersion:
          typeof parsed.protocolVersion === "number" ? parsed.protocolVersion : undefined,
        capabilities:
          parsed.capabilities && typeof parsed.capabilities === "object"
            ? this.parseCapabilities(parsed.capabilities)
            : undefined,
        models:
          parsed.models && typeof parsed.models === "object"
            ? (parsed.models as Record<string, string | null>)
            : undefined,
        counts:
          parsed.counts && typeof parsed.counts === "object"
            ? (parsed.counts as Record<string, number>)
            : undefined,
      };
    } catch {
      log.warn("vega status returned invalid JSON");
      return {};
    }
  }

  private resolveReadPath(relPath: string): string {
    if (relPath.startsWith("..") || path.isAbsolute(relPath)) {
      throw new Error("path escapes workspace");
    }
    const absPath = path.resolve(this.workspaceDir, relPath);
    // Use realpath-style normalization to handle symlinks in workspaceDir.
    // Fall back to path.resolve comparison if realpath is not available synchronously.
    const normalizedWs = this.workspaceDir.endsWith(path.sep)
      ? this.workspaceDir
      : `${this.workspaceDir}${path.sep}`;
    const normalizedAbs = absPath.endsWith(path.sep) ? absPath : `${absPath}${path.sep}`;
    if (absPath !== this.workspaceDir && !normalizedAbs.startsWith(normalizedWs)) {
      throw new Error("path escapes workspace");
    }
    return absPath;
  }

  private async readPartialText(
    absPath: string,
    from?: number,
    lines?: number,
  ): Promise<{ missing: boolean; text?: string }> {
    const start = Math.max(1, from ?? 1);
    const count = Math.max(1, lines ?? Number.POSITIVE_INFINITY);

    try {
      const text = await fs.readFile(absPath, "utf-8");
      const fileLines = text.split("\n");
      const slice = fileLines.slice(start - 1, start - 1 + count);
      return { missing: false, text: slice.join("\n") };
    } catch (err) {
      if (isFileMissingError(err)) {
        return { missing: true };
      }
      throw err;
    }
  }

  private isScopeAllowed(sessionKey?: string): boolean {
    return isScopeAllowed(this.vega.scope, sessionKey);
  }

  private logScopeDenied(sessionKey?: string): void {
    const channel = deriveScopeChannel(sessionKey) ?? "unknown";
    const chatType = deriveScopeChatType(sessionKey) ?? "unknown";
    const key = sessionKey?.trim() || "<none>";
    log.warn(
      `vega search denied by scope (channel=${channel}, chatType=${chatType}, session=${key})`,
    );
  }
}
