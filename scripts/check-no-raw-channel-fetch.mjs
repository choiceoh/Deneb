#!/usr/bin/env node

import { runCallsiteGuard } from "./lib/callsite-guard.mjs";
import {
  parseSource,
  runAsScript,
  toLine,
  unwrapExpression,
  visitNode,
} from "./lib/ts-guard-utils.mjs";

const sourceRoots = ["src/channels", "src/routing", "src/line", "extensions"];

// Temporary allowlist for legacy callsites. New raw fetch callsites in channel/plugin runtime
// code should be rejected and migrated to fetchWithSsrFGuard/shared channel helpers.
const allowedRawFetchCallsites = new Set(["extensions/telegram/src/api-fetch.ts:8"]);

function isRawFetchCall(expression) {
  const callee = unwrapExpression(expression);
  if (!callee) {
    return false;
  }
  if (callee.type === "Identifier") {
    return callee.name === "fetch";
  }
  if (callee.type === "MemberExpression" && !callee.computed) {
    return (
      callee.object?.type === "Identifier" &&
      callee.object.name === "globalThis" &&
      callee.property?.type === "Identifier" &&
      callee.property.name === "fetch"
    );
  }
  return false;
}

export function findRawFetchCallLines(content, fileName = "source.ts") {
  const { program, sourceText } = parseSource(fileName, content);
  const lines = [];
  visitNode(program, (node) => {
    if (node.type === "CallExpression" && isRawFetchCall(node.callee)) {
      lines.push(toLine(sourceText, node.callee));
    }
  });
  return lines;
}

export async function main() {
  await runCallsiteGuard({
    importMetaUrl: import.meta.url,
    sourceRoots,
    extraTestSuffixes: [".browser.test.ts", ".node.test.ts"],
    findCallLines: findRawFetchCallLines,
    allowCallsite: (callsite) => allowedRawFetchCallsites.has(callsite),
    header: "Found raw fetch() usage in channel/plugin runtime sources outside allowlist:",
    footer: "Use fetchWithSsrFGuard() or existing channel/plugin SDK wrappers for network calls.",
  });
}

runAsScript(import.meta.url, main);
