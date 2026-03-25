import type { Command } from "commander";
import type { BrowserParentOpts } from "../browser-cli-shared.js";
import { registerBrowserElementCommands } from "./register.element.js";
import { registerBrowserFormWaitEvalCommands } from "./register.form-wait-eval.js";
import { registerBrowserNavigationCommands } from "./register.navigation.js";

export function registerBrowserActionInputCommands(
  browser: Command,
  parentOpts: (cmd: Command) => BrowserParentOpts,
) {
  registerBrowserNavigationCommands(browser, parentOpts);
  registerBrowserElementCommands(browser, parentOpts);
  registerBrowserFormWaitEvalCommands(browser, parentOpts);
}
