#!/usr/bin/env node

import { runCallsiteGuard } from "./lib/callsite-guard.mjs";
import {
  parseSource,
  runAsScript,
  toLine,
  unwrapExpression,
  visitNode,
} from "./lib/ts-guard-utils.mjs";

const sourceRoots = [
  "src/channels",
  "src/infra/outbound",
  "src/line",
  "src/media-understanding",
  "extensions",
];
const allowedRelativePaths = new Set(["extensions/feishu/src/dedup.ts"]);

function collectOsTmpdirImports(program) {
  const osModuleSpecifiers = new Set(["node:os", "os"]);
  const osNamespaceOrDefault = new Set();
  const namedTmpdir = new Set();
  for (const statement of program.body) {
    if (statement.type !== "ImportDeclaration" || !statement.source) {
      continue;
    }
    if (!osModuleSpecifiers.has(statement.source.value)) {
      continue;
    }
    for (const specifier of statement.specifiers ?? []) {
      if (specifier.type === "ImportDefaultSpecifier") {
        osNamespaceOrDefault.add(specifier.local.name);
      } else if (specifier.type === "ImportNamespaceSpecifier") {
        osNamespaceOrDefault.add(specifier.local.name);
      } else if (specifier.type === "ImportSpecifier") {
        const imported =
          specifier.imported?.name ?? specifier.imported?.value ?? specifier.local.name;
        if (imported === "tmpdir") {
          namedTmpdir.add(specifier.local.name);
        }
      }
    }
  }
  return { osNamespaceOrDefault, namedTmpdir };
}

export function findMessagingTmpdirCallLines(content, fileName = "source.ts") {
  const { program, sourceText } = parseSource(fileName, content);
  const { osNamespaceOrDefault, namedTmpdir } = collectOsTmpdirImports(program);
  const lines = [];

  visitNode(program, (node) => {
    if (node.type === "CallExpression") {
      const callee = unwrapExpression(node.callee);
      if (!callee) {
        return;
      }
      if (
        callee.type === "MemberExpression" &&
        !callee.computed &&
        callee.property?.type === "Identifier" &&
        callee.property.name === "tmpdir" &&
        callee.object?.type === "Identifier" &&
        osNamespaceOrDefault.has(callee.object.name)
      ) {
        lines.push(toLine(sourceText, callee));
      } else if (callee.type === "Identifier" && namedTmpdir.has(callee.name)) {
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
    findCallLines: findMessagingTmpdirCallLines,
    skipRelativePath: (relativePath) => allowedRelativePaths.has(relativePath),
    header: "Found os.tmpdir()/tmpdir() usage in messaging/channel runtime sources:",
    footer:
      "Use resolvePreferredDenebTmpDir() or plugin-sdk temp helpers instead of host tmp defaults.",
    sortViolations: false,
  });
}

runAsScript(import.meta.url, main);
