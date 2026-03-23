// Tool-specific detail resolvers for tool display.
// Extracted from tool-display-common.ts — each function resolves
// a human-readable detail string for a specific tool type.

type ArgsRecord = Record<string, unknown>;

function asRecord(args: unknown): ArgsRecord | undefined {
  return args && typeof args === "object" ? (args as ArgsRecord) : undefined;
}

export function resolvePathArg(args: unknown): string | undefined {
  const record = asRecord(args);
  if (!record) {
    return undefined;
  }
  for (const candidate of [record.path, record.file_path, record.filePath]) {
    if (typeof candidate !== "string") {
      continue;
    }
    const trimmed = candidate.trim();
    if (trimmed) {
      return trimmed;
    }
  }
  return undefined;
}

export function resolveReadDetail(args: unknown): string | undefined {
  const record = asRecord(args);
  if (!record) {
    return undefined;
  }

  const path = resolvePathArg(record);
  if (!path) {
    return undefined;
  }

  const offsetRaw =
    typeof record.offset === "number" && Number.isFinite(record.offset)
      ? Math.floor(record.offset)
      : undefined;
  const limitRaw =
    typeof record.limit === "number" && Number.isFinite(record.limit)
      ? Math.floor(record.limit)
      : undefined;

  const offset = offsetRaw !== undefined ? Math.max(1, offsetRaw) : undefined;
  const limit = limitRaw !== undefined ? Math.max(1, limitRaw) : undefined;

  if (offset !== undefined && limit !== undefined) {
    const unit = limit === 1 ? "line" : "lines";
    return `${unit} ${offset}-${offset + limit - 1} from ${path}`;
  }
  if (offset !== undefined) {
    return `from line ${offset} in ${path}`;
  }
  if (limit !== undefined) {
    const unit = limit === 1 ? "line" : "lines";
    return `first ${limit} ${unit} of ${path}`;
  }
  return `from ${path}`;
}

export function resolveWriteDetail(toolKey: string, args: unknown): string | undefined {
  const record = asRecord(args);
  if (!record) {
    return undefined;
  }

  const path =
    resolvePathArg(record) ?? (typeof record.url === "string" ? record.url.trim() : undefined);
  if (!path) {
    return undefined;
  }

  if (toolKey === "attach") {
    return `from ${path}`;
  }

  const destinationPrefix = toolKey === "edit" ? "in" : "to";
  const content =
    typeof record.content === "string"
      ? record.content
      : typeof record.newText === "string"
        ? record.newText
        : typeof record.new_string === "string"
          ? record.new_string
          : undefined;

  if (content && content.length > 0) {
    return `${destinationPrefix} ${path} (${content.length} chars)`;
  }

  return `${destinationPrefix} ${path}`;
}

export function resolveWebSearchDetail(args: unknown): string | undefined {
  const record = asRecord(args);
  if (!record) {
    return undefined;
  }

  const query = typeof record.query === "string" ? record.query.trim() : undefined;
  const count =
    typeof record.count === "number" && Number.isFinite(record.count) && record.count > 0
      ? Math.floor(record.count)
      : undefined;

  if (!query) {
    return undefined;
  }

  return count !== undefined ? `for "${query}" (top ${count})` : `for "${query}"`;
}

export function resolveWebFetchDetail(args: unknown): string | undefined {
  const record = asRecord(args);
  if (!record) {
    return undefined;
  }

  const url = typeof record.url === "string" ? record.url.trim() : undefined;
  if (!url) {
    return undefined;
  }

  const mode = typeof record.extractMode === "string" ? record.extractMode.trim() : undefined;
  const maxChars =
    typeof record.maxChars === "number" && Number.isFinite(record.maxChars) && record.maxChars > 0
      ? Math.floor(record.maxChars)
      : undefined;

  const suffix = [
    mode ? `mode ${mode}` : undefined,
    maxChars !== undefined ? `max ${maxChars} chars` : undefined,
  ]
    .filter((value): value is string => Boolean(value))
    .join(", ");

  return suffix ? `from ${url} (${suffix})` : `from ${url}`;
}
