import { inspectTelegramAccount as inspectTelegramAccountImpl } from "deneb/plugin-sdk/telegram";

export type { InspectedTelegramAccount } from "deneb/plugin-sdk/telegram";

type InspectTelegramAccount = typeof import("deneb/plugin-sdk/telegram").inspectTelegramAccount;

export function inspectTelegramAccount(
  ...args: Parameters<InspectTelegramAccount>
): ReturnType<InspectTelegramAccount> {
  return inspectTelegramAccountImpl(...args);
}
