import { sendMessageTelegram as sendMessageTelegramImpl } from "deneb/plugin-sdk/telegram";

type RuntimeSend = {
  sendMessage: typeof import("deneb/plugin-sdk/telegram").sendMessageTelegram;
};

export const runtimeSend = {
  sendMessage: sendMessageTelegramImpl,
} satisfies RuntimeSend;
