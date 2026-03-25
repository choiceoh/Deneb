#!/usr/bin/env node

import { promises as fs } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { parseSync } from "oxc-parser";
import {
  collectTypeScriptFilesFromRoots,
  offsetToLine,
  resolveSourceRoots,
  runAsScript,
  visitNode,
} from "./lib/ts-guard-utils.mjs";

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const scanRoots = resolveSourceRoots(repoRoot, ["src/plugin-sdk", "src/plugins/runtime"]);

function normalizePath(filePath) {
  return path.relative(repoRoot, filePath).split(path.sep).join("/");
}

function compareEntries(left, right) {
  return (
    left.category.localeCompare(right.category) ||
    left.file.localeCompare(right.file) ||
    left.line - right.line ||
    left.kind.localeCompare(right.kind) ||
    left.specifier.localeCompare(right.specifier) ||
    left.reason.localeCompare(right.reason)
  );
}

function resolveSpecifier(specifier, importerFile) {
  if (specifier.startsWith(".")) {
    return normalizePath(path.resolve(path.dirname(importerFile), specifier));
  }
  if (specifier.startsWith("/")) {
    return normalizePath(specifier);
  }
  return null;
}

function pushEntry(entries, entry) {
  entries.push(entry);
}

function scanPluginSdkExtensionFacadeSmells(program, sourceText, filePath) {
  const relativeFile = normalizePath(filePath);
  if (!relativeFile.startsWith("src/plugin-sdk/")) {
    return [];
  }

  const entries = [];

  visitNode(program, (node) => {
    if (
      (node.type === "ExportNamedDeclaration" || node.type === "ExportAllDeclaration") &&
      node.source
    ) {
      const specifier = node.source.value;
      const resolvedPath = resolveSpecifier(specifier, filePath);
      if (resolvedPath?.startsWith("extensions/")) {
        pushEntry(entries, {
          category: "plugin-sdk-extension-facade",
          file: relativeFile,
          line: offsetToLine(sourceText, node.source.start),
          kind: "export",
          specifier,
          resolvedPath,
          reason: "plugin-sdk public surface re-exports extension-owned implementation",
        });
      }
    }
  });

  return entries;
}

function scanRuntimeTypeImplementationSmells(program, sourceText, filePath) {
  const relativeFile = normalizePath(filePath);
  if (!/^src\/plugins\/runtime\/types(?:-[^/]+)?\.ts$/.test(relativeFile)) {
    return [];
  }

  const entries = [];

  // oxc parses `import("./path")` type nodes as TSImportType
  visitNode(program, (node) => {
    if (node.type === "TSImportType" && node.argument?.type === "TSLiteralType") {
      const literal = node.argument.literal;
      if (literal?.type === "Literal" && typeof literal.value === "string") {
        const specifier = literal.value;
        const resolvedPath = resolveSpecifier(specifier, filePath);
        if (
          resolvedPath &&
          (/^src\/plugins\/runtime\/runtime-[^/]+\.ts$/.test(resolvedPath) ||
            /^extensions\/[^/]+\/runtime-api\.[^/]+$/.test(resolvedPath))
        ) {
          pushEntry(entries, {
            category: "runtime-type-implementation-edge",
            file: relativeFile,
            line: offsetToLine(sourceText, literal.start),
            kind: "import-type",
            specifier,
            resolvedPath,
            reason: "runtime type file references implementation shim directly",
          });
        }
      }
    }
  });

  return entries;
}

