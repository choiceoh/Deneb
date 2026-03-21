import fs from "node:fs/promises";
import path from "node:path";
import { isFileMissingError } from "./fs-utils.js";

/**
 * Parsed section from a memory.md file.
 * Each section starts with a markdown heading (## or ###).
 */
export type MemorySection = {
  /** Heading level (2 for ##, 3 for ###, etc.) */
  level: number;
  /** Heading text without the # prefix */
  title: string;
  /** Full content of the section including the heading line */
  content: string;
  /** Zero-based line offset in the original file */
  startLine: number;
  /** Zero-based line offset of the last line */
  endLine: number;
};

export type MemoryEntry = {
  /** Timestamp when the entry was added (ISO 8601) */
  timestamp: string;
  /** The text content of the entry */
  text: string;
  /** Optional tags for categorization */
  tags?: string[];
};

export type AddEntryOptions = {
  /** Section title to add the entry under (created if missing) */
  section?: string;
  /** Optional tags for the entry */
  tags?: string[];
  /** Timestamp override (defaults to now) */
  timestamp?: string;
};

export type UpdateSectionOptions = {
  /** New content to replace the section body (heading preserved) */
  content: string;
  /** If true, append instead of replace */
  append?: boolean;
};

const HEADING_RE = /^(#{1,6})\s+(.+)$/;
const ENTRY_TIMESTAMP_RE = /^- \*\*(\d{4}-\d{2}-\d{2}T[\d:.Z+-]+)\*\*/;

/**
 * Resolves the default memory.md file path for a workspace.
 * Prefers MEMORY.md if it exists, falls back to memory.md.
 */
export async function resolveMemoryMdPath(workspaceDir: string): Promise<string> {
  const upper = path.join(workspaceDir, "MEMORY.md");
  try {
    await fs.access(upper);
    return upper;
  } catch {
    return path.join(workspaceDir, "memory.md");
  }
}

/**
 * Reads the memory.md file content. Returns empty string if the file does not exist.
 */
export async function readMemoryMd(filePath: string): Promise<string> {
  try {
    return await fs.readFile(filePath, "utf-8");
  } catch (err) {
    if (isFileMissingError(err)) {
      return "";
    }
    throw err;
  }
}

/**
 * Writes content to a memory.md file, creating parent directories if needed.
 */
export async function writeMemoryMd(filePath: string, content: string): Promise<void> {
  await fs.mkdir(path.dirname(filePath), { recursive: true });
  await fs.writeFile(filePath, content, "utf-8");
}

/**
 * Parses a memory.md file into sections based on markdown headings.
 */
export function parseSections(content: string): MemorySection[] {
  const lines = content.split("\n");
  const sections: MemorySection[] = [];
  let currentSection: MemorySection | null = null;

  for (let i = 0; i < lines.length; i++) {
    const match = lines[i].match(HEADING_RE);
    if (match) {
      if (currentSection) {
        currentSection.endLine = i - 1;
        // Trim trailing blank lines from section content
        currentSection.content = buildSectionContent(lines, currentSection.startLine, i - 1);
        sections.push(currentSection);
      }
      currentSection = {
        level: match[1].length,
        title: match[2].trim(),
        content: "",
        startLine: i,
        endLine: i,
      };
    }
  }

  if (currentSection) {
    currentSection.endLine = lines.length - 1;
    currentSection.content = buildSectionContent(lines, currentSection.startLine, lines.length - 1);
    sections.push(currentSection);
  }

  return sections;
}

function buildSectionContent(lines: string[], start: number, end: number): string {
  // Trim trailing blank lines
  let trimmedEnd = end;
  while (trimmedEnd > start && lines[trimmedEnd].trim() === "") {
    trimmedEnd--;
  }
  return lines.slice(start, trimmedEnd + 1).join("\n");
}

/**
 * Finds a section by title (case-insensitive).
 */
export function findSection(sections: MemorySection[], title: string): MemorySection | undefined {
  const normalized = title.toLowerCase().trim();
  return sections.find((s) => s.title.toLowerCase().trim() === normalized);
}

/**
 * Lists all section titles and their heading levels.
 */
export function listSections(content: string): Array<{ level: number; title: string }> {
  return parseSections(content).map((s) => ({ level: s.level, title: s.title }));
}

/**
 * Formats a memory entry as a markdown list item.
 */
export function formatEntry(entry: MemoryEntry): string {
  const tagSuffix = entry.tags?.length ? ` \`${entry.tags.join("` `")}\`` : "";
  return `- **${entry.timestamp}** ${entry.text}${tagSuffix}`;
}

/**
 * Parses a markdown list item back into a MemoryEntry (if it matches the entry format).
 */
export function parseEntry(line: string): MemoryEntry | null {
  const match = line.match(ENTRY_TIMESTAMP_RE);
  if (!match) {
    return null;
  }
  const timestamp = match[1];
  const rest = line.slice(match[0].length).trim();

  // Extract inline code tags from the end
  const tags: string[] = [];
  const tagRe = /`([^`]+)`/g;
  let tagMatch: RegExpExecArray | null;
  let textEnd = rest.length;

  // Find tags at the end of the line
  const allTags: Array<{ tag: string; index: number; length: number }> = [];
  while ((tagMatch = tagRe.exec(rest)) !== null) {
    allTags.push({ tag: tagMatch[1], index: tagMatch.index, length: tagMatch[0].length });
  }

  // Only treat consecutive trailing backtick-tags as tags
  for (let i = allTags.length - 1; i >= 0; i--) {
    const t = allTags[i];
    const afterTag = t.index + t.length;
    if (afterTag === textEnd || rest.slice(afterTag, textEnd).trim() === "") {
      tags.unshift(t.tag);
      textEnd = t.index;
    } else {
      break;
    }
  }

  const text = rest.slice(0, textEnd).trim();
  return { timestamp, text, tags: tags.length > 0 ? tags : undefined };
}

/**
 * Adds an entry to a memory.md file under a specific section.
 * Creates the file and/or section if they don't exist.
 */
export async function addEntry(
  filePath: string,
  text: string,
  options: AddEntryOptions = {},
): Promise<void> {
  const content = await readMemoryMd(filePath);
  const timestamp = options.timestamp ?? new Date().toISOString();
  const entry = formatEntry({ timestamp, text, tags: options.tags });

  const updated = insertEntryIntoContent(content, entry, options.section);
  await writeMemoryMd(filePath, updated);
}

function insertEntryIntoContent(
  content: string,
  entry: string,
  sectionTitle: string | undefined,
): string {
  if (!sectionTitle) {
    // Append to end of file
    const trimmed = content.trimEnd();
    return trimmed ? `${trimmed}\n\n${entry}\n` : `${entry}\n`;
  }

  const sections = parseSections(content);
  const existing = findSection(sections, sectionTitle);

  if (existing) {
    // Append entry to end of existing section
    const lines = content.split("\n");
    const insertAt = existing.endLine + 1;
    lines.splice(insertAt, 0, entry);
    return lines.join("\n");
  }

  // Create new section at end
  const trimmed = content.trimEnd();
  const newSection = `## ${sectionTitle}\n\n${entry}`;
  return trimmed ? `${trimmed}\n\n${newSection}\n` : `${newSection}\n`;
}

