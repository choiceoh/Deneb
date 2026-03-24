export function collectOption(value: string, previous: string[] = []): string[] {
  return [...previous, value];
}

export function parsePositiveIntOrUndefined(value: unknown): number | undefined {
  if (value === undefined || value === null || value === "") {
    return undefined;
  }
  if (typeof value === "number") {
    if (!Number.isFinite(value)) {
      return undefined;
    }
    const parsed = Math.trunc(value);
    return parsed > 0 ? parsed : undefined;
  }
  if (typeof value === "string") {
    const parsed = Number.parseInt(value, 10);
    if (!Number.isFinite(parsed)) {
      return undefined;
    }
    return parsed > 0 ? parsed : undefined;
  }
  return undefined;
}

export function resolveActionArgs(actionCommand?: import("commander").Command): string[] {
  if (!actionCommand) {
    return [];
  }
  const args = actionCommand.args;
  return Array.isArray(args) ? args : [];
}
