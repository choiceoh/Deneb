import { loadCoreRs } from "../../../bindings/core-rs.js";
import type { ErrorShape } from "./types.js";

export const ErrorCodes = {
  // --- 기존 코드 (하위 호환) ---
  NOT_LINKED: "NOT_LINKED",
  NOT_PAIRED: "NOT_PAIRED",
  AGENT_TIMEOUT: "AGENT_TIMEOUT",
  INVALID_REQUEST: "INVALID_REQUEST",
  UNAVAILABLE: "UNAVAILABLE",

  // --- INVALID_REQUEST 세분화 ---
  /** 필수 파라미터 누락 (nodeId, text, key 등) */
  MISSING_PARAM: "MISSING_PARAM",
  /** 요청한 리소스를 찾을 수 없음 (session, agent, node, approval 등) */
  NOT_FOUND: "NOT_FOUND",
  /** 인증 실패 또는 권한 부족 */
  UNAUTHORIZED: "UNAUTHORIZED",
  /** 파라미터 값이 유효하지 않거나 스키마 불일치 */
  VALIDATION_FAILED: "VALIDATION_FAILED",
  /** 리소스 상태 충돌 (이미 존재, ID 불일치, 중복 요청 등) */
  CONFLICT: "CONFLICT",
  /** 정책에 의해 차단됨 (send policy, 예약어, 삭제 불가 등) */
  FORBIDDEN: "FORBIDDEN",

  // --- UNAVAILABLE 세분화 ---
  /** 대상 노드가 연결되지 않음 */
  NODE_DISCONNECTED: "NODE_DISCONNECTED",
  /** 의존 서비스 장애 (TTS, auto-maintenance 등) */
  DEPENDENCY_FAILED: "DEPENDENCY_FAILED",
  /** 기능이 비활성화 상태 */
  FEATURE_DISABLED: "FEATURE_DISABLED",
} as const;

export type ErrorCode = (typeof ErrorCodes)[keyof typeof ErrorCodes];

/** 에러코드 → 원인 설명 매핑. 로그/디버깅에서 코드만 보고 원인을 빠르게 파악할 수 있도록. */
export const ErrorCodeCauses: Record<ErrorCode, string> = {
  NOT_LINKED: "게이트웨이에 연결된 노드가 없음. 노드가 등록되지 않았거나 해제됨",
  NOT_PAIRED: "디바이스가 페어링되지 않음. 페어링 승인이 필요하거나 만료됨",
  AGENT_TIMEOUT:
    "에이전트 응답 시간 초과. LLM 프로바이더 지연, 네트워크 문제, 또는 에이전트 크래시",
  INVALID_REQUEST: "요청이 유효하지 않음 (레거시 - 세분화 코드 사용 권장)",
  UNAVAILABLE: "서비스 접근 불가 (레거시 - 세분화 코드 사용 권장)",
  MISSING_PARAM: "필수 파라미터가 요청에 포함되지 않음",
  NOT_FOUND: "요청한 리소스(세션, 에이전트, 노드 등)가 존재하지 않거나 만료됨",
  UNAUTHORIZED: "인증 토큰 누락/불일치 또는 해당 작업에 대한 권한 부족",
  VALIDATION_FAILED: "파라미터 값이 허용 범위를 벗어남, 타입 불일치, 또는 스키마 위반",
  CONFLICT: "리소스 상태 충돌. 이미 존재하는 리소스 생성 시도, ID 불일치, 또는 중복 요청",
  FORBIDDEN: "보안 정책, 세션 정책, 또는 시스템 제약에 의해 작업이 차단됨",
  NODE_DISCONNECTED: "대상 노드의 WebSocket 연결이 끊어짐. 노드 재시작 또는 네트워크 문제",
  DEPENDENCY_FAILED: "TTS, 모델 카탈로그 등 의존 서비스가 응답하지 않음",
  FEATURE_DISABLED: "요청한 기능이 현재 설정에서 비활성화 상태",
};

export function errorShape(
  code: ErrorCode,
  message: string,
  opts?: { details?: unknown; retryable?: boolean; retryAfterMs?: number; cause?: string },
): ErrorShape {
  return {
    code,
    message,
    ...opts,
  };
}

/**
 * Validate that a string is a known gateway error code.
 */
export function isValidErrorCode(code: string): code is ErrorCode {
  return loadCoreRs().validateErrorCode(code);
}

/**
 * Check if an error code is retryable by default.
 */
export function isRetryableErrorCode(code: string): boolean {
  return loadCoreRs().isRetryableErrorCode(code);
}
