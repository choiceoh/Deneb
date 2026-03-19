import type { OpenClawConfig } from "../config/config.js";
import type { ChannelId } from "./plugins/types.js";

type TelegramInspectModule = typeof import("./read-only-account-inspect.telegram.runtime.js");

let telegramInspectModulePromise: Promise<TelegramInspectModule> | undefined;

function loadTelegramInspectModule() {
  telegramInspectModulePromise ??= import("./read-only-account-inspect.telegram.runtime.js");
  return telegramInspectModulePromise;
}

export type ReadOnlyInspectedAccount =
  | Awaited<ReturnType<TelegramInspectModule["inspectTelegramAccount"]>>;

export async function inspectReadOnlyChannelAccount(params: {
  channelId: ChannelId;
  cfg: OpenClawConfig;
  accountId?: string | null;
}): Promise<ReadOnlyInspectedAccount | null> {
  if (params.channelId === "telegram") {
    const { inspectTelegramAccount } = await loadTelegramInspectModule();
    return inspectTelegramAccount({
      cfg: params.cfg,
      accountId: params.accountId,
    });
  }
  return null;
}