/**
 * Updates an existing section's content.
 * If append is true, the new content is appended to the section body.
 * Otherwise, the section body is replaced (heading preserved).
 */
export async function updateSection(
  filePath: string,
  sectionTitle: string,
  options: UpdateSectionOptions,
): Promise<boolean> {
  const content = await readMemoryMd(filePath);
  const sections = parseSections(content);
  const section = findSection(sections, sectionTitle);

  if (!section) {
    return false;
  }

  const lines = content.split("\n");

  if (options.append) {
    const insertAt = section.endLine + 1;
    const newLines = options.content.split("\n");
    lines.splice(insertAt, 0, ...newLines);
  } else {
    const bodyStart = section.startLine + 1;
    const bodyLength = section.endLine - section.startLine;
    const newBody = options.content.trimStart();
    lines.splice(bodyStart, bodyLength, "", newBody);
  }

  await writeMemoryMd(filePath, lines.join("\n"));
  return true;
}

/**
 * Removes a section by title from the memory.md file.
 * Returns true if the section was found and removed.
 */
export async function removeSection(filePath: string, sectionTitle: string): Promise<boolean> {
  const content = await readMemoryMd(filePath);
  const sections = parseSections(content);
  const section = findSection(sections, sectionTitle);

  if (!section) {
    return false;
  }

  const lines = content.split("\n");

  // Find the end: either start of next same-or-higher-level section, or end of file
  let removeEnd = lines.length;
  for (const other of sections) {
    if (other.startLine > section.startLine && other.level <= section.level) {
      removeEnd = other.startLine;
      break;
    }
  }

  // Remove trailing blank lines before the next section
  while (removeEnd > section.startLine && lines[removeEnd - 1]?.trim() === "") {
    removeEnd--;
  }

  lines.splice(section.startLine, removeEnd - section.startLine);

  // Clean up double blank lines at splice point
  const result = lines
    .join("\n")
    .replace(/\n{3,}/g, "\n\n")
    .trimEnd();
  await writeMemoryMd(filePath, result ? `${result}\n` : "");
  return true;
}

