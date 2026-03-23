import { loadVoiceWakeConfig, setVoiceWakeTriggers } from "../../../infra/voicewake.js";
import { ErrorCodes, errorShape } from "../../protocol/index.js";
import { normalizeVoiceWakeTriggers } from "../../server-utils.js";
import { formatForLog } from "../../ws/ws-log.js";
import type { GatewayRequestHandlers } from "../types.js";

export const voicewakeHandlers: GatewayRequestHandlers = {
  "voicewake.get": async ({ respond }) => {
    try {
      const cfg = await loadVoiceWakeConfig();
      respond(true, { triggers: cfg.triggers });
    } catch (err) {
      respond(false, undefined, errorShape(ErrorCodes.DEPENDENCY_FAILED, formatForLog(err)));
    }
  },
  "voicewake.set": async ({ params, respond, context }) => {
    if (!Array.isArray(params.triggers)) {
      respond(
        false,
        undefined,
        errorShape(ErrorCodes.MISSING_PARAM, "voicewake.set requires triggers: string[]"),
      );
      return;
    }
    try {
      const triggers = normalizeVoiceWakeTriggers(params.triggers);
      const cfg = await setVoiceWakeTriggers(triggers);
      context.broadcastVoiceWakeChanged(cfg.triggers);
      respond(true, { triggers: cfg.triggers });
    } catch (err) {
      respond(false, undefined, errorShape(ErrorCodes.DEPENDENCY_FAILED, formatForLog(err)));
    }
  },
};
