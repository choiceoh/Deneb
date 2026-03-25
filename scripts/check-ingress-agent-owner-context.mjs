#!/usr/bin/env node

import path from "node:path";
import { runCallsiteGuard } from "./lib/callsite-guard.mjs";
import {
  parseSource,
  runAsScript,
  toLine,
  unwrapExpression,
  visitNode,
} from "./lib/ts-guard-utils.mjs";

const sourceRoots = ["src/gateway"];
const enforcedFiles = new Set([
  "src/gateway/openai-http.ts",
  "src/gateway/openresponses-http.ts",
  "src/gateway/server-methods/agent.ts",
  "src/gateway/server-node-events.ts",
]);

export function findLegacyAgentCommandCallLines(content, fileName = "source.ts") {
  const { program, sourceText } = parseSource(fileName, content);
  const lines = [];
  visitNode(program, (node) => {
    if (node.type === "CallExpression") {
      const callee = unwrapExpression(node.callee);
      if (callee?.type === "Identifier" && callee.name === "agentCommand") {
        lines.push(toLine(sourceText, callee));
      }
    }
  });
  return lines;
}

export async function main() {
  await runCallsiteGuard({
    importMetaUrl: import.meta.url,
    sourceRoots,
    findCallLines: findLegacyAgentCommandCallLines,
    skipRelativePath: (relPath) => !enforcedFiles.has(relPath.replaceAll(path.sep, "/")),
    header: "Found ingress callsites using local agentCommand() (must be explicit owner-aware):",
    footer:
      "Use agentCommandFromIngress(...) and pass senderIsOwner explicitly at ingress boundaries.",
  });
}

runAsScript(import.meta.url, main);
