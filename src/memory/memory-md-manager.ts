import crypto from "node:crypto";
import fs from "node:fs/promises";
import path from "node:path";
import { isFileMissingError } from "./fs-utils.js";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export type MemoryImportance = "low" | "normal" | "high" | "critical";

export type MemoryEntry = {
  /** Short stable ID (first 8 chars of content hash) */
  id: string;
  /** ISO 8601 timestamp */
  timestamp: string;
  /** The memory content */
  content: string;
  /** Categorization tags */
  tags: string[];
  /** Importance level for recall prioritization */
  importance: MemoryImportance;
};

export type SaveResult = {
  ok: true;
  action: "created" | "updated" | "duplicate";
  id: string;
  file: string;
  /** The entry that was written (or the existing duplicate) */
  entry: MemoryEntry;
};

export type ForgetResult =
  | { ok: true; removed: number; ids: string[] }
  | { ok: false; reason: string };

export type RecallResult = {
  entries: MemoryEntry[];
  total: number;
  file: string;
};

export type MemorySection = {
  level: number;
  title: string;
  content: string;
  startLine: number;
  endLine: number;
};

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const HEADING_RE = /^(#{1,6})\s+(.+)$/;

// Entry format: - [id:abcd1234] **2026-03-21T10:00:00Z** {high} content #tag1 #tag2
const ENTRY_RE =
  /^- \[id:([a-f0-9]+)\] \*\*(\d{4}-\d{2}-\d{2}T[\d:.Z+-]+)\*\*(?:\s+\{(\w+)\})?\s+(.+)$/;

const IMPORTANCE_ORDER: Record<MemoryImportance, number> = {
  low: 0,
  normal: 1,
  high: 2,
  critical: 3,
};

const READ_ONLY_FILES = new Set(["memory.md", "soul.md", "tools.md", "agents.md"]);

// ---------------------------------------------------------------------------
// MemoryMdManager — AI-agent-first memory file manager
// ---------------------------------------------------------------------------

export class MemoryMdManager {
  private readonly workspaceDir: string;
  private readonly timezone: string;

  constructor(workspaceDir: string, timezone = "UTC") {
    this.workspaceDir = workspaceDir;
    this.timezone = timezone;
  }

  // ---- Save (create or upsert) -------------------------------------------

  /**
   * Save a memory. Deduplicates by content similarity within the target file.
   * If a similar entry exists, it is updated (upsert). Returns structured result
   * so the AI agent knows exactly what happened.
   */
  async save(
    content: string,
    options: {
      tags?: string[];
      importance?: MemoryImportance;
      /** Explicit target file (relative to workspace); defaults to today's daily file */
      file?: string;
      /** Timestamp override */
      timestamp?: string;
    } = {},
  ): Promise<SaveResult> {
    const trimmed = content.trim();
    if (!trimmed) {
      return {
        ok: true,
        action: "duplicate",
        id: "",
        file: "",
        entry: { id: "", timestamp: "", content: "", tags: [], importance: "normal" },
      };
    }

    const timestamp = options.timestamp ?? new Date().toISOString();
    const tags = normalizeTags(options.tags);
    const importance = options.importance ?? "normal";
    const id = contentId(trimmed);

    const relPath = options.file ?? this.dailyFilePath();
    this.assertWritable(relPath);
    const absPath = path.join(this.workspaceDir, relPath);

    const existing = await readFile(absPath);
    const entries = parseAllEntries(existing);

    // Exact duplicate check
    const exactDup = entries.find((e) => e.id === id);
    if (exactDup) {
      return { ok: true, action: "duplicate", id, file: relPath, entry: exactDup };
    }

    // Fuzzy duplicate: if >80% token overlap, update the existing entry
    const similar = findSimilarEntry(entries, trimmed);
    if (similar) {
      const updated: MemoryEntry = {
        ...similar,
        id,
        timestamp,
        content: trimmed,
        tags: mergeTags(similar.tags, tags),
        importance: maxImportance(similar.importance, importance),
      };
      const newContent = replaceEntryInContent(existing, similar.id, formatEntry(updated));
      await writeFile(absPath, newContent);
      return { ok: true, action: "updated", id, file: relPath, entry: updated };
    }

    // New entry — append
    const entry: MemoryEntry = { id, timestamp, content: trimmed, tags, importance };
    const appended = appendEntry(existing, formatEntry(entry));
    await writeFile(absPath, appended);
    return { ok: true, action: "created", id, file: relPath, entry };
  }

  // ---- Recall (filtered list) --------------------------------------------

  /**
   * Recall memories from a specific file or today's daily file.
   * Supports filtering by tags, importance, and text substring.
   */
  async recall(
    filter: {
      file?: string;
      tags?: string[];
      minImportance?: MemoryImportance;
      contains?: string;
      limit?: number;
    } = {},
  ): Promise<RecallResult> {
    const relPath = filter.file ?? this.dailyFilePath();
    const absPath = path.join(this.workspaceDir, relPath);
    const content = await readFile(absPath);
    let entries = parseAllEntries(content);

    if (filter.tags?.length) {
      const wanted = new Set(filter.tags.map((t) => t.toLowerCase()));
      entries = entries.filter((e) => e.tags.some((t) => wanted.has(t.toLowerCase())));
    }

    if (filter.minImportance) {
      const threshold = IMPORTANCE_ORDER[filter.minImportance];
      entries = entries.filter((e) => IMPORTANCE_ORDER[e.importance] >= threshold);
    }

    if (filter.contains) {
      const needle = filter.contains.toLowerCase();
      entries = entries.filter((e) => e.content.toLowerCase().includes(needle));
    }

    const total = entries.length;
    if (filter.limit && filter.limit > 0) {
      entries = entries.slice(0, filter.limit);
    }

    return { entries, total, file: relPath };
  }

  // ---- Recall All (across multiple daily files) --------------------------

  /**
   * Recall memories across all daily files in memory/.
   * Scans all .md files under memory/ and merges results, sorted by timestamp descending.
   */
  async recallAll(
    filter: {
      tags?: string[];
      minImportance?: MemoryImportance;
      contains?: string;
      limit?: number;
    } = {},
  ): Promise<{ entries: Array<MemoryEntry & { file: string }>; total: number }> {
    const memoryDir = path.join(this.workspaceDir, "memory");
    let files: string[];
    try {
      files = (await fs.readdir(memoryDir))
        .filter((f) => f.endsWith(".md"))
        .toSorted()
        .toReversed();
    } catch {
      return { entries: [], total: 0 };
    }

    const allEntries: Array<MemoryEntry & { file: string }> = [];
    for (const file of files) {
      const relPath = `memory/${file}`;
      const result = await this.recall({ ...filter, file: relPath, limit: undefined });
      for (const entry of result.entries) {
        allEntries.push({ ...entry, file: relPath });
      }
    }

    // Sort by timestamp descending (newest first)
    const sorted = allEntries.toSorted((a, b) => b.timestamp.localeCompare(a.timestamp));

    const total = sorted.length;
    const limited = filter.limit && filter.limit > 0 ? sorted.slice(0, filter.limit) : sorted;
    return { entries: limited, total };
  }

  // ---- Forget (remove entries) -------------------------------------------

  /**
   * Remove memories by ID, tag, or content match.
   * At least one filter must be provided to prevent accidental mass deletion.
   */
  async forget(filter: {
    ids?: string[];
    tags?: string[];
    contains?: string;
    file?: string;
  }): Promise<ForgetResult> {
    if (!filter.ids?.length && !filter.tags?.length && !filter.contains) {
      return { ok: false, reason: "At least one filter (ids, tags, or contains) is required." };
    }

    const relPath = filter.file ?? this.dailyFilePath();
    this.assertWritable(relPath);
    const absPath = path.join(this.workspaceDir, relPath);
    const content = await readFile(absPath);
    const entries = parseAllEntries(content);

    const idsToRemove = new Set<string>();
    for (const entry of entries) {
      if (filter.ids?.includes(entry.id)) {
        idsToRemove.add(entry.id);
        continue;
      }
      if (filter.tags?.length) {
        const entryTags = new Set(entry.tags.map((t) => t.toLowerCase()));
        if (filter.tags.some((t) => entryTags.has(t.toLowerCase()))) {
          idsToRemove.add(entry.id);
          continue;
        }
      }
      if (filter.contains && entry.content.toLowerCase().includes(filter.contains.toLowerCase())) {
        idsToRemove.add(entry.id);
      }
    }

    if (idsToRemove.size === 0) {
      return { ok: true, removed: 0, ids: [] };
    }

    const lines = content.split("\n");
    const filtered = lines.filter((line) => {
      const parsed = parseEntryLine(line);
      return !parsed || !idsToRemove.has(parsed.id);
    });

    const result = filtered
      .join("\n")
      .replace(/\n{3,}/g, "\n\n")
      .trimEnd();
    await writeFile(absPath, result ? `${result}\n` : "");
    return { ok: true, removed: idsToRemove.size, ids: [...idsToRemove] };
  }

  // ---- Consolidate (merge old daily files) -------------------------------

  /**
   * Consolidate multiple daily files into a single summary file.
   * Keeps only entries at or above the given importance threshold.
   * Removes consolidated source files after merging.
   */
  async consolidate(options: {
    /** Only consolidate files older than this date (YYYY-MM-DD). Defaults to 7 days ago. */
    before?: string;
    /** Minimum importance to keep. Defaults to "normal". */
    minImportance?: MemoryImportance;
    /** Target file. Defaults to memory/archive.md */
    targetFile?: string;
  }): Promise<{ merged: number; kept: number; removedFiles: string[] }> {
    const before = options.before ?? daysAgo(7);
    const minImportance = options.minImportance ?? "normal";
    const threshold = IMPORTANCE_ORDER[minImportance];
    const targetRel = options.targetFile ?? "memory/archive.md";
    this.assertWritable(targetRel);

    const memoryDir = path.join(this.workspaceDir, "memory");
    let files: string[];
    try {
      files = (await fs.readdir(memoryDir)).filter((f) => f.endsWith(".md")).toSorted();
    } catch {
      return { merged: 0, kept: 0, removedFiles: [] };
    }

    // Filter to daily files before the cutoff (YYYY-MM-DD.md pattern)
    const dailyFileRe = /^(\d{4}-\d{2}-\d{2})\.md$/;
    const toConsolidate = files.filter((f) => {
      const m = f.match(dailyFileRe);
      return m && m[1] < before;
    });

    if (toConsolidate.length === 0) {
      return { merged: 0, kept: 0, removedFiles: [] };
    }

    const keptEntries: MemoryEntry[] = [];
    const removedFiles: string[] = [];

    for (const file of toConsolidate) {
      const absPath = path.join(memoryDir, file);
      const content = await readFile(absPath);
      const entries = parseAllEntries(content);

      for (const entry of entries) {
        if (IMPORTANCE_ORDER[entry.importance] >= threshold) {
          keptEntries.push(entry);
        }
      }

      await fs.unlink(absPath);
      removedFiles.push(`memory/${file}`);
    }

    // Append kept entries to the target archive file
    if (keptEntries.length > 0) {
      const targetAbs = path.join(this.workspaceDir, targetRel);
      let existing = await readFile(targetAbs);
      for (const entry of keptEntries) {
        existing = appendEntry(existing, formatEntry(entry));
      }
      await writeFile(targetAbs, existing);
    }

    return { merged: toConsolidate.length, kept: keptEntries.length, removedFiles };
  }

  // ---- Section helpers (for MEMORY.md reference reads) -------------------

  /** Parse all sections from a memory file. */
  async sections(file?: string): Promise<MemorySection[]> {
    const relPath = file ?? "MEMORY.md";
    const absPath = path.join(this.workspaceDir, relPath);
    const content = await readFile(absPath);
    return parseSections(content);
  }

  /** List section titles from a file. */
  async listSections(file?: string): Promise<Array<{ level: number; title: string }>> {
    const sections = await this.sections(file);
    return sections.map((s) => ({ level: s.level, title: s.title }));
  }

  // ---- Helpers -----------------------------------------------------------

  /** Resolve the daily file path for today in the configured timezone. */
  dailyFilePath(nowMs?: number): string {
    const dateStr = formatDateInTimezone(nowMs ?? Date.now(), this.timezone);
    return `memory/${dateStr}.md`;
  }

  private assertWritable(relPath: string): void {
    const basename = path.basename(relPath).toLowerCase();
    if (READ_ONLY_FILES.has(basename)) {
      throw new Error(
        `${relPath} is read-only. Write to daily files (memory/YYYY-MM-DD.md) instead.`,
      );
    }
  }
}

// ---------------------------------------------------------------------------
// Entry formatting / parsing
// ---------------------------------------------------------------------------

export function formatEntry(entry: MemoryEntry): string {
  const importancePart = entry.importance !== "normal" ? ` {${entry.importance}}` : "";
  const tagsPart = entry.tags.length > 0 ? ` ${entry.tags.map((t) => `#${t}`).join(" ")}` : "";
  return `- [id:${entry.id}] **${entry.timestamp}**${importancePart} ${entry.content}${tagsPart}`;
}

export function parseEntryLine(line: string): MemoryEntry | null {
  const m = line.match(ENTRY_RE);
  if (!m) {
    return null;
  }

  const id = m[1];
  const timestamp = m[2];
  const importance = (m[3] as MemoryImportance | undefined) ?? "normal";
  const rest = m[4];

  // Extract trailing #hashtag tokens (e.g. "#ci #deploy")
  // Tags are always at the end; walk backwards from the end of the string.
  const tags: string[] = [];
  const trailingTagRe = /(?:^|\s)(#[a-z0-9_-]+)$/i;
  let remaining = rest;
  for (;;) {
    const tagMatch = remaining.match(trailingTagRe);
    if (!tagMatch) {
      break;
    }
    tags.unshift(tagMatch[1].slice(1)); // strip leading #
    remaining = remaining.slice(0, tagMatch.index).trimEnd();
  }

  const content = remaining.trim();
  return { id, timestamp, content, tags, importance };
}

export function parseSections(content: string): MemorySection[] {
  const lines = content.split("\n");
  const sections: MemorySection[] = [];
  let current: MemorySection | null = null;

  for (let i = 0; i < lines.length; i++) {
    const match = lines[i].match(HEADING_RE);
    if (match) {
      if (current) {
        current.endLine = i - 1;
        current.content = trimmedSlice(lines, current.startLine, i - 1);
        sections.push(current);
      }
      current = {
        level: match[1].length,
        title: match[2].trim(),
        content: "",
        startLine: i,
        endLine: i,
      };
    }
  }

  if (current) {
    current.endLine = lines.length - 1;
    current.content = trimmedSlice(lines, current.startLine, lines.length - 1);
    sections.push(current);
  }

  return sections;
}

// ---------------------------------------------------------------------------
// Pure helpers
// ---------------------------------------------------------------------------

export function contentId(text: string): string {
  return crypto.createHash("sha256").update(text.trim().toLowerCase()).digest("hex").slice(0, 8);
}

function normalizeTags(tags?: string[]): string[] {
  if (!tags?.length) {
    return [];
  }
  return [...new Set(tags.map((t) => t.trim().toLowerCase()).filter(Boolean))];
}

function mergeTags(a: string[], b: string[]): string[] {
  return [...new Set([...a, ...b])];
}

function maxImportance(a: MemoryImportance, b: MemoryImportance): MemoryImportance {
  return IMPORTANCE_ORDER[a] >= IMPORTANCE_ORDER[b] ? a : b;
}

/**
 * Simple token-overlap similarity check.
 * Returns the entry with >80% bigram overlap, or null.
 */
function findSimilarEntry(entries: MemoryEntry[], newContent: string): MemoryEntry | null {
  const newBigrams = toBigrams(newContent);
  if (newBigrams.size === 0) {
    return null;
  }

  let bestMatch: MemoryEntry | null = null;
  let bestScore = 0;

  for (const entry of entries) {
    const existingBigrams = toBigrams(entry.content);
    if (existingBigrams.size === 0) {
      continue;
    }
    let overlap = 0;
    for (const bg of newBigrams) {
      if (existingBigrams.has(bg)) {
        overlap++;
      }
    }
    const score = (2 * overlap) / (newBigrams.size + existingBigrams.size);
    if (score > 0.8 && score > bestScore) {
      bestScore = score;
      bestMatch = entry;
    }
  }

  return bestMatch;
}

function toBigrams(text: string): Set<string> {
  const tokens = text.toLowerCase().split(/\s+/).filter(Boolean);
  const bigrams = new Set<string>();
  for (let i = 0; i < tokens.length - 1; i++) {
    bigrams.add(`${tokens[i]} ${tokens[i + 1]}`);
  }
  // Also add single tokens for short texts
  if (tokens.length <= 3) {
    for (const t of tokens) {
      bigrams.add(t);
    }
  }
  return bigrams;
}

function parseAllEntries(content: string): MemoryEntry[] {
  if (!content.trim()) {
    return [];
  }
  const entries: MemoryEntry[] = [];
  for (const line of content.split("\n")) {
    const entry = parseEntryLine(line);
    if (entry) {
      entries.push(entry);
    }
  }
  return entries;
}

function replaceEntryInContent(content: string, oldId: string, newLine: string): string {
  const lines = content.split("\n");
  const result = lines.map((line) => {
    const entry = parseEntryLine(line);
    if (entry?.id === oldId) {
      return newLine;
    }
    return line;
  });
  return result.join("\n");
}

function appendEntry(content: string, entryLine: string): string {
  const trimmed = content.trimEnd();
  return trimmed ? `${trimmed}\n${entryLine}\n` : `${entryLine}\n`;
}

function trimmedSlice(lines: string[], start: number, end: number): string {
  let trimmedEnd = end;
  while (trimmedEnd > start && lines[trimmedEnd].trim() === "") {
    trimmedEnd--;
  }
  return lines.slice(start, trimmedEnd + 1).join("\n");
}

function formatDateInTimezone(nowMs: number, timezone: string): string {
  const parts = new Intl.DateTimeFormat("en-US", {
    timeZone: timezone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).formatToParts(new Date(nowMs));
  const y = parts.find((p) => p.type === "year")?.value;
  const m = parts.find((p) => p.type === "month")?.value;
  const d = parts.find((p) => p.type === "day")?.value;
  return y && m && d ? `${y}-${m}-${d}` : new Date(nowMs).toISOString().slice(0, 10);
}

function daysAgo(n: number): string {
  const d = new Date();
  d.setDate(d.getDate() - n);
  return d.toISOString().slice(0, 10);
}

async function readFile(absPath: string): Promise<string> {
  try {
    return await fs.readFile(absPath, "utf-8");
  } catch (err) {
    if (isFileMissingError(err)) {
      return "";
    }
    throw err;
  }
}

async function writeFile(absPath: string, content: string): Promise<void> {
  await fs.mkdir(path.dirname(absPath), { recursive: true });
  await fs.writeFile(absPath, content, "utf-8");
}