function scanRuntimeServiceLocatorSmells(program, sourceText, filePath) {
  const relativeFile = normalizePath(filePath);
  if (
    !relativeFile.startsWith("src/plugin-sdk/") &&
    !relativeFile.startsWith("src/plugins/runtime/")
  ) {
    return [];
  }

  const entries = [];
  const exportedNames = new Set();
  const runtimeStoreCalls = [];
  const mutableStateNodes = [];

  for (const statement of program.body) {
    if (statement.type === "FunctionDeclaration" && statement.id) {
      // Check if it's an export (ExportNamedDeclaration wraps it, or check top-level)
      // In oxc ESTree, exported functions are wrapped in ExportNamedDeclaration
    }

    if (statement.type === "ExportNamedDeclaration" && statement.declaration) {
      const decl = statement.declaration;
      if (decl.type === "FunctionDeclaration" && decl.id) {
        exportedNames.add(decl.id.name);
      } else if (decl.type === "VariableDeclaration") {
        for (const declarator of decl.declarations) {
          if (declarator.id?.type === "Identifier") {
            exportedNames.add(declarator.id.name);
          }
        }
        // Check for mutable (let) — exported let is mutable but exported
        // The original only flagged non-exported let
      }
    }

    // Non-exported variable statements
    if (statement.type === "VariableDeclaration" && statement.kind === "let") {
      for (const declarator of statement.declarations) {
        if (declarator.id?.type === "Identifier") {
          mutableStateNodes.push(declarator.id);
        }
      }
    }
  }

  visitNode(program, (node) => {
    if (
      node.type === "CallExpression" &&
      node.callee?.type === "Identifier" &&
      node.callee.name === "createPluginRuntimeStore"
    ) {
      runtimeStoreCalls.push(node.callee);
    }
  });

  const getterNames = [...exportedNames].filter((name) => /^get[A-Z]/.test(name));
  const setterNames = [...exportedNames].filter((name) => /^set[A-Z]/.test(name));

  if (runtimeStoreCalls.length > 0 && getterNames.length > 0 && setterNames.length > 0) {
    for (const callNode of runtimeStoreCalls) {
      pushEntry(entries, {
        category: "runtime-service-locator",
        file: relativeFile,
        line: offsetToLine(sourceText, callNode.start),
        kind: "runtime-store",
        specifier: "createPluginRuntimeStore",
        resolvedPath: relativeFile,
        reason: `exports paired runtime accessors (${getterNames.join(", ")} / ${setterNames.join(", ")}) over module-global store state`,
      });
    }
  }

  if (mutableStateNodes.length > 0 && getterNames.length > 0 && setterNames.length > 0) {
    for (const identifier of mutableStateNodes) {
      pushEntry(entries, {
        category: "runtime-service-locator",
        file: relativeFile,
        line: offsetToLine(sourceText, identifier.start),
        kind: "mutable-state",
        specifier: identifier.name,
        resolvedPath: relativeFile,
        reason: `module-global mutable state backs exported runtime accessors (${getterNames.join(", ")} / ${setterNames.join(", ")})`,
      });
    }
  }

  return entries;
}

export async function collectArchitectureSmells() {
  const files = (await collectTypeScriptFilesFromRoots(scanRoots)).toSorted((left, right) =>
    normalizePath(left).localeCompare(normalizePath(right)),
  );

  const inventory = [];
  for (const filePath of files) {
    const source = await fs.readFile(filePath, "utf8");
    const result = parseSync(filePath, source);
    inventory.push(...scanPluginSdkExtensionFacadeSmells(result.program, source, filePath));
    inventory.push(...scanRuntimeTypeImplementationSmells(result.program, source, filePath));
    inventory.push(...scanRuntimeServiceLocatorSmells(result.program, source, filePath));
  }

  return inventory.toSorted(compareEntries);
}

function formatInventoryHuman(inventory) {
  if (inventory.length === 0) {
    return "No architecture smells found for the configured checks.";
  }

  const lines = ["Architecture smell inventory:"];
  let activeCategory = "";
  let activeFile = "";
  for (const entry of inventory) {
    if (entry.category !== activeCategory) {
      activeCategory = entry.category;
      activeFile = "";
      lines.push(entry.category);
    }
    if (entry.file !== activeFile) {
      activeFile = entry.file;
      lines.push(`  ${activeFile}`);
    }
    lines.push(`    - line ${entry.line} [${entry.kind}] ${entry.reason}`);
    lines.push(`      specifier: ${entry.specifier}`);
    lines.push(`      resolved: ${entry.resolvedPath}`);
  }
  return lines.join("\n");
}

export async function main(argv = process.argv.slice(2)) {
  const json = argv.includes("--json");
  const inventory = await collectArchitectureSmells();

  if (json) {
    process.stdout.write(`${JSON.stringify(inventory, null, 2)}\n`);
    return;
  }

  console.log(formatInventoryHuman(inventory));
  console.log(`${inventory.length} smell${inventory.length === 1 ? "" : "s"} found.`);
}

runAsScript(import.meta.url, main);
