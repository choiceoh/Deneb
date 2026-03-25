import type { Command } from "commander";
import type { BrowserParentOpts } from "../browser-cli-shared.js";

export function registerBrowserNavigationCommands(
  _browser: Command,
  _parentOpts: (cmd: Command) => BrowserParentOpts,
) {
  // Navigation commands (navigate, resize) removed —
  // underlying shared browser utilities were deleted.
}
