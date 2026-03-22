import { definePluginEntry } from "deneb/plugin-sdk/core";
import {
  createDevCheckTool,
  createDevTestTool,
  createDevBuildTool,
  createDevFindRelatedTool,
  createDevGitSummaryTool,
} from "./src/tools.js";

export default definePluginEntry({
  id: "ai-dev-tools",
  name: "AI Developer Tools",
  description: "Built-in tools for AI agents patching and modifying the Deneb codebase",
  register(api) {
    api.registerTool(
      (ctx) => [
        createDevCheckTool({ workspaceDir: ctx.workspaceDir }),
        createDevTestTool({ workspaceDir: ctx.workspaceDir }),
        createDevBuildTool({ workspaceDir: ctx.workspaceDir }),
        createDevFindRelatedTool({ workspaceDir: ctx.workspaceDir }),
        createDevGitSummaryTool({ workspaceDir: ctx.workspaceDir }),
      ],
      { names: ["dev_check", "dev_test", "dev_build", "dev_find_related", "dev_git_summary"] },
    );
  },
});
