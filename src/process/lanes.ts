export const enum CommandLane {
  Main = "main",
  Cron = "cron",
  Subagent = "subagent",
  Nested = "nested",
  /** Dedicated lane for parallel plugin/extension loading during startup. */
  PluginLoad = "plugin-load",
}
