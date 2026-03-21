import { vi } from "vitest";
import { installChromeUserDataDirHooks } from "./chrome-user-data-dir.test-harness.js";

const chromeUserDataDir = { dir: "/tmp/deneb" };
installChromeUserDataDirHooks(chromeUserDataDir);

vi.mock("./chrome.js", () => ({
  isChromeCdpReady: vi.fn(async () => true),
  isChromeReachable: vi.fn(async () => true),
  launchDenebChrome: vi.fn(async () => {
    throw new Error("unexpected launch");
  }),
  resolveDenebUserDataDir: vi.fn(() => chromeUserDataDir.dir),
  stopDenebChrome: vi.fn(async () => {}),
}));
