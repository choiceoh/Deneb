#!/usr/bin/env node

import { createPairingGuardContext } from "./lib/pairing-guard-context.mjs";
import {
  collectFileViolations,
  getPropertyNameText,
  parseSource,
  runAsScript,
  toLine,
  visitNode,
} from "./lib/ts-guard-utils.mjs";

const { repoRoot, sourceRoots } = createPairingGuardContext(import.meta.url);

function isUndefinedLikeExpression(node) {
  if (node.type === "Identifier" && node.name === "undefined") {
    return true;
  }
  // oxc represents `null` as a Literal with value null
  if (node.type === "Literal" && node.value === null) {
    return true;
  }
  return false;
}

function hasRequiredAccountIdProperty(node) {
  if (node.type !== "ObjectExpression") {
    return false;
  }
  for (const property of node.properties) {
    if (property.type === "SpreadElement") {
      continue;
    }
    if (
      property.shorthand &&
      property.key?.type === "Identifier" &&
      property.key.name === "accountId"
    ) {
      return true;
    }
    if (property.type !== "Property") {
      continue;
    }
    if (getPropertyNameText(property.key) !== "accountId") {
      continue;
    }
    if (isUndefinedLikeExpression(property.value)) {
      return false;
    }
    return true;
  }
  return false;
}

export function findViolations(content, filePath) {
  const { program, sourceText } = parseSource(filePath, content);
  const violations = [];

  visitNode(program, (node) => {
    if (node.type === "CallExpression" && node.callee?.type === "Identifier") {
      const callName = node.callee.name;
      if (callName === "readChannelAllowFromStore") {
        if (node.arguments.length < 3 || isUndefinedLikeExpression(node.arguments[2])) {
          violations.push({
            line: toLine(sourceText, node),
            reason: "readChannelAllowFromStore call must pass explicit accountId as 3rd arg",
          });
        }
      } else if (
        callName === "readLegacyChannelAllowFromStore" ||
        callName === "readLegacyChannelAllowFromStoreSync"
      ) {
        violations.push({
          line: toLine(sourceText, node),
          reason: `${callName} is legacy-only; use account-scoped readChannelAllowFromStore* APIs`,
        });
      } else if (callName === "upsertChannelPairingRequest") {
        const firstArg = node.arguments?.[0];
        if (!firstArg || !hasRequiredAccountIdProperty(firstArg)) {
          violations.push({
            line: toLine(sourceText, node),
            reason: "upsertChannelPairingRequest call must include accountId in params",
          });
        }
      }
    }
  });

  return violations;
}

async function main() {
  const violations = await collectFileViolations({
    sourceRoots,
    repoRoot,
    findViolations,
  });

  if (violations.length === 0) {
    return;
  }

  console.error("Found unscoped pairing-store calls:");
  for (const violation of violations) {
    console.error(`- ${violation.path}:${violation.line} (${violation.reason})`);
  }
  process.exit(1);
}

runAsScript(import.meta.url, main);
