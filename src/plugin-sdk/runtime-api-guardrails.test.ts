import { readdirSync, readFileSync } from "node:fs";
import { dirname, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { parseSync } from "oxc-parser";
import { describe, expect, it } from "vitest";

const ROOT_DIR = resolve(dirname(fileURLToPath(import.meta.url)), "..");

const RUNTIME_API_EXPORT_GUARDS: Record<string, readonly string[]> = {
  "extensions/telegram/runtime-api.ts": [
    'export type { ChannelMessageActionAdapter, ChannelPlugin, DenebConfig, DenebPluginApi, PluginRuntime, TelegramAccountConfig, TelegramActionConfig, TelegramNetworkConfig } from "deneb/plugin-sdk/telegram";',
    'export type { DenebPluginService, DenebPluginServiceContext, PluginLogger } from "deneb/plugin-sdk/core";',
    'export type { AcpRuntime, AcpRuntimeCapabilities, AcpRuntimeDoctorReport, AcpRuntimeEnsureInput, AcpRuntimeEvent, AcpRuntimeHandle, AcpRuntimeStatus, AcpRuntimeTurnInput, AcpRuntimeErrorCode, AcpSessionUpdateTag } from "deneb/plugin-sdk/acp-runtime";',
    'export { AcpRuntimeError } from "deneb/plugin-sdk/acp-runtime";',
    'export { buildTokenChannelStatusSummary, clearAccountEntryFields, DEFAULT_ACCOUNT_ID, normalizeAccountId, PAIRING_APPROVED_MESSAGE, parseTelegramTopicConversation, projectCredentialSnapshotFields, resolveConfiguredFromCredentialStatuses, resolveTelegramPollVisibility } from "deneb/plugin-sdk/telegram";',
    'export { buildChannelConfigSchema, getChatChannelMeta, jsonResult, readNumberParam, readReactionParams, readStringArrayParam, readStringOrNumberParam, readStringParam, resolvePollMaxSelections, TelegramConfigSchema } from "deneb/plugin-sdk/telegram-core";',
    'export type { TelegramProbe } from "./src/probe.js";',
    'export { auditTelegramGroupMembership, collectTelegramUnmentionedGroupIds } from "./src/audit.js";',
    'export { telegramMessageActions } from "./src/channel-actions.js";',
    'export { monitorTelegramProvider } from "./src/monitor.js";',
    'export { probeTelegram } from "./src/probe.js";',
    'export { createForumTopicTelegram, deleteMessageTelegram, editForumTopicTelegram, editMessageReplyMarkupTelegram, editMessageTelegram, pinMessageTelegram, reactMessageTelegram, renameForumTopicTelegram, sendMessageTelegram, sendPollTelegram, sendStickerTelegram, sendTypingTelegram, unpinMessageTelegram } from "./src/send.js";',
    'export { createTelegramThreadBindingManager, getTelegramThreadBindingManager, setTelegramThreadBindingIdleTimeoutBySessionKey, setTelegramThreadBindingMaxAgeBySessionKey } from "./src/thread-bindings.js";',
    'export { resolveTelegramToken } from "./src/token.js";',
  ],
} as const;

function collectRuntimeApiFiles(): string[] {
  const extensionsDir = resolve(ROOT_DIR, "..", "extensions");
  const files: string[] = [];
  const stack = [extensionsDir];
  while (stack.length > 0) {
    const current = stack.pop();
    if (!current) {
      continue;
    }
    for (const entry of readdirSync(current, { withFileTypes: true })) {
      const fullPath = resolve(current, entry.name);
      if (entry.isDirectory()) {
        if (entry.name === "node_modules" || entry.name === "dist" || entry.name === "coverage") {
          continue;
        }
        stack.push(fullPath);
        continue;
      }
      if (!entry.isFile() || entry.name !== "runtime-api.ts") {
        continue;
      }
      files.push(relative(resolve(ROOT_DIR, ".."), fullPath).replaceAll("\\", "/"));
    }
  }
  return files;
}

function readExportStatements(path: string): string[] {
  const sourceText = readFileSync(resolve(ROOT_DIR, "..", path), "utf8");
  const result = parseSync(path, sourceText);
  const program = result.program as {
    body: Array<{
      type: string;
      source?: { value: string; start: number; end: number };
      specifiers?: Array<{
        type: string;
        local: { name: string };
        exported: { name: string };
      }>;
      exportKind?: string;
      declaration?: {
        type: string;
        start: number;
        end: number;
      };
      start: number;
      end: number;
    }>;
  };

  return program.body.flatMap((statement) => {
    // ExportNamedDeclaration with source (re-export)
    if (statement.type === "ExportNamedDeclaration" && statement.source) {
      const specifiers = (statement.specifiers ?? []).map((spec) => {
        const imported = spec.local.name;
        const exported = spec.exported.name;
        const alias = imported !== exported ? `${imported} as ${exported}` : exported;
        // Check if specifier is type-only
        if ((spec as Record<string, unknown>).exportKind === "type") {
          return `type ${alias}`;
        }
        return alias;
      });
      const exportPrefix = statement.exportKind === "type" ? "export type" : "export";
      return [`${exportPrefix} { ${specifiers.join(", ")} } from "${statement.source.value}";`];
    }

    // ExportNamedDeclaration with declaration (export function/class/const)
    if (statement.type === "ExportNamedDeclaration" && statement.declaration) {
      const text = sourceText.slice(statement.start, statement.end).replace(/\s+/g, " ").trim();
      return [text];
    }

    // ExportAllDeclaration
    if (statement.type === "ExportAllDeclaration" && statement.source) {
      const prefix = statement.exportKind === "type" ? "export type *" : "export *";
      return [`${prefix} from "${statement.source.value}";`];
    }

    return [];
  });
}

describe("runtime api guardrails", () => {
  it("keeps runtime api surfaces on an explicit export allowlist", () => {
    const runtimeApiFiles = collectRuntimeApiFiles();
    expect(runtimeApiFiles).toEqual(
      expect.arrayContaining(Object.keys(RUNTIME_API_EXPORT_GUARDS).toSorted()),
    );

    for (const file of Object.keys(RUNTIME_API_EXPORT_GUARDS).toSorted()) {
      expect(readExportStatements(file), `${file} runtime api exports changed`).toEqual(
        RUNTIME_API_EXPORT_GUARDS[file],
      );
    }
  });
});
