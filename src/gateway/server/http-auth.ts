import type { IncomingMessage, ServerResponse } from "node:http";
import type { AuthRateLimiter } from "../auth/auth-rate-limit.js";
import type { ResolvedGatewayAuth } from "../auth/auth.js";
import { authorizeGatewayBearerRequestOrReply } from "../http/http-auth-helpers.js";

export async function enforcePluginRouteGatewayAuth(params: {
  req: IncomingMessage;
  res: ServerResponse;
  auth: ResolvedGatewayAuth;
  trustedProxies: string[];
  allowRealIpFallback: boolean;
  rateLimiter?: AuthRateLimiter;
}): Promise<boolean> {
  return await authorizeGatewayBearerRequestOrReply(params);
}
