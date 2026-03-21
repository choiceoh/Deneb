import { describe, expect, it } from "vitest";
import { buildPlatformRuntimeLogHints, buildPlatformServiceStartHints } from "./runtime-hints.js";

describe("buildPlatformRuntimeLogHints", () => {
  it("renders launchd log hints on darwin", () => {
    expect(
      buildPlatformRuntimeLogHints({
        platform: "darwin",
        env: {
          DENEB_STATE_DIR: "/tmp/deneb-state",
          DENEB_LOG_PREFIX: "gateway",
        },
        systemdServiceName: "deneb-gateway",
        windowsTaskName: "Deneb Gateway",
      }),
    ).toEqual([
      "Launchd stdout (if installed): /tmp/deneb-state/logs/gateway.log",
      "Launchd stderr (if installed): /tmp/deneb-state/logs/gateway.err.log",
    ]);
  });

  it("renders systemd and windows hints by platform", () => {
    expect(
      buildPlatformRuntimeLogHints({
        platform: "linux",
        systemdServiceName: "deneb-gateway",
        windowsTaskName: "Deneb Gateway",
      }),
    ).toEqual(["Logs: journalctl --user -u deneb-gateway.service -n 200 --no-pager"]);
    expect(
      buildPlatformRuntimeLogHints({
        platform: "win32",
        systemdServiceName: "deneb-gateway",
        windowsTaskName: "Deneb Gateway",
      }),
    ).toEqual(['Logs: schtasks /Query /TN "Deneb Gateway" /V /FO LIST']);
  });
});

describe("buildPlatformServiceStartHints", () => {
  it("builds platform-specific service start hints", () => {
    expect(
      buildPlatformServiceStartHints({
        platform: "darwin",
        installCommand: "deneb gateway install",
        startCommand: "deneb gateway",
        launchAgentPlistPath: "~/Library/LaunchAgents/com.deneb.gateway.plist",
        systemdServiceName: "deneb-gateway",
        windowsTaskName: "Deneb Gateway",
      }),
    ).toEqual([
      "deneb gateway install",
      "deneb gateway",
      "launchctl bootstrap gui/$UID ~/Library/LaunchAgents/com.deneb.gateway.plist",
    ]);
    expect(
      buildPlatformServiceStartHints({
        platform: "linux",
        installCommand: "deneb gateway install",
        startCommand: "deneb gateway",
        launchAgentPlistPath: "~/Library/LaunchAgents/com.deneb.gateway.plist",
        systemdServiceName: "deneb-gateway",
        windowsTaskName: "Deneb Gateway",
      }),
    ).toEqual([
      "deneb gateway install",
      "deneb gateway",
      "systemctl --user start deneb-gateway.service",
    ]);
  });
});
