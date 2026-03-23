export {
  isAuthErrorMessage,
  isAuthPermanentErrorMessage,
  isBillingErrorMessage,
  isOverloadedErrorMessage,
  isRateLimitErrorMessage,
  isTimeoutErrorMessage,
} from "./failover-matches.js";

export {
  isContextOverflowError,
  isLikelyContextOverflowError,
  isCompactionFailureError,
  extractObservedOverflowTokenCount,
} from "./errors-context.js";

export {
  isCloudflareOrHtmlErrorPage,
  isTransientHttpError,
  classifyFailoverReasonFromHttpStatus,
  getApiErrorPayloadFingerprint,
  isRawApiErrorPayload,
  parseApiErrorInfo,
  classifyFailoverReason,
  isFailoverErrorMessage,
  isFailoverAssistantError,
  isModelNotFoundErrorMessage,
  isCloudCodeAssistFormatError,
  type ApiErrorInfo,
  type FailoverReason,
} from "./errors-api.js";

export {
  formatBillingErrorMessage,
  BILLING_ERROR_USER_MESSAGE,
  formatRawAssistantErrorForUi,
  formatAssistantErrorText,
  sanitizeUserFacingText,
} from "./errors-format.js";

export {
  isRateLimitAssistantError,
  isMissingToolCallInputError,
  isBillingAssistantError,
  isAuthAssistantError,
  parseImageDimensionError,
  isImageDimensionErrorMessage,
  parseImageSizeError,
  isImageSizeError,
} from "./errors-assistant.js";
