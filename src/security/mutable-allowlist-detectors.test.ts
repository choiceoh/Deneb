import { describe, expect, it } from "vitest";
import {
  isDiscordMutableAllowEntry,
  isSlackMutableAllowEntry,
  isGoogleChatMutableAllowEntry,
  isMattermostMutableAllowEntry,
  isIrcMutableAllowEntry,
} from "./mutable-allowlist-detectors.js";

describe("isDiscordMutableAllowEntry", () => {
  it("returns false for numeric IDs (stable)", () => {
    expect(isDiscordMutableAllowEntry("123456789012345678")).toBe(false);
  });

  it("returns false for mention format <@id>", () => {
    expect(isDiscordMutableAllowEntry("<@123456789>")).toBe(false);
    expect(isDiscordMutableAllowEntry("<@!123456789>")).toBe(false);
  });

  it("returns false for wildcard", () => {
    expect(isDiscordMutableAllowEntry("*")).toBe(false);
  });

  it("returns false for empty/whitespace", () => {
    expect(isDiscordMutableAllowEntry("")).toBe(false);
    expect(isDiscordMutableAllowEntry("  ")).toBe(false);
  });

  it("returns true for usernames (mutable)", () => {
    expect(isDiscordMutableAllowEntry("username#1234")).toBe(true);
    expect(isDiscordMutableAllowEntry("SomeUser")).toBe(true);
  });

  it("returns false for prefixed IDs", () => {
    expect(isDiscordMutableAllowEntry("discord:123456789")).toBe(false);
    expect(isDiscordMutableAllowEntry("user:123456789")).toBe(false);
  });

  it("returns true for empty prefix content", () => {
    expect(isDiscordMutableAllowEntry("discord:")).toBe(true);
    expect(isDiscordMutableAllowEntry("user:  ")).toBe(true);
  });
});

describe("isSlackMutableAllowEntry", () => {
  it("returns false for Slack mention format <@ID>", () => {
    expect(isSlackMutableAllowEntry("<@U12345678>")).toBe(false);
  });

  it("returns false for Slack user IDs", () => {
    expect(isSlackMutableAllowEntry("U12345678")).toBe(false);
    expect(isSlackMutableAllowEntry("W12345678")).toBe(false);
  });

  it("returns false for prefixed Slack IDs", () => {
    expect(isSlackMutableAllowEntry("slack:U12345678")).toBe(false);
  });

  it("returns true for display names (mutable)", () => {
    expect(isSlackMutableAllowEntry("John Doe")).toBe(true);
  });

  it("returns false for wildcard", () => {
    expect(isSlackMutableAllowEntry("*")).toBe(false);
  });
});

describe("isGoogleChatMutableAllowEntry", () => {
  it("returns true for email addresses (mutable)", () => {
    expect(isGoogleChatMutableAllowEntry("user@domain.com")).toBe(true);
  });

  it("returns true for prefixed email", () => {
    expect(isGoogleChatMutableAllowEntry("googlechat:users/user@domain.com")).toBe(true);
  });

  it("returns false for numeric user IDs", () => {
    expect(isGoogleChatMutableAllowEntry("googlechat:users/123456")).toBe(false);
  });

  it("returns false for wildcard/empty", () => {
    expect(isGoogleChatMutableAllowEntry("*")).toBe(false);
    expect(isGoogleChatMutableAllowEntry("")).toBe(false);
  });
});

describe("isMattermostMutableAllowEntry", () => {
  it("returns false for 26-char alphanumeric IDs (stable)", () => {
    expect(isMattermostMutableAllowEntry("abcdefghijklmnopqrstuvwxyz")).toBe(false);
  });

  it("returns true for usernames", () => {
    expect(isMattermostMutableAllowEntry("john.doe")).toBe(true);
    expect(isMattermostMutableAllowEntry("@john.doe")).toBe(true);
  });

  it("returns false for prefixed stable IDs", () => {
    expect(isMattermostMutableAllowEntry("mattermost:abcdefghijklmnopqrstuvwxyz")).toBe(false);
  });

  it("returns false for wildcard/empty", () => {
    expect(isMattermostMutableAllowEntry("*")).toBe(false);
    expect(isMattermostMutableAllowEntry("")).toBe(false);
  });
});

describe("isIrcMutableAllowEntry", () => {
  it("returns false for hostmask with ! and @", () => {
    expect(isIrcMutableAllowEntry("nick!user@host.com")).toBe(false);
  });

  it("returns true for plain nicknames (mutable)", () => {
    expect(isIrcMutableAllowEntry("nickname")).toBe(true);
  });

  it("returns false for entries with @ only", () => {
    expect(isIrcMutableAllowEntry("user@host")).toBe(false);
  });

  it("returns false for wildcard/empty", () => {
    expect(isIrcMutableAllowEntry("*")).toBe(false);
    expect(isIrcMutableAllowEntry("")).toBe(false);
  });
});
