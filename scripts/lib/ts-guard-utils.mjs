import { promises as fs } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { parseSync } from "oxc-parser";

const baseTestSuffixes = [".test.ts", ".test-utils.ts", ".test-harness.ts", ".e2e-harness.ts"];

export function resolveRepoRoot(importMetaUrl) {
  return path.resolve(path.dirname(fileURLToPath(importMetaUrl)), "..", "..");
}

export function resolveSourceRoots(repoRoot, relativeRoots) {
  return relativeRoots.map((root) => path.join(repoRoot, ...root.split("/").filter(Boolean)));
}

export function isTestLikeTypeScriptFile(filePath, options = {}) {
  const extraTestSuffixes = options.extraTestSuffixes ?? [];
  return [...baseTestSuffixes, ...extraTestSuffixes].some((suffix) => filePath.endsWith(suffix));
}

export async function collectTypeScriptFiles(targetPath, options = {}) {
  const includeTests = options.includeTests ?? false;
  const extraTestSuffixes = options.extraTestSuffixes ?? [];
  const skipNodeModules = options.skipNodeModules ?? true;
  const ignoreMissing = options.ignoreMissing ?? false;

  let stat;
  try {
    stat = await fs.stat(targetPath);
  } catch (error) {
    if (
      ignoreMissing &&
      error &&
      typeof error === "object" &&
      "code" in error &&
      error.code === "ENOENT"
    ) {
      return [];
    }
    throw error;
  }

  if (stat.isFile()) {
    if (!targetPath.endsWith(".ts")) {
      return [];
    }
    if (!includeTests && isTestLikeTypeScriptFile(targetPath, { extraTestSuffixes })) {
      return [];
    }
    return [targetPath];
  }

  const entries = await fs.readdir(targetPath, { withFileTypes: true });
  const out = [];
  for (const entry of entries) {
    const entryPath = path.join(targetPath, entry.name);
    if (entry.isDirectory()) {
      if (skipNodeModules && entry.name === "node_modules") {
        continue;
      }
      out.push(...(await collectTypeScriptFiles(entryPath, options)));
      continue;
    }
    if (!entry.isFile() || !entryPath.endsWith(".ts")) {
      continue;
    }
    if (!includeTests && isTestLikeTypeScriptFile(entryPath, { extraTestSuffixes })) {
      continue;
    }
    out.push(entryPath);
  }
  return out;
}

export async function collectTypeScriptFilesFromRoots(sourceRoots, options = {}) {
  return (
    await Promise.all(
      sourceRoots.map(
        async (root) =>
          await collectTypeScriptFiles(root, {
            ignoreMissing: true,
            ...options,
          }),
      ),
    )
  ).flat();
}

export async function collectFileViolations(params) {
  const files = await collectTypeScriptFilesFromRoots(params.sourceRoots, {
    extraTestSuffixes: params.extraTestSuffixes,
  });

  const violations = [];
  for (const filePath of files) {
    if (params.skipFile?.(filePath)) {
      continue;
    }
    const content = await fs.readFile(filePath, "utf8");
    const fileViolations = params.findViolations(content, filePath);
    for (const violation of fileViolations) {
      violations.push({
        path: path.relative(params.repoRoot, filePath),
        ...violation,
      });
    }
  }
  return violations;
}

// --- oxc-parser based AST utilities ---

/** Parse TypeScript source into an oxc ESTree AST. */
export function parseSource(fileName, content) {
  const result = parseSync(fileName, content);
  return { program: result.program, sourceText: content };
}

/** Convert a byte offset to a 1-based line number. */
export function offsetToLine(sourceText, offset) {
  let line = 1;
  for (let i = 0; i < offset && i < sourceText.length; i++) {
    if (sourceText.charCodeAt(i) === 10) {
      line++;
    }
  }
  return line;
}

/**
 * Compatibility shim: compute 1-based line number from an AST node.
 * Works with both the old `toLine(sourceFile, node)` call pattern
 * and the new oxc-based pattern.
 */
export function toLine(sourceTextOrFile, node) {
  // sourceTextOrFile is the raw source string (new oxc path)
  if (typeof sourceTextOrFile === "string") {
    return offsetToLine(sourceTextOrFile, node.start);
  }
  // Legacy ts.SourceFile path — should not be reached after full migration
  return sourceTextOrFile.getLineAndCharacterOfPosition(node.getStart(sourceTextOrFile)).line + 1;
}

/** Extract the source text slice for an AST node. */
export function nodeText(sourceText, node) {
  return sourceText.slice(node.start, node.end);
}

/** Get the text of a property name node (Identifier, StringLiteral, NumericLiteral). */
export function getPropertyNameText(name) {
  if (name.type === "Identifier") {
    return name.name;
  }
  if (name.type === "StringLiteral" || name.type === "NumericLiteral") {
    return typeof name.value === "number" ? String(name.value) : name.value;
  }
  // oxc Literal node
  if (name.type === "Literal") {
    return name.value != null ? String(name.value) : null;
  }
  return null;
}

/** Unwrap parenthesized, as, satisfies, and non-null expressions. */
export function unwrapExpression(expression) {
  let current = expression;
  while (true) {
    if (!current) {
      return current;
    }
    const t = current.type;
    if (
      t === "ParenthesizedExpression" ||
      t === "TSAsExpression" ||
      t === "TSSatisfiesExpression" ||
      t === "TSTypeAssertion" ||
      t === "TSNonNullExpression" ||
      t === "TSInstantiationExpression"
    ) {
      current = current.expression;
      continue;
    }
    return current;
  }
}

/** Walk all descendant nodes of an AST node, calling visitor on each. */
export function visitNode(node, visitor) {
  if (!node || typeof node !== "object") {
    return;
  }
  visitor(node);
  for (const key of Object.keys(node)) {
    if (key === "type" || key === "start" || key === "end") {
      continue;
    }
    const value = node[key];
    if (Array.isArray(value)) {
      for (const child of value) {
        if (child && typeof child === "object" && child.type) {
          visitNode(child, visitor);
        }
      }
    } else if (value && typeof value === "object" && value.type) {
      visitNode(value, visitor);
    }
  }
}

/** Check if a node is an ImportExpression (dynamic import). */
export function isImportExpression(node) {
  return node.type === "ImportExpression";
}

/** Get the module specifier string from an import/export declaration. */
export function getModuleSpecifier(node) {
  if (node.type === "ImportDeclaration" && node.source) {
    return node.source.value;
  }
  if (node.type === "ExportNamedDeclaration" && node.source) {
    return node.source.value;
  }
  if (node.type === "ExportAllDeclaration" && node.source) {
    return node.source.value;
  }
  return null;
}

export function isDirectExecution(importMetaUrl) {
  const entry = process.argv[1];
  if (!entry) {
    return false;
  }
  return path.resolve(entry) === fileURLToPath(importMetaUrl);
}

export function runAsScript(importMetaUrl, main) {
  if (!isDirectExecution(importMetaUrl)) {
    return;
  }
  main().catch((error) => {
    console.error(error);
    process.exit(1);
  });
}
