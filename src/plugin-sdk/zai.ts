// Public Z.ai helpers for provider plugins that need endpoint detection.

export {
  clearZaiEndpointCache,
  detectZaiEndpoint,
  detectZaiEndpointDetailed,
  formatZaiDetectionFailures,
  type ZaiDetectedEndpoint,
  type ZaiDetectionResult,
  type ZaiEndpointId,
  type ZaiProbeFailure,
} from "../plugins/provider-zai-endpoint.js";
