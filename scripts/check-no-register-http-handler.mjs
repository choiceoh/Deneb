#!/usr/bin/env node

import { runCallsiteGuard } from "./lib/callsite-guard.mjs";
import {
  parseSource,
  runAsScript,
  toLine,
  unwrapExpression,
  visitNode,
} from "./lib/ts-guard-utils.mjs";

const sourceRoots = ["src", "extensions"];

function isDeprecatedRegisterHttpHandlerCall(expression) {
  const callee = unwrapExpression(expression);
  return (
    callee?.type === "MemberExpression" &&
    callee.property?.type === "Identifier" &&
    callee.property.name === "registerHttpHandler"
  );
}

export function findDeprecatedRegisterHttpHandlerLines(content, fileName = "source.ts") {
  const { program, sourceText } = parseSource(fileName, content);
  const lines = [];
  visitNode(program, (node) => {
    if (node.type === "CallExpression" && isDeprecatedRegisterHttpHandlerCall(node.callee)) {
      lines.push(toLine(sourceText, node.callee));
    }
  });
  return lines;
}

export async function main() {
  await runCallsiteGuard({
    importMetaUrl: import.meta.url,
    sourceRoots,
    findCallLines: findDeprecatedRegisterHttpHandlerLines,
    header: "Found deprecated plugin API call registerHttpHandler(...):",
    footer:
      "Use registerHttpRoute({ path, auth, match, handler }) and registerPluginHttpRoute for dynamic webhook paths.",
  });
}

runAsScript(import.meta.url, main);
