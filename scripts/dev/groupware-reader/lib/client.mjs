import crypto from "node:crypto";
import { ensureSession, loginAndSave, loadSession, saveSession } from "./session.mjs";

export function envConfig() {
  return {
    url: (process.env.DENEB_GROUPWARE_URL || "https://tsgw.topsolar.kr").replace(/\/$/, ""),
    user: (process.env.DENEB_GROUPWARE_USER || "").trim(),
    pass: process.env.DENEB_GROUPWARE_PASSWORD || "",
    company: (process.env.DENEB_GROUPWARE_COMPANY || "topsolar").trim(),
  };
}

function signHeaders(sess, pathname) {
  const tid = crypto.randomBytes(16).toString("hex");
  const ts = String(Math.floor(Date.now() / 1000));
  const msg = sess.token + tid + ts + pathname;
  const sign = crypto.createHmac("sha256", sess.hashKey).update(msg).digest("base64");
  return {
    Authorization: `Bearer ${sess.token}`,
    timestamp: ts,
    "transaction-id": tid,
    "wehago-sign": sign,
    "Content-Type": "application/json;charset=UTF-8",
    Accept: "application/json, application/octet-stream",
  };
}

async function doFetch(sess, pathname, body) {
  const res = await fetch(sess.url + pathname, {
    method: "POST",
    headers: signHeaders(sess, pathname),
    body: JSON.stringify(body ?? {}),
  });
  const ctype = (res.headers.get("content-type") || "").toLowerCase();
  if (ctype.includes("octet-stream") || ctype.includes("application/pdf") || ctype.startsWith("image/")) {
    const buf = Buffer.from(await res.arrayBuffer());
    return {
      status: res.status,
      binary: true,
      buffer: buf,
      contentType: ctype,
      disposition: res.headers.get("content-disposition") || "",
    };
  }
  const text = await res.text();
  let json;
  try {
    json = JSON.parse(text);
  } catch {
    json = { resultCode: res.status, resultMsg: text.slice(0, 300) };
  }
  return { status: res.status, binary: false, json };
}

function isAuthFail(out) {
  if (out.binary) return out.status === 401;
  const code = out.json?.resultCode;
  const msg = String(out.json?.resultMsg || "");
  return (
    out.status === 401 ||
    code === 136 ||
    code === 601 ||
    /인증|token|Wehago-sign|쿠키/i.test(msg)
  );
}

export async function apiPost(pathname, body, { refreshOnAuth = true } = {}) {
  const env = envConfig();
  if (!env.user || !env.pass) throw new Error("DENEB_GROUPWARE_USER/PASSWORD required");
  let sess = await ensureSession(env);

  let out = await doFetch(sess, pathname, body);
  if (out.binary) {
    // Callers expecting JSON must not use apiPost for downloads.
    throw new Error(`expected JSON from ${pathname}, got binary ${out.contentType}`);
  }
  if (isAuthFail(out) && refreshOnAuth) {
    sess = await loginAndSave(env);
    out = await doFetch(sess, pathname, body);
    if (out.binary) {
      throw new Error(`expected JSON from ${pathname}, got binary ${out.contentType}`);
    }
  }
  return out;
}

/** Signed POST that returns either JSON or a binary Buffer (ECM downloads). */
export async function apiPostMaybeBinary(pathname, body, { refreshOnAuth = true } = {}) {
  const env = envConfig();
  if (!env.user || !env.pass) throw new Error("DENEB_GROUPWARE_USER/PASSWORD required");
  let sess = await ensureSession(env);

  let out = await doFetch(sess, pathname, body);
  if (isAuthFail(out) && refreshOnAuth) {
    sess = await loginAndSave(env);
    out = await doFetch(sess, pathname, body);
  }
  return out;
}

export { loadSession, saveSession, ensureSession, loginAndSave };
