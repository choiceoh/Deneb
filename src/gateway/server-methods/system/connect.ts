import { ErrorCodes, errorShape } from "../../protocol/index.js";
import type { GatewayRequestHandlers } from "../types.js";

export const connectHandlers: GatewayRequestHandlers = {
  connect: ({ respond }) => {
    respond(
      false,
      undefined,
      errorShape(ErrorCodes.VALIDATION_FAILED, "connect is only valid as the first request"),
    );
  },
};
