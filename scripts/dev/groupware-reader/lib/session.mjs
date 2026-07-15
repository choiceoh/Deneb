/**
 * Login once via Playwright, cache auth_a_token + hash_key for signed HTTP.
 * Session file: ~/.deneb/groupware-session.json (mode 0600).
 */
import { chromium } from "playwright";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

const SESSION_PATH =
  process.env.DENEB_GROUPWARE_SESSION ||
  path.join(os.homedir(), ".deneb", "groupware-session.json");

export function sessionPath() {
  return SESSION_PATH;
}

export function loadSession() {
  try {
    const raw = fs.readFileSync(SESSION_PATH, "utf8");
    const s = JSON.parse(raw);
    if (!s?.token || !s?.hashKey || !s?.url) return null;
    return s;
  } catch {
    return null;
  }
}

export function saveSession(s) {
  const dir = path.dirname(SESSION_PATH);
  fs.mkdirSync(dir, { recursive: true, mode: 0o700 });
  fs.writeFileSync(SESSION_PATH, JSON.stringify(s, null, 2) + "\n", { mode: 0o600 });
}

export async function loginAndSave({ url, user, pass, company }) {
  const base = url.replace(/\/$/, "");
  const browser = await chromium.launch({
    headless: true,
    args: ["--disable-dev-shm-usage", "--no-sandbox"],
  });
  try {
    const context = await browser.newContext({
      locale: "ko-KR",
      viewport: { width: 1280, height: 800 },
    });
    const page = await context.newPage();
    await page.goto(`${base}/#/login`, { waitUntil: "domcontentloaded", timeout: 60_000 });
    await page.waitForTimeout(1000);

    const companyBox = page.getByPlaceholder("회사코드를");
    if (await companyBox.count()) {
      const disabled = await companyBox.isDisabled().catch(() => false);
      if (!disabled) await companyBox.fill(company || "topsolar");
    }

    await page.getByPlaceholder("아이디를").fill(user);
    const nextBtn = page.getByRole("button", { name: "다음" });
    if (await nextBtn.count()) {
      await nextBtn.click();
      await page.waitForTimeout(700);
    }
    await page.locator('input[type="password"]').first().fill(pass);
    const loginBtn = page.getByRole("button", { name: /^로그인$/ });
    if (await loginBtn.count()) await loginBtn.click();
    else await page.keyboard.press("Enter");

    await page.waitForFunction(
      () => !String(location.hash || "").includes("/login"),
      null,
      { timeout: 45_000 },
    );
    await page.waitForTimeout(1500);

    const sess = await page.evaluate(() => {
      const ui = JSON.parse(sessionStorage.getItem("userInfo") || "{}");
      const uc = ui.ucUserInfo || {};
      return {
        token: ui.auth_a_token || "",
        hashKey: ui.hash_key || "",
        empSeq: String(uc.empSeq || ""),
        compSeq: String(uc.compSeq || ""),
        deptSeq: String(uc.deptSeq || ""),
        groupSeq: String(uc.groupSeq || ""),
        empName: String(uc.empName || ""),
      };
    });
    if (!sess.token || !sess.hashKey) {
      throw new Error("login ok but auth_a_token/hash_key missing from sessionStorage");
    }
    const out = { ...sess, url: base, savedAt: Date.now() };
    saveSession(out);
    return out;
  } finally {
    await browser.close().catch(() => {});
  }
}

export async function ensureSession(env) {
  const existing = loadSession();
  // Reuse for up to 12h; 401 handler will force refresh.
  if (existing && Date.now() - (existing.savedAt || 0) < 12 * 3600 * 1000) {
    return existing;
  }
  return loginAndSave(env);
}
