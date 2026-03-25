#!/usr/bin/env node

import { promises as fs } from "node:fs";
import path from "node:path";
import {
  collectTypeScriptFiles,
  getPropertyNameText,
  nodeText,
  parseSource,
  resolveRepoRoot,
  runAsScript,
  toLine,
  visitNode,
} from "./lib/ts-guard-utils.mjs";

const repoRoot = resolveRepoRoot(import.meta.url);

const acpCoreProtectedSources = [
  path.join(repoRoot, "src", "acp"),
  path.join(repoRoot, "src", "agents", "acp-spawn.ts"),
  path.join(repoRoot, "src", "auto-reply", "reply", "commands-acp"),
  path.join(repoRoot, "src", "infra", "outbound", "conversation-id.ts"),
];

const channelCoreProtectedSources = [
  path.join(repoRoot, "src", "channels", "thread-bindings-policy.ts"),
  path.join(repoRoot, "src", "channels", "thread-bindings-messages.ts"),
];
const acpUserFacingTextSources = [
  path.join(repoRoot, "src", "auto-reply", "reply", "commands-acp"),
];
const systemMarkLiteralGuardSources = [
  path.join(repoRoot, "src", "auto-reply", "reply", "commands-acp"),
  path.join(repoRoot, "src", "auto-reply", "reply", "dispatch-acp.ts"),
  path.join(repoRoot, "src", "auto-reply", "reply", "directive-handling.shared.ts"),
  path.join(repoRoot, "src", "channels", "thread-bindings-messages.ts"),
];

const channelIds = [
  "discord",
  "googlechat",
  "imessage",
  "irc",
  "line",
  "matrix",
  "signal",
  "slack",
  "telegram",
  "web",
  "whatsapp",
];

const channelIdSet = new Set(channelIds);
const channelSegmentRe = new RegExp(`(^|[._/-])(?:${channelIds.join("|")})([._/-]|$)`);
const comparisonOperators = new Set(["===", "!==", "==", "!="]);

const allowedViolations = new Set([]);

function isChannelsPropertyAccess(node) {
  if (node.type === "MemberExpression" && !node.computed && node.property?.type === "Identifier") {
    return node.property.name === "channels";
  }
  if (node.type === "MemberExpression" && node.computed && node.property?.type === "Literal") {
    return node.property.value === "channels";
  }
  return false;
}

function readStringLiteral(node) {
  if (node.type === "Literal" && typeof node.value === "string") {
    return node.value;
  }
  if (
    node.type === "TemplateLiteral" &&
    node.quasis?.length === 1 &&
    node.expressions?.length === 0
  ) {
    return node.quasis[0].value?.cooked ?? null;
  }
  return null;
}

function isChannelLiteralNode(node) {
  const text = readStringLiteral(node);
  return text ? channelIdSet.has(text) : false;
}

function matchesChannelModuleSpecifier(specifier) {
  return channelSegmentRe.test(specifier.replaceAll("\\", "/"));
}

const userFacingChannelNameRe =
  /\b(?:discord|telegram|slack|signal|imessage|whatsapp|google\s*chat|irc|line|matrix)\b/i;
const systemMarkLiteral = "⚙️";

function isModuleSpecifierNode(node, parentMap) {
  const parent = parentMap.get(node);
  if (!parent) {
    return false;
  }
  if (
    parent.type === "ImportDeclaration" ||
    parent.type === "ExportNamedDeclaration" ||
    parent.type === "ExportAllDeclaration"
  ) {
    return parent.source === node;
  }
  return parent.type === "ImportExpression" && parent.source === node;
}

function buildParentMap(program) {
  const map = new Map();
  visitNode(program, (node) => {
    for (const key of Object.keys(node)) {
      if (key === "type" || key === "start" || key === "end") {
        continue;
      }
      const value = node[key];
      if (Array.isArray(value)) {
        for (const child of value) {
          if (child && typeof child === "object" && child.type) {
            map.set(child, node);
          }
        }
      } else if (value && typeof value === "object" && value.type) {
        map.set(value, node);
      }
    }
  });
  return map;
}

