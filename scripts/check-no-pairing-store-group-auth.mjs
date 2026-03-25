#!/usr/bin/env node

import { createPairingGuardContext } from "./lib/pairing-guard-context.mjs";
import {
  collectFileViolations,
  getPropertyNameText,
  nodeText,
  parseSource,
  runAsScript,
  toLine,
  visitNode,
} from "./lib/ts-guard-utils.mjs";

const { repoRoot, sourceRoots, resolveFromRepo } = createPairingGuardContext(import.meta.url);

const allowedFiles = new Set([
  resolveFromRepo("src/security/dm-policy-shared.ts"),
  resolveFromRepo("src/channels/allow-from.ts"),
  // Config migration/audit logic may intentionally reference store + group fields.
  resolveFromRepo("src/security/fix.ts"),
  resolveFromRepo("src/security/audit-channel.ts"),
]);

const storeIdentifierRe = /^(?:storeAllowFrom|storedAllowFrom|storeAllowList)$/i;
const groupNameRe =
  /(?:groupAllowFrom|effectiveGroupAllowFrom|groupAllowed|groupAllow|groupAuth|groupSender)/i;
const storeSourceCallNames = new Set([
  "readChannelAllowFromStore",
  "readChannelAllowFromStoreSync",
  "readStoreAllowFromForDmPolicy",
]);
const allowedResolverCallNames = new Set([
  "resolveEffectiveAllowFromLists",
  "resolveDmGroupAccessWithLists",
  "resolveMattermostEffectiveAllowFromLists",
  "resolveIrcEffectiveAllowlists",
]);

function getDeclarationNameText(name) {
  if (name.type === "Identifier") {
    return name.name;
  }
  if (name.type === "ObjectPattern" || name.type === "ArrayPattern") {
    return null;
  }
  return null;
}

function containsPairingStoreSource(node) {
  let found = false;
  visitNode(node, (current) => {
    if (found) {
      return;
    }
    if (current.type === "Identifier" && storeIdentifierRe.test(current.name)) {
      found = true;
      return;
    }
    if (current.type === "CallExpression") {
      const callName = getCallName(current);
      if (callName && storeSourceCallNames.has(callName)) {
        found = true;
      }
    }
  });
  return found;
}

function getCallName(node) {
  if (node.type !== "CallExpression") {
    return null;
  }
  if (node.callee?.type === "Identifier") {
    return node.callee.name;
  }
  if (
    node.callee?.type === "MemberExpression" &&
    !node.callee.computed &&
    node.callee.property?.type === "Identifier"
  ) {
    return node.callee.property.name;
  }
  return null;
}

function isSuspiciousNormalizeWithStoreCall(node, sourceText) {
  if (node.type !== "CallExpression") {
    return false;
  }
  if (node.callee?.type !== "Identifier" || node.callee.name !== "normalizeAllowFromWithStore") {
    return false;
  }
  const firstArg = node.arguments?.[0];
  if (!firstArg || firstArg.type !== "ObjectExpression") {
    return false;
  }
  let hasStoreProp = false;
  let hasGroupAllowProp = false;
  for (const property of firstArg.properties) {
    if (property.type !== "Property") {
      continue;
    }
    const name = getPropertyNameText(property.key);
    if (!name) {
      continue;
    }
    if (name === "storeAllowFrom" && containsPairingStoreSource(property.value)) {
      hasStoreProp = true;
    }
    if (name === "allowFrom" && groupNameRe.test(nodeText(sourceText, property.value))) {
      hasGroupAllowProp = true;
    }
  }
  return hasStoreProp && hasGroupAllowProp;
}

export function findViolations(content, filePath) {
  const { program, sourceText } = parseSource(filePath, content);
  const violations = [];

  visitNode(program, (node) => {
    if (node.type === "VariableDeclarator" && node.init) {
      const name = node.id ? getDeclarationNameText(node.id) : null;
      if (name && groupNameRe.test(name) && containsPairingStoreSource(node.init)) {
        const callName = getCallName(node.init);
        if (callName && allowedResolverCallNames.has(callName)) {
          return;
        }
        violations.push({
          line: toLine(sourceText, node),
          reason: `group-scoped variable "${name}" references pairing-store identifiers`,
        });
      }
    }

    if (node.type === "Property" && node.kind === "init") {
      const propName = getPropertyNameText(node.key);
      if (propName && groupNameRe.test(propName) && containsPairingStoreSource(node.value)) {
        violations.push({
          line: toLine(sourceText, node),
          reason: `group-scoped property "${propName}" references pairing-store identifiers`,
        });
      }
    }

    if (isSuspiciousNormalizeWithStoreCall(node, sourceText)) {
      violations.push({
        line: toLine(sourceText, node),
        reason: "group allowlist uses normalizeAllowFromWithStore(...) with pairing-store entries",
      });
    }
  });

  return violations;
}

async function main() {
  const violations = await collectFileViolations({
    sourceRoots,
    repoRoot,
    findViolations,
    skipFile: (filePath) => allowedFiles.has(filePath),
  });

  if (violations.length === 0) {
    return;
  }

  console.error("Found pairing-store identifiers referenced in group auth composition:");
  for (const violation of violations) {
    console.error(`- ${violation.path}:${violation.line} (${violation.reason})`);
  }
  console.error(
    "Group auth must be composed via shared resolvers (resolveDmGroupAccessWithLists / resolveEffectiveAllowFromLists).",
  );
  process.exit(1);
}

runAsScript(import.meta.url, main);
