import type { GatewayRequestHandlers } from "../types.js";
import { sessionsLifecycleHandlers } from "./sessions-lifecycle.js";
import { sessionsMessagingHandlers } from "./sessions-messaging.js";
import { sessionsQueryHandlers } from "./sessions-query.js";

export const sessionsHandlers: GatewayRequestHandlers = {
  ...sessionsQueryHandlers,
  ...sessionsMessagingHandlers,
  ...sessionsLifecycleHandlers,
};