/**
 * Removes a specific entry by timestamp from the memory.md file.
 * Returns true if the entry was found and removed.
 */
export async function removeEntry(filePath: string, timestamp: string): Promise<boolean> {
  const content = await readMemoryMd(filePath);
  const lines = content.split("\n");
  let found = false;

  const filtered = lines.filter((line) => {
    const match = line.match(ENTRY_TIMESTAMP_RE);
    if (match && match[1] === timestamp) {
      found = true;
      return false;
    }
    return true;
  });

  if (!found) {
    return false;
  }

  const result = filtered
    .join("\n")
    .replace(/\n{3,}/g, "\n\n")
    .trimEnd();
  await writeMemoryMd(filePath, result ? `${result}\n` : "");
  return true;
}

/**
 * Lists all entries from a memory.md file, optionally filtered by section and/or tags.
 */
export async function listEntries(
  filePath: string,
  filter?: { section?: string; tags?: string[] },
): Promise<MemoryEntry[]> {
  const content = await readMemoryMd(filePath);
  if (!content.trim()) {
    return [];
  }

  let linesToScan: string[];

  if (filter?.section) {
    const sections = parseSections(content);
    const section = findSection(sections, filter.section);
    if (!section) {
      return [];
    }
    linesToScan = section.content.split("\n");
  } else {
    linesToScan = content.split("\n");
  }

  const entries: MemoryEntry[] = [];
  for (const line of linesToScan) {
    const entry = parseEntry(line);
    if (entry) {
      if (filter?.tags?.length) {
        const entryTags = new Set(entry.tags ?? []);
        if (!filter.tags.some((t) => entryTags.has(t))) {
          continue;
        }
      }
      entries.push(entry);
    }
  }

  return entries;
}

/**
 * Initializes a new memory.md file with a basic template.
 * Returns false if the file already exists (does not overwrite).
 */
export async function initMemoryMd(
  workspaceDir: string,
  options?: { filename?: string; title?: string },
): Promise<{ created: boolean; filePath: string }> {
  const filename = options?.filename ?? "MEMORY.md";
  const filePath = path.join(workspaceDir, filename);
  const title = options?.title ?? "Memory";

  try {
    await fs.access(filePath);
    return { created: false, filePath };
  } catch {
    // File doesn't exist, create it
  }

  const template = `# ${title}\n\n## Notes\n\n## Decisions\n\n## Tasks\n`;
  await writeMemoryMd(filePath, template);
  return { created: true, filePath };
}
