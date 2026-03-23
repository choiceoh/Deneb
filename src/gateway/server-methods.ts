import { withPluginRuntimeGatewayRequestScope } from "../plugins/runtime/gateway-request-scope.js";
import { parseGatewayRole } from "./auth/role-policy.js";
import { ErrorCodes, errorShape } from "./protocol/index.js";
import { cronHandlers } from "./server-methods/admin/cron.js";
import { wizardHandlers } from "./server-methods/admin/wizard.js";
import { agentHandlers } from "./server-methods/agents/agent.js";
import { agentsHandlers } from "./server-methods/agents/agents.js";
import { channelsHandlers } from "./server-methods/channels/channels.js";
import { chatHandlers } from "./server-methods/chat/chat.js";
import { configHandlers } from "./server-methods/config/config.js";
import { browserHandlers } from "./server-methods/media/browser.js";
import { ttsHandlers } from "./server-methods/media/tts.js";
import { voicewakeHandlers } from "./server-methods/media/voicewake.js";
import { webHandlers } from "./server-methods/media/web.js";
import { sendHandlers } from "./server-methods/messaging/send.js";
import { talkHandlers } from "./server-methods/messaging/talk.js";
import { nodeHandlers } from "./server-methods/nodes/nodes.js";
import { sessionsHandlers } from "./server-methods/sessions/sessions.js";
import { skillsHandlers } from "./server-methods/skills/skills.js";
import { toolsCatalogHandlers } from "./server-methods/skills/tools-catalog.js";
import { autoMaintenanceHandlers } from "./server-methods/system/auto-maintenance.js";
import { connectHandlers } from "./server-methods/system/connect.js";
import { doctorHandlers } from "./server-methods/system/doctor.js";
import { healthHandlers } from "./server-methods/system/health.js";
import { logsHandlers } from "./server-methods/system/logs.js";
import { modelsHandlers } from "./server-methods/system/models.js";
import { systemHandlers } from "./server-methods/system/system.js";
import { updateHandlers } from "./server-methods/system/update.js";
import type { GatewayRequestHandlers, GatewayRequestOptions } from "./server-methods/types.js";

function authorizeGatewayMethod(method: string, client: GatewayRequestOptions["client"]) {
  if (!client?.connect) {
    return null;
  }
  if (method === "health") {
    return null;
  }
  const roleRaw = client.connect.role ?? "operator";
  const role = parseGatewayRole(roleRaw);
  if (!role) {
    return errorShape(ErrorCodes.UNAUTHORIZED, `unauthorized role: ${roleRaw}`);
  }
  return null;
}

export const coreGatewayHandlers: GatewayRequestHandlers = {
  ...connectHandlers,
  ...logsHandlers,
  ...voicewakeHandlers,
  ...healthHandlers,
  ...channelsHandlers,
  ...chatHandlers,
  ...cronHandlers,
  ...doctorHandlers,
  ...webHandlers,
  ...modelsHandlers,
  ...configHandlers,
  ...wizardHandlers,
  ...talkHandlers,
  ...toolsCatalogHandlers,
  ...ttsHandlers,
  ...skillsHandlers,
  ...sessionsHandlers,
  ...systemHandlers,
  ...updateHandlers,
  ...nodeHandlers,
  ...sendHandlers,
  ...agentHandlers,
  ...agentsHandlers,
  ...autoMaintenanceHandlers,
  ...browserHandlers,
};

export async function handleGatewayRequest(
  opts: GatewayRequestOptions & { extraHandlers?: GatewayRequestHandlers },
): Promise<void> {
  const { req, respond, client, isWebchatConnect, context } = opts;
  const authError = authorizeGatewayMethod(req.method, client);
  if (authError) {
    respond(false, undefined, authError);
    return;
  }
  const handler = opts.extraHandlers?.[req.method] ?? coreGatewayHandlers[req.method];
  if (!handler) {
    respond(false, undefined, errorShape(ErrorCodes.NOT_FOUND, `unknown method: ${req.method}`));
    return;
  }
  const invokeHandler = () =>
    handler({
      req,
      params: (req.params ?? {}) as Record<string, unknown>,
      client,
      isWebchatConnect,
      respond,
      context,
    });
  // All handlers run inside a request scope so that plugin runtime
  // subagent methods (e.g. context engine tools spawning sub-agents
  // during tool execution) can dispatch back into the gateway.
  await withPluginRuntimeGatewayRequestScope({ context, client, isWebchatConnect }, invokeHandler);
}
