#!/usr/bin/env node

import ts from "typescript";
import { runCallsiteGuard } from "./lib/callsite-guard.mjs";
import { runAsScript, toLine, unwrapExpression } from "./lib/ts-guard-utils.mjs";

const sourceRoots = ["src/channels", "src/routing", "src/line", "extensions"];

// Temporary allowlist for legacy callsites. New raw fetch callsites in channel/plugin runtime
// code should be rejected and migrated to fetchWithSsrFGuard/shared channel helpers.
const allowedRawFetchCallsites = new Set(["extensions/telegram/src/api-fetch.ts:8"]);

function isRawFetchCall(expression) {
  const callee = unwrapExpression(expression);
  if (ts.isIdentifier(callee)) {
    return callee.text === "fetch";
  }
  if (ts.isPropertyAccessExpression(callee)) {
    return (
      ts.isIdentifier(callee.expression) &&
      callee.expression.text === "globalThis" &&
      callee.name.text === "fetch"
    );
  }
  return false;
}

export function findRawFetchCallLines(content, fileName = "source.ts") {
  const sourceFile = ts.createSourceFile(fileName, content, ts.ScriptTarget.Latest, true);
  const lines = [];
  const visit = (node) => {
    if (ts.isCallExpression(node) && isRawFetchCall(node.expression)) {
      lines.push(toLine(sourceFile, node.expression));
    }
    ts.forEachChild(node, visit);
  };
  visit(sourceFile);
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
