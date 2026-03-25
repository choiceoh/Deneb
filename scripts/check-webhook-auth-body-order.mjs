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

const sourceRoots = ["extensions"];
const enforcedFiles = new Set(["extensions/googlechat/src/monitor.ts"]);
const blockedCallees = new Set(["readJsonBodyWithLimit", "readRequestBodyWithLimit"]);

function getCalleeName(expression) {
  const callee = unwrapExpression(expression);
  if (!callee) {
    return null;
  }
  if (callee.type === "Identifier") {
    return callee.name;
  }
  if (
    callee.type === "MemberExpression" &&
    !callee.computed &&
    callee.property?.type === "Identifier"
  ) {
    return callee.property.name;
  }
  return null;
}

export function findBlockedWebhookBodyReadLines(content, fileName = "source.ts") {
  const { program, sourceText } = parseSource(fileName, content);
  const lines = [];
  visitNode(program, (node) => {
    if (node.type === "CallExpression") {
      const calleeName = getCalleeName(node.callee);
      if (calleeName && blockedCallees.has(calleeName)) {
        lines.push(toLine(sourceText, node.callee));
      }
    }
  });
  return lines;
}

export async function main() {
  await runCallsiteGuard({
    importMetaUrl: import.meta.url,
    sourceRoots,
    findCallLines: findBlockedWebhookBodyReadLines,
    skipRelativePath: (relPath) => !enforcedFiles.has(relPath.replaceAll(path.sep, "/")),
    header: "Found forbidden low-level body reads in auth-sensitive webhook handlers:",
    footer:
      "Use plugin-sdk webhook guards (`readJsonWebhookBodyOrReject` / `readWebhookBodyOrReject`) with explicit pre-auth/post-auth profiles.",
  });
}

runAsScript(import.meta.url, main);
