#!/usr/bin/env node

import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { parseSync } from "oxc-parser";
import {
  collectTypeScriptFilesFromRoots,
  offsetToLine,
  resolveSourceRoots,
  visitNode,
} from "./lib/ts-guard-utils.mjs";

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const scanRoots = resolveSourceRoots(repoRoot, ["src", "extensions", "scripts", "test"]);

function readPackageExports() {
  const packageJson = JSON.parse(readFileSync(path.join(repoRoot, "package.json"), "utf8"));
  return new Set(
    Object.keys(packageJson.exports ?? {})
      .filter((key) => key.startsWith("./plugin-sdk/"))
      .map((key) => key.slice("./plugin-sdk/".length)),
  );
}

function readEntrypoints() {
  const entrypoints = JSON.parse(
    readFileSync(path.join(repoRoot, "scripts/lib/plugin-sdk-entrypoints.json"), "utf8"),
  );
  return new Set(entrypoints.filter((entry) => entry !== "index"));
}

function normalizePath(filePath) {
  return path.relative(repoRoot, filePath).split(path.sep).join("/");
}

function parsePluginSdkSubpath(specifier) {
  if (!specifier.startsWith("deneb/plugin-sdk/")) {
    return null;
  }
  const subpath = specifier.slice("deneb/plugin-sdk/".length);
  return subpath || null;
}

function compareEntries(left, right) {
  return (
    left.file.localeCompare(right.file) ||
    left.line - right.line ||
    left.kind.localeCompare(right.kind) ||
    left.specifier.localeCompare(right.specifier) ||
    left.subpath.localeCompare(right.subpath)
  );
}

async function collectViolations() {
  const entrypoints = readEntrypoints();
  const exports = readPackageExports();
  const files = (await collectTypeScriptFilesFromRoots(scanRoots, { includeTests: true })).toSorted(
    (left, right) => normalizePath(left).localeCompare(normalizePath(right)),
  );
  const violations = [];

  for (const filePath of files) {
    const sourceText = readFileSync(filePath, "utf8");
    const result = parseSync(filePath, sourceText);

    function push(kind, specifierNode, specifier) {
      const subpath = parsePluginSdkSubpath(specifier);
      if (!subpath) {
        return;
      }

      const missingFrom = [];
      if (!entrypoints.has(subpath)) {
        missingFrom.push("scripts/lib/plugin-sdk-entrypoints.json");
      }
      if (!exports.has(subpath)) {
        missingFrom.push("package.json exports");
      }
      if (missingFrom.length === 0) {
        return;
      }

      violations.push({
        file: normalizePath(filePath),
        line: offsetToLine(sourceText, specifierNode.start),
        kind,
        specifier,
        subpath,
        missingFrom,
      });
    }

    visitNode(result.program, (node) => {
      if (node.type === "ImportDeclaration" && node.source) {
        push("import", node.source, node.source.value);
      } else if (node.type === "ExportNamedDeclaration" && node.source) {
        push("export", node.source, node.source.value);
      } else if (node.type === "ExportAllDeclaration" && node.source) {
        push("export", node.source, node.source.value);
      } else if (
        node.type === "ImportExpression" &&
        node.source?.type === "Literal" &&
        typeof node.source.value === "string"
      ) {
        push("dynamic-import", node.source, node.source.value);
      }
    });
  }

  return violations.toSorted(compareEntries);
}

async function main() {
  const violations = await collectViolations();
  if (violations.length === 0) {
    console.log("OK: all referenced deneb/plugin-sdk/<subpath> imports are exported.");
    return;
  }

  console.error(
    "Rule: every referenced deneb/plugin-sdk/<subpath> must exist in the public package exports.",
  );
  for (const violation of violations) {
    console.error(
      `- ${violation.file}:${violation.line} [${violation.kind}] ${violation.specifier} missing from ${violation.missingFrom.join(" and ")}`,
    );
  }
  process.exit(1);
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
