// Stub: method scopes removed for solo-dev simplification.
export const ADMIN_SCOPE = "operator.admin" as const;
export const READ_SCOPE = "operator.read" as const;
export const WRITE_SCOPE = "operator.write" as const;
export const APPROVALS_SCOPE = "operator.approvals" as const;
export const PAIRING_SCOPE = "operator.pairing" as const;

export type OperatorScope =
  | typeof ADMIN_SCOPE
  | typeof READ_SCOPE
  | typeof WRITE_SCOPE
  | typeof APPROVALS_SCOPE
  | typeof PAIRING_SCOPE;

export const CLI_DEFAULT_OPERATOR_SCOPES: OperatorScope[] = [
  ADMIN_SCOPE,
  READ_SCOPE,
  WRITE_SCOPE,
  APPROVALS_SCOPE,
  PAIRING_SCOPE,
];

export function isNodeRoleMethod(_method: string): boolean {
  return false;
}

export function resolveRequiredOperatorScopeForMethod(_method: string): OperatorScope | undefined {
  return undefined;
}

export function resolveLeastPrivilegeOperatorScopesForMethod(_method: string): OperatorScope[] {
  return [];
}

export function authorizeOperatorScopesForMethod(
  _method: string,
  _scopes: readonly string[],
): { allowed: true } | { allowed: false; missingScope: OperatorScope } {
  return { allowed: true };
}

export function isGatewayMethodClassified(_method: string): boolean {
  return true;
}
