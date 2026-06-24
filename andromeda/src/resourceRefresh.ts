const RESOURCE_TOOL_PATTERNS: Array<[RegExp, string[]]> = [
  [/gmail|mail/, ["mail"]],
  [/calendar|event/, ["calendar", "calendar-range"]],
  [/memory|wiki|diary|topicdocs/, ["wiki", "search", "notebook"]],
  [/files?/, ["files"]],
  [/notebook/, ["notebook"]],
  [/search/, ["search"]],
  [/todo|task/, ["todo"]],
  [/workfeed/, ["workfeed"]],
  [/project|digest/, ["progress"]],
  [/cron/, ["crons"]],
  [/people|contact/, ["people"]],
];

export function relatedResourcesForResource(resource: string | undefined): string[] {
  switch (resource) {
    case "calendar":
    case "calendar-range":
      return ["calendar", "calendar-range"];
    case "wiki":
      return ["wiki", "search", "notebook"];
    default:
      return resource ? [resource] : [];
  }
}

export function relatedResourcesForTools(tools: Iterable<string>, activeResource?: string): string[] {
  const resources = new Set(relatedResourcesForResource(activeResource));
  for (const tool of tools) {
    const normalized = tool.toLowerCase();
    for (const [pattern, matches] of RESOURCE_TOOL_PATTERNS) {
      if (!pattern.test(normalized)) continue;
      for (const resource of matches) resources.add(resource);
    }
  }
  return [...resources];
}