export function findChannelAgnosticBoundaryViolations(
  content,
  fileName = "source.ts",
  options = {},
) {
  const checkModuleSpecifiers = options.checkModuleSpecifiers ?? true;
  const checkConfigPaths = options.checkConfigPaths ?? true;
  const checkChannelComparisons = options.checkChannelComparisons ?? true;
  const checkChannelAssignments = options.checkChannelAssignments ?? true;
  const moduleSpecifierMatcher = options.moduleSpecifierMatcher ?? matchesChannelModuleSpecifier;

  const { program, sourceText } = parseSource(fileName, content);
  const violations = [];

  visitNode(program, (node) => {
    // Import declarations
    if (checkModuleSpecifiers && node.type === "ImportDeclaration" && node.source) {
      const specifier = node.source.value;
      if (moduleSpecifierMatcher(specifier)) {
        violations.push({
          line: toLine(sourceText, node.source),
          reason: `imports channel module "${specifier}"`,
        });
      }
    }

    // Export declarations with source
    if (checkModuleSpecifiers && node.type === "ExportNamedDeclaration" && node.source) {
      const specifier = node.source.value;
      if (moduleSpecifierMatcher(specifier)) {
        violations.push({
          line: toLine(sourceText, node.source),
          reason: `re-exports channel module "${specifier}"`,
        });
      }
    }

    // Dynamic imports
    if (
      checkModuleSpecifiers &&
      node.type === "ImportExpression" &&
      node.source?.type === "Literal" &&
      typeof node.source.value === "string"
    ) {
      const specifier = node.source.value;
      if (moduleSpecifierMatcher(specifier)) {
        violations.push({
          line: toLine(sourceText, node.source),
          reason: `dynamically imports channel module "${specifier}"`,
        });
      }
    }

    // Config path: channels.<channelId>
    if (
      checkConfigPaths &&
      node.type === "MemberExpression" &&
      !node.computed &&
      node.property?.type === "Identifier" &&
      channelIdSet.has(node.property.name)
    ) {
      if (isChannelsPropertyAccess(node.object)) {
        violations.push({
          line: toLine(sourceText, node.property),
          reason: `references config path "channels.${node.property.name}"`,
        });
      }
    }

    // Config path: channels["<channelId>"]
    if (
      checkConfigPaths &&
      node.type === "MemberExpression" &&
      node.computed &&
      node.property?.type === "Literal" &&
      typeof node.property.value === "string" &&
      channelIdSet.has(node.property.value)
    ) {
      if (isChannelsPropertyAccess(node.object)) {
        violations.push({
          line: toLine(sourceText, node.property),
          reason: `references config path "channels[${JSON.stringify(node.property.value)}]"`,
        });
      }
    }

    // Channel comparison
    if (
      checkChannelComparisons &&
      node.type === "BinaryExpression" &&
      comparisonOperators.has(node.operator)
    ) {
      if (isChannelLiteralNode(node.left) || isChannelLiteralNode(node.right)) {
        const leftText = nodeText(sourceText, node.left);
        const rightText = nodeText(sourceText, node.right);
        violations.push({
          line: toLine(sourceText, node),
          reason: `compares with channel id literal (${leftText} ${node.operator} ${rightText})`,
        });
      }
    }

    // Channel assignment
    if (checkChannelAssignments && node.type === "Property") {
      const propName = getPropertyNameText(node.key);
      if (propName === "channel" && isChannelLiteralNode(node.value)) {
        violations.push({
          line: toLine(sourceText, node.value),
          reason: `assigns channel id literal to "channel" (${nodeText(sourceText, node.value)})`,
        });
      }
    }
  });

  return violations;
}

export function findChannelCoreReverseDependencyViolations(content, fileName = "source.ts") {
  return findChannelAgnosticBoundaryViolations(content, fileName, {
    checkModuleSpecifiers: true,
    checkConfigPaths: false,
    checkChannelComparisons: false,
    checkChannelAssignments: false,
    moduleSpecifierMatcher: matchesChannelModuleSpecifier,
  });
}

export function findAcpUserFacingChannelNameViolations(content, fileName = "source.ts") {
  const { program, sourceText } = parseSource(fileName, content);
  const parentMap = buildParentMap(program);
  const violations = [];

  visitNode(program, (node) => {
    const text = readStringLiteral(node);
    if (text && userFacingChannelNameRe.test(text) && !isModuleSpecifierNode(node, parentMap)) {
      violations.push({
        line: toLine(sourceText, node),
        reason: `user-facing text references channel name (${JSON.stringify(text)})`,
      });
    }
  });

  return violations;
}

export function findSystemMarkLiteralViolations(content, fileName = "source.ts") {
  const { program, sourceText } = parseSource(fileName, content);
  const parentMap = buildParentMap(program);
  const violations = [];

  visitNode(program, (node) => {
    const text = readStringLiteral(node);
    if (text && text.includes(systemMarkLiteral) && !isModuleSpecifierNode(node, parentMap)) {
      violations.push({
        line: toLine(sourceText, node),
        reason: `hardcoded system mark literal (${JSON.stringify(text)})`,
      });
    }
  });

  return violations;
}

const boundaryRuleSets = [
  {
    id: "acp-core",
    sources: acpCoreProtectedSources,
    scan: (content, fileName) => findChannelAgnosticBoundaryViolations(content, fileName),
  },
  {
    id: "channel-core-reverse-deps",
    sources: channelCoreProtectedSources,
    scan: (content, fileName) => findChannelCoreReverseDependencyViolations(content, fileName),
  },
  {
    id: "acp-user-facing-text",
    sources: acpUserFacingTextSources,
    scan: (content, fileName) => findAcpUserFacingChannelNameViolations(content, fileName),
  },
  {
    id: "system-mark-literal-usage",
    sources: systemMarkLiteralGuardSources,
    scan: (content, fileName) => findSystemMarkLiteralViolations(content, fileName),
  },
];

export async function main() {
  const violations = [];
  for (const ruleSet of boundaryRuleSets) {
    const files = (
      await Promise.all(
        ruleSet.sources.map(
          async (sourcePath) =>
            await collectTypeScriptFiles(sourcePath, {
              ignoreMissing: true,
            }),
        ),
      )
    ).flat();
    for (const filePath of files) {
      const relativeFile = path.relative(repoRoot, filePath);
      if (
        allowedViolations.has(`${ruleSet.id}:${relativeFile}`) ||
        allowedViolations.has(relativeFile)
      ) {
        continue;
      }
      const content = await fs.readFile(filePath, "utf8");
      for (const violation of ruleSet.scan(content, relativeFile)) {
        violations.push(`${ruleSet.id} ${relativeFile}:${violation.line}: ${violation.reason}`);
      }
    }
  }

  if (violations.length === 0) {
    return;
  }

  console.error("Found channel-specific references in channel-agnostic sources:");
  for (const violation of violations) {
    console.error(`- ${violation}`);
  }
  console.error(
    "Move channel-specific logic to channel adapters or add a justified allowlist entry.",
  );
  process.exit(1);
}

runAsScript(import.meta.url, main);
