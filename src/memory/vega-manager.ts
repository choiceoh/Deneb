/**
 * VegaMemoryManager — memory backend for Vega.
 *
 * Previously called the Vega Python CLI via subprocess. Now stubbed out
 * pending migration to the Go gateway RPC layer, where Vega runs via
 * Rust FFI (core-rs) through the gateway.
 */

import fs from "node:fs/promises";
import path from "node:path";
import { createSubsystemLogger } from "../logging/subsystem.js";
import type { ResolvedVegaConfig } from "./backend-config.js";
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
  }

  private async initialize(): Promise<void> {
    // TODO: Probe Vega version via gateway RPC instead of subprocess
    log.info("vega memory manager initialized (gateway RPC integration pending)");
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

    // TODO: Route search through gateway RPC instead of subprocess.
    // The gateway will call Vega via Rust FFI (core-rs).
    log.warn("vega search is a no-op — gateway RPC integration pending");
    return [];
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

  status(): MemoryProviderStatus {
    // TODO: Fetch live status from gateway RPC instead of subprocess.
    return {
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
          lastUpdateAt: null,
        },
      },
    };
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
    // TODO: Trigger sync via gateway RPC instead of subprocess.
    if (params?.progress) {
      params.progress({ completed: 0, total: 1, label: "Updating Vega index…" });
    }
    log.warn("vega sync is a no-op — gateway RPC integration pending");
    if (params?.progress) {
      params.progress({ completed: 1, total: 1, label: "Vega index update skipped (stub)" });
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
  }

  // ── Private helpers ──

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
