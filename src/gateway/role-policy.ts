// Stub: role policy removed for solo-dev simplification.
export const GATEWAY_ROLES = ["operator", "node"] as const;

export type GatewayRole = (typeof GATEWAY_ROLES)[number];

export function parseGatewayRole(roleRaw: unknown): GatewayRole | null {
  if (roleRaw === "operator" || roleRaw === "node") {
    return roleRaw;
  }
  return null;
}

export function roleCanSkipDeviceIdentity(_role: GatewayRole, _sharedAuthOk: boolean): boolean {
  return true;
}

export function isRoleAuthorizedForMethod(_role: GatewayRole, _method: string): boolean {
  return true;
}
