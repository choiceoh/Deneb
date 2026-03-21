/**
 * Config integrity guard — rejects destructive writes that would break the
 * running gateway or lose critical operator-set configuration.
 *
 * The guard runs *after* Zod schema validation and *before* the atomic write.
 * It compares the outgoing config against the on-disk snapshot and blocks
 * writes that look like accidental bulk-deletions or critical-key removals —
 * the kind of mistakes an AI agent (or a bad script) would make.
 *
 * Callers can bypass the guard by passing `force: true` in ConfigWriteOptions
 * or by setting DENEB_CONFIG_FORCE_WRITE=1 in the environment.
 */

export type IntegrityViolation = {
  code: string;
  message: string;
};

export type IntegrityCheckParams = {
  /** The previously-persisted config (snapshot.resolved / snapshot.config). */
  previous: Record<string, unknown>;
  /** The config about to be written. */
  next: Record<string, unknown>;
  /** Byte length of the previous config file. */
  previousBytes: number | null;
  /** Byte length of the next config payload. */
  nextBytes: number | null;
};

/**
 * Top-level keys whose removal (when previously present) is considered a
 * destructive change that must be explicitly forced.
 */
const CRITICAL_KEYS: ReadonlySet<string> = new Set([
  "gateway",
  "models",
  "agents",
  "channels",
  "secrets",
  "auth",
]);

/**
 * Minimum previous file size (bytes) before the size-drop guard kicks in.
 * Tiny configs (< 256 B) are likely test/empty and don't need the check.
 */
const SIZE_DROP_THRESHOLD_BYTES = 256;

/**
 * Maximum allowed size shrinkage ratio.  If the next payload is smaller than
 * 40 % of the previous payload the write is blocked.
 */
const SIZE_DROP_MAX_RATIO = 0.4;

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

/**
 * Run all integrity checks and return a list of violations (empty = OK).
 */
export function checkConfigIntegrity(params: IntegrityCheckParams): IntegrityViolation[] {
  const violations: IntegrityViolation[] = [];

  // --- 1. Critical-key removal guard ---
  for (const key of CRITICAL_KEYS) {
    const hadKey =
      isPlainObject(params.previous) &&
      key in params.previous &&
      params.previous[key] !== undefined;
    const hasKey =
      isPlainObject(params.next) && key in params.next && params.next[key] !== undefined;

    if (hadKey && !hasKey) {
      violations.push({
        code: "CRITICAL_KEY_REMOVED",
        message: `Critical config key "${key}" was removed. Use force to override.`,
      });
    }
  }

  // --- 2. Bulk key removal guard ---
  if (isPlainObject(params.previous) && isPlainObject(params.next)) {
    const prevKeys = Object.keys(params.previous).filter((k) => params.previous[k] !== undefined);
    const nextKeys = new Set(Object.keys(params.next).filter((k) => params.next[k] !== undefined));
    const removedCount = prevKeys.filter((k) => !nextKeys.has(k)).length;
    // Block if more than half of the top-level keys were removed at once.
    if (prevKeys.length >= 4 && removedCount > Math.floor(prevKeys.length / 2)) {
      violations.push({
        code: "BULK_KEY_REMOVAL",
        message: `${removedCount} of ${prevKeys.length} top-level keys were removed in a single write. Use force to override.`,
      });
    }
  }

  // --- 3. Size-drop guard ---
  if (
    typeof params.previousBytes === "number" &&
    typeof params.nextBytes === "number" &&
    params.previousBytes >= SIZE_DROP_THRESHOLD_BYTES &&
    params.nextBytes < Math.floor(params.previousBytes * SIZE_DROP_MAX_RATIO)
  ) {
    violations.push({
      code: "SIZE_DROP",
      message: `Config size dropped from ${params.previousBytes} to ${params.nextBytes} bytes (>${Math.round((1 - SIZE_DROP_MAX_RATIO) * 100)}% reduction). Use force to override.`,
    });
  }

  return violations;
}

export class ConfigIntegrityError extends Error {
  violations: IntegrityViolation[];

  constructor(violations: IntegrityViolation[]) {
    const summary = violations.map((v) => `  [${v.code}] ${v.message}`).join("\n");
    super(`Config integrity check failed:\n${summary}`);
    this.name = "ConfigIntegrityError";
    this.violations = violations;
  }
}
