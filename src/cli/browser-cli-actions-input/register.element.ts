import type { Command } from "commander";
import type { BrowserParentOpts } from "../browser-cli-shared.js";

export function registerBrowserElementCommands(
  _browser: Command,
  _parentOpts: (cmd: Command) => BrowserParentOpts,
) {
  // Element action commands (click, type, press, hover, etc.) removed —
  // underlying shared browser utilities were deleted.
}
