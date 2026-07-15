#!/usr/bin/env node
/**
 * groupware-reader — Amaranth10 via signed internal APIs.
 *
 * Fast path: cached session (~/.deneb/groupware-session.json) + HMAC HTTP.
 * Login path: Playwright once when session missing/expired/401.
 *
 * Surfaces:
 *   area=approval  folder=pending|done|cc|total|all
 *   area=board
 *   area=sales     action=summary|list  folder=ytd|month|today|year|last_year
 *                  query=YYYYMMDD:YYYYMMDD optional explicit range
 *   action=act (approval only) — mutate; used by work-feed chips, not chat tool
 *
 * Env:
 *   DENEB_GROUPWARE_URL / USER / PASSWORD / COMPANY
 *   DENEB_GROUPWARE_SESSION  optional session file path
 *
 * Usage:
 *   node read.mjs --login-check
 *   node read.mjs --area approval --action list [--folder pending|done|cc|total|all]
 *   node read.mjs --area approval --action read --query '제목'
 *   node read.mjs --area approval --action attachment --doc-id 99178 --attachment '지출영수증'
 *   node read.mjs --area board --action list|read ...
 *   node read.mjs --area sales --action summary [--folder ytd|month|today]
 *   node read.mjs --area approval --action act --decision approve|reject --doc-id 99178
 */
import { stdin as input } from "node:process";
import { envConfig, loginAndSave, loadSession } from "./lib/client.mjs";
import {
  actApproval,
  listApproval,
  listBoard,
  normalizeFolder,
  readApproval,
  readApprovalAttachment,
  readBoard,
  summarySales,
} from "./lib/actions.mjs";

function argValue(flag) {
  const i = process.argv.indexOf(flag);
  return i >= 0 ? process.argv[i + 1] || "" : "";
}

const loginCheck = process.argv.includes("--login-check");
const area = (argValue("--area") || "approval").trim().toLowerCase();
const action = (argValue("--action") || (loginCheck ? "login-check" : "read")).trim().toLowerCase();
const query = (argValue("--query") || "").trim();
const source = argValue("--source");
const decision = (argValue("--decision") || "").trim();
const docId = (argValue("--doc-id") || argValue("--docId") || "").trim();
const attachment = (argValue("--attachment") || "").trim();
const comment = argValue("--comment");
const limitRaw = parseInt(argValue("--limit") || "20", 10);
const limit = Number.isFinite(limitRaw) && limitRaw > 0 ? Math.min(limitRaw, 50) : 20;
const folder = normalizeFolder(
  argValue("--folder") || (action === "list" ? "all" : "pending"),
);

async function readStdin() {
  if (input.isTTY) return "";
  const chunks = [];
  for await (const c of input) chunks.push(c);
  return Buffer.concat(chunks).toString("utf8").trim();
}

function die(msg, code = 1) {
  console.error(msg);
  process.exit(code);
}

async function main() {
  const env = envConfig();
  if (!env.user || !env.pass) {
    die("DENEB_GROUPWARE_USER / DENEB_GROUPWARE_PASSWORD required on srv4");
  }

  if (loginCheck || action === "login-check") {
    const s = await loginAndSave(env);
    console.log(
      `login ok user=${s.empName || env.user} empSeq=${s.empSeq} session=fresh`,
    );
    return;
  }

  if (area === "sales" && (action === "read" || action === "")) action = "summary";
  if (!["approval", "board", "sales"].includes(area)) die(`unknown --area ${area}`);
  if (area === "sales") {
    if (!["summary", "list"].includes(action)) die(`sales action must be summary|list (got ${action})`);
  } else if (!["list", "read", "attachment", "act"].includes(action)) {
    die(`unknown --action ${action}`);
  }
  if (["attachment", "act"].includes(action) && area !== "approval") {
    die(`${action} is only valid for --area approval`);
  }
  if (area === "approval" && !["attachment", "act"].includes(action) && !["pending", "done", "cc", "total", "all"].includes(folder)) {
    die(`unknown --folder ${folder}`);
  }

  const t0 = Date.now();
  let out;
  if (area === "sales") {
    out = await summarySales(folder === "all" || folder === "pending" ? "ytd" : folder, query);
  } else if (action === "act") {
    out = await actApproval(docId || query, decision, comment);
  } else if (action === "attachment") {
    out = await readApprovalAttachment(docId, attachment || query);
  } else if (area === "board") {
    out = action === "list" ? await listBoard(limit) : await readBoard(query);
  } else if (action === "list") {
    out = await listApproval(folder, limit);
  } else {
    const noti = await readStdin();
    out = await readApproval(folder, query, noti);
  }
  if (source) out = `출처: ${source}\n` + out;
  console.log(out);
  console.error(`ok ${Date.now() - t0}ms session=${loadSession() ? "cached" : "none"}`);
}

main().catch((err) => die(String(err?.stack || err)));
