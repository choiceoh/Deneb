import type { Command } from "commander";
import type { BrowserParentOpts } from "../browser-cli-shared.js";

export function registerBrowserFormWaitEvalCommands(
  _browser: Command,
  _parentOpts: (cmd: Command) => BrowserParentOpts,
) {
  // Form/wait/eval commands (fill, wait, evaluate) removed —
  // underlying shared browser utilities were deleted.
}
