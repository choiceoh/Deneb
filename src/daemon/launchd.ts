/**
 * Stub: macOS launchd helpers (original module removed).
 */

export function resolveLaunchAgentPlistPath(_env?: NodeJS.ProcessEnv): string {
  return "";
}

export function isLaunchAgentListed(_label?: string): boolean {
  return false;
}

export function isLaunchAgentLoaded(_label?: string): boolean {
  return false;
}

export function launchAgentPlistExists(_label?: string): boolean {
  return false;
}

export async function repairLaunchAgentBootstrap(
  _options?: Record<string, unknown>,
): Promise<void> {
  // no-op stub
}
