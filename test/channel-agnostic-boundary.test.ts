import { describe, expect, it } from "vitest";
import {
  findAcpUserFacingChannelNameViolations,
  findChannelAgnosticBoundaryViolations,
  findChannelCoreReverseDependencyViolations,
  findSystemMarkLiteralViolations,
} from "../scripts/check-channel-agnostic-boundaries.mjs";

describe("findChannelAgnosticBoundaryViolations", () => {
  it("detects static import of channel module", () => {
    const code = `import { foo } from "../telegram/client.ts";`;
    const violations = findChannelAgnosticBoundaryViolations(code);

    expect(violations).toHaveLength(1);
    expect(violations[0].reason).toMatch(/imports channel module/);
  });

  it("detects dynamic import of channel module", () => {
    const code = `const mod = await import("../discord/bot.ts");`;
    const violations = findChannelAgnosticBoundaryViolations(code);

    expect(violations).toHaveLength(1);
    expect(violations[0].reason).toMatch(/dynamically imports channel module/);
  });

  it("detects re-export of channel module", () => {
    const code = `export { handler } from "../slack/handler.ts";`;
    const violations = findChannelAgnosticBoundaryViolations(code);

    expect(violations).toHaveLength(1);
    expect(violations[0].reason).toMatch(/re-exports channel module/);
  });

  it("detects config path access via dot notation", () => {
    const code = `const cfg = config.channels.telegram;`;
    const violations = findChannelAgnosticBoundaryViolations(code);

    expect(violations).toHaveLength(1);
    expect(violations[0].reason).toMatch(/references config path "channels\.telegram"/);
  });

  it("detects config path access via bracket notation", () => {
    const code = `const cfg = config.channels["discord"];`;
    const violations = findChannelAgnosticBoundaryViolations(code);

    expect(violations).toHaveLength(1);
    expect(violations[0].reason).toMatch(/references config path/);
  });

  it("detects channel id comparison with ===", () => {
    const code = `if (channelId === "telegram") {}`;
    const violations = findChannelAgnosticBoundaryViolations(code);

    expect(violations).toHaveLength(1);
    expect(violations[0].reason).toMatch(/compares with channel id literal/);
  });

  it("detects channel id comparison with !==", () => {
    const code = `if (channelId !== "slack") {}`;
    const violations = findChannelAgnosticBoundaryViolations(code);

    expect(violations).toHaveLength(1);
    expect(violations[0].reason).toMatch(/compares with channel id literal/);
  });

  it("detects channel id assignment to channel property", () => {
    const code = `const obj = { channel: "discord" };`;
    const violations = findChannelAgnosticBoundaryViolations(code);

    expect(violations).toHaveLength(1);
    expect(violations[0].reason).toMatch(/assigns channel id literal to "channel"/);
  });

  it("passes clean code with no channel references", () => {
    const code = `
      import { foo } from "./utils.ts";
      const bar = config.general.name;
      if (id === "custom") {}
    `;
    const violations = findChannelAgnosticBoundaryViolations(code);

    expect(violations).toEqual([]);
  });

  it("respects checkModuleSpecifiers=false option", () => {
    const code = `import { foo } from "../telegram/client.ts";`;
    const violations = findChannelAgnosticBoundaryViolations(code, "source.ts", {
      checkModuleSpecifiers: false,
    });

    expect(violations).toEqual([]);
  });

  it("respects checkConfigPaths=false option", () => {
    const code = `const cfg = config.channels.telegram;`;
    const violations = findChannelAgnosticBoundaryViolations(code, "source.ts", {
      checkConfigPaths: false,
    });

    expect(violations).toEqual([]);
  });

  it("respects checkChannelComparisons=false option", () => {
    const code = `if (channelId === "telegram") {}`;
    const violations = findChannelAgnosticBoundaryViolations(code, "source.ts", {
      checkChannelComparisons: false,
    });

    expect(violations).toEqual([]);
  });

  it("respects checkChannelAssignments=false option", () => {
    const code = `const obj = { channel: "discord" };`;
    const violations = findChannelAgnosticBoundaryViolations(code, "source.ts", {
      checkChannelAssignments: false,
    });

    expect(violations).toEqual([]);
  });

  it("detects multiple violations in the same file", () => {
    const code = `
      import { foo } from "../telegram/client.ts";
      if (channelId === "discord") {}
      const obj = { channel: "slack" };
    `;
    const violations = findChannelAgnosticBoundaryViolations(code);

    expect(violations.length).toBeGreaterThanOrEqual(3);
  });
});

describe("findChannelCoreReverseDependencyViolations", () => {
  it("detects channel module import", () => {
    const code = `import { foo } from "../telegram/client.ts";`;
    const violations = findChannelCoreReverseDependencyViolations(code);

    expect(violations).toHaveLength(1);
    expect(violations[0].reason).toMatch(/imports channel module/);
  });

  it("ignores config path access", () => {
    const code = `const cfg = config.channels.telegram;`;
    const violations = findChannelCoreReverseDependencyViolations(code);

    expect(violations).toEqual([]);
  });

  it("ignores channel comparisons", () => {
    const code = `if (channelId === "telegram") {}`;
    const violations = findChannelCoreReverseDependencyViolations(code);

    expect(violations).toEqual([]);
  });
});

describe("findAcpUserFacingChannelNameViolations", () => {
  it("detects string literal referencing channel name", () => {
    const code = `const msg = "Connect your Discord server";`;
    const violations = findAcpUserFacingChannelNameViolations(code);

    expect(violations).toHaveLength(1);
    expect(violations[0].reason).toMatch(/user-facing text references channel name/);
  });

  it("ignores module specifier strings", () => {
    const code = `import { foo } from "../discord/handler.ts";`;
    const violations = findAcpUserFacingChannelNameViolations(code);

    expect(violations).toEqual([]);
  });

  it("detects case-insensitive channel name", () => {
    const code = `const label = "Telegram bot setup";`;
    const violations = findAcpUserFacingChannelNameViolations(code);

    expect(violations).toHaveLength(1);
  });

  it("passes clean code without channel names", () => {
    const code = `const msg = "Setup your messaging service";`;
    const violations = findAcpUserFacingChannelNameViolations(code);

    expect(violations).toEqual([]);
  });
});

describe("findSystemMarkLiteralViolations", () => {
  it("detects hardcoded system mark literal", () => {
    const code = `const mark = "\u2699\uFE0F system command";`;
    const violations = findSystemMarkLiteralViolations(code);

    expect(violations).toHaveLength(1);
    expect(violations[0].reason).toMatch(/hardcoded system mark literal/);
  });

  it("passes clean code without system mark", () => {
    const code = `const mark = "normal text";`;
    const violations = findSystemMarkLiteralViolations(code);

    expect(violations).toEqual([]);
  });

  it("ignores system mark in module specifier", () => {
    const code = `import { foo } from "./\u2699\uFE0F-utils.ts";`;
    const violations = findSystemMarkLiteralViolations(code);

    expect(violations).toEqual([]);
  });
});
