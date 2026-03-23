import { describe, expect, it } from "vitest";
import { findLegacyAgentCommandCallLines } from "../scripts/check-ingress-agent-owner-context.mjs";
import { findMessagingTmpdirCallLines } from "../scripts/check-no-random-messaging-tmp.mjs";
import { findRawFetchCallLines } from "../scripts/check-no-raw-channel-fetch.mjs";
import { findRawWindowOpenLines } from "../scripts/check-no-raw-window-open.mjs";
import { findDeprecatedRegisterHttpHandlerLines } from "../scripts/check-no-register-http-handler.mjs";
import { findBlockedWebhookBodyReadLines } from "../scripts/check-webhook-auth-body-order.mjs";

describe("findMessagingTmpdirCallLines", () => {
  it("detects os.tmpdir() via namespace import", () => {
    const code = `
      import os from "node:os";
      const dir = os.tmpdir();
    `;
    const lines = findMessagingTmpdirCallLines(code);

    expect(lines).toHaveLength(1);
  });

  it("detects os.tmpdir() via star import", () => {
    const code = `
      import * as os from "node:os";
      const dir = os.tmpdir();
    `;
    const lines = findMessagingTmpdirCallLines(code);

    expect(lines).toHaveLength(1);
  });

  it("detects named tmpdir import", () => {
    const code = `
      import { tmpdir } from "node:os";
      const dir = tmpdir();
    `;
    const lines = findMessagingTmpdirCallLines(code);

    expect(lines).toHaveLength(1);
  });

  it("detects renamed tmpdir import", () => {
    const code = `
      import { tmpdir as getTmp } from "node:os";
      const dir = getTmp();
    `;
    const lines = findMessagingTmpdirCallLines(code);

    expect(lines).toHaveLength(1);
  });

  it("passes code without os.tmpdir usage", () => {
    const code = `
      import path from "node:path";
      const dir = path.resolve("/tmp");
    `;
    const lines = findMessagingTmpdirCallLines(code);

    expect(lines).toEqual([]);
  });

  it("passes tmpdir-like call without os import", () => {
    const code = `
      function tmpdir() { return "/tmp"; }
      const dir = tmpdir();
    `;
    const lines = findMessagingTmpdirCallLines(code);

    expect(lines).toEqual([]);
  });
});

describe("findRawFetchCallLines", () => {
  it("detects bare fetch() call", () => {
    const code = `const res = await fetch("https://api.example.com");`;
    const lines = findRawFetchCallLines(code);

    expect(lines).toHaveLength(1);
  });

  it("detects globalThis.fetch() call", () => {
    const code = `const res = await globalThis.fetch("https://api.example.com");`;
    const lines = findRawFetchCallLines(code);

    expect(lines).toHaveLength(1);
  });

  it("passes non-fetch calls", () => {
    const code = `const res = await fetchWithSsrFGuard("https://api.example.com");`;
    const lines = findRawFetchCallLines(code);

    expect(lines).toEqual([]);
  });

  it("passes method call named fetch on non-global object", () => {
    const code = `const res = await client.fetch("https://api.example.com");`;
    const lines = findRawFetchCallLines(code);

    expect(lines).toEqual([]);
  });
});

describe("findDeprecatedRegisterHttpHandlerLines", () => {
  it("detects registerHttpHandler call", () => {
    const code = `server.registerHttpHandler("/webhook", handler);`;
    const lines = findDeprecatedRegisterHttpHandlerLines(code);

    expect(lines).toHaveLength(1);
  });

  it("passes registerHttpRoute call", () => {
    const code = `server.registerHttpRoute({ path: "/webhook", handler });`;
    const lines = findDeprecatedRegisterHttpHandlerLines(code);

    expect(lines).toEqual([]);
  });

  it("passes unrelated method calls", () => {
    const code = `server.registerPlugin(plugin);`;
    const lines = findDeprecatedRegisterHttpHandlerLines(code);

    expect(lines).toEqual([]);
  });
});

describe("findBlockedWebhookBodyReadLines", () => {
  it("detects readJsonBodyWithLimit call", () => {
    const code = `const body = await readJsonBodyWithLimit(req);`;
    const lines = findBlockedWebhookBodyReadLines(code);

    expect(lines).toHaveLength(1);
  });

  it("detects readRequestBodyWithLimit call", () => {
    const code = `const body = await readRequestBodyWithLimit(req);`;
    const lines = findBlockedWebhookBodyReadLines(code);

    expect(lines).toHaveLength(1);
  });

  it("passes safe webhook body read functions", () => {
    const code = `const body = await readJsonWebhookBodyOrReject(req);`;
    const lines = findBlockedWebhookBodyReadLines(code);

    expect(lines).toEqual([]);
  });

  it("detects method access form", () => {
    const code = `const body = await helpers.readJsonBodyWithLimit(req);`;
    const lines = findBlockedWebhookBodyReadLines(code);

    expect(lines).toHaveLength(1);
  });
});

describe("findLegacyAgentCommandCallLines", () => {
  it("detects agentCommand() call", () => {
    const code = `await agentCommand({ action: "run" });`;
    const lines = findLegacyAgentCommandCallLines(code);

    expect(lines).toHaveLength(1);
  });

  it("passes agentCommandFromIngress call", () => {
    const code = `await agentCommandFromIngress({ action: "run", senderIsOwner: true });`;
    const lines = findLegacyAgentCommandCallLines(code);

    expect(lines).toEqual([]);
  });

  it("passes unrelated function calls", () => {
    const code = `await runCommand({ action: "test" });`;
    const lines = findLegacyAgentCommandCallLines(code);

    expect(lines).toEqual([]);
  });
});

describe("findRawWindowOpenLines", () => {
  it("detects window.open() call", () => {
    const code = `window.open("https://example.com");`;
    const lines = findRawWindowOpenLines(code);

    expect(lines).toHaveLength(1);
  });

  it("detects globalThis.open() call", () => {
    const code = `globalThis.open("https://example.com");`;
    const lines = findRawWindowOpenLines(code);

    expect(lines).toHaveLength(1);
  });

  it("passes safe helper call", () => {
    const code = `openExternalUrlSafe("https://example.com");`;
    const lines = findRawWindowOpenLines(code);

    expect(lines).toEqual([]);
  });

  it("passes open on non-window object", () => {
    const code = `dialog.open("settings");`;
    const lines = findRawWindowOpenLines(code);

    expect(lines).toEqual([]);
  });

  it("detects multiple window.open calls", () => {
    const code = `
      window.open("https://a.com");
      window.open("https://b.com");
    `;
    const lines = findRawWindowOpenLines(code);

    expect(lines).toHaveLength(2);
  });
});
