import {
  clickChromeMcpElement,
  closeChromeMcpTab,
  dragChromeMcpElement,
  evaluateChromeMcpScript,
  fillChromeMcpElement,
  fillChromeMcpForm,
  hoverChromeMcpElement,
  pressChromeMcpKey,
  resizeChromeMcpPage,
} from "../chrome-mcp.js";
import type { BrowserActRequest, BrowserFormField } from "../client-actions-core.js";
import { normalizeBrowserFormField } from "../form-fields.js";
import { getBrowserProfileCapabilities } from "../profile-capabilities.js";
import type { BrowserRouteContext } from "../server-context.js";
import { registerBrowserAgentActDownloadRoutes } from "./agent.act.download.js";
import { registerBrowserAgentActHookRoutes } from "./agent.act.hooks.js";
import {
  countBatchActions,
  MAX_BATCH_ACTIONS,
  normalizeBatchAction,
  SELECTOR_ALLOWED_KINDS,
  validateBatchTargetIds,
} from "./agent.act.normalize.js";
import {
  type ActKind,
  isActKind,
  parseClickButton,
  parseClickModifiers,
} from "./agent.act.shared.js";
import {
  browserEvaluateDisabledMessage,
  waitForExistingSessionCondition,
} from "./agent.act.waits.js";
import {
  readBody,
  requirePwAi,
  resolveTargetIdFromBody,
  withRouteTabContext,
  SELECTOR_UNSUPPORTED_MESSAGE,
} from "./agent.shared.js";
import type { BrowserRouteRegistrar } from "./types.js";
import { jsonError, toBoolean, toNumber, toStringArray, toStringOrEmpty } from "./utils.js";

export function registerBrowserAgentActRoutes(
  app: BrowserRouteRegistrar,
  ctx: BrowserRouteContext,
) {
  app.post("/act", async (req, res) => {
    const body = readBody(req);
    const kindRaw = toStringOrEmpty(body.kind);
    if (!isActKind(kindRaw)) {
      return jsonError(res, 400, "kind is required");
    }
    const kind: ActKind = kindRaw;
    const targetId = resolveTargetIdFromBody(body);
    if (Object.hasOwn(body, "selector") && !SELECTOR_ALLOWED_KINDS.has(kind)) {
      return jsonError(res, 400, SELECTOR_UNSUPPORTED_MESSAGE);
    }
    const earlyFn = kind === "wait" || kind === "evaluate" ? toStringOrEmpty(body.fn) : "";
    if (
      (kind === "evaluate" || (kind === "wait" && earlyFn)) &&
      !ctx.state().resolved.evaluateEnabled
    ) {
      return jsonError(
        res,
        403,
        browserEvaluateDisabledMessage(kind === "evaluate" ? "evaluate" : "wait"),
      );
    }

    await withRouteTabContext({
      req,
      res,
      ctx,
      targetId,
      run: async ({ profileCtx, cdpUrl, tab }) => {
        const evaluateEnabled = ctx.state().resolved.evaluateEnabled;
        const isExistingSession = getBrowserProfileCapabilities(profileCtx.profile).usesChromeMcp;
        const profileName = profileCtx.profile.name;

        switch (kind) {
          case "click": {
            const ref = toStringOrEmpty(body.ref) || undefined;
            const selector = toStringOrEmpty(body.selector) || undefined;
            if (!ref && !selector) {
              return jsonError(res, 400, "ref or selector is required");
            }
            const doubleClick = toBoolean(body.doubleClick) ?? false;
            const timeoutMs = toNumber(body.timeoutMs);
            const delayMs = toNumber(body.delayMs);
            const buttonRaw = toStringOrEmpty(body.button) || "";
            const button = buttonRaw ? parseClickButton(buttonRaw) : undefined;
            if (buttonRaw && !button) {
              return jsonError(res, 400, "button must be left|right|middle");
            }

            const modifiersRaw = toStringArray(body.modifiers) ?? [];
            const parsedModifiers = parseClickModifiers(modifiersRaw);
            if (parsedModifiers.error) {
              return jsonError(res, 400, parsedModifiers.error);
            }
            const modifiers = parsedModifiers.modifiers;
            if (isExistingSession) {
              if (selector) {
                return jsonError(
                  res,
                  501,
                  "existing-session click does not support selector targeting yet; use ref.",
                );
              }
              if ((button && button !== "left") || (modifiers && modifiers.length > 0)) {
                return jsonError(
                  res,
                  501,
                  "existing-session click currently supports left-click only (no button overrides/modifiers).",
                );
              }
              await clickChromeMcpElement({
                profileName,
                userDataDir: profileCtx.profile.userDataDir,
                targetId: tab.targetId,
                uid: ref!,
                doubleClick,
              });
              return res.json({ ok: true, targetId: tab.targetId, url: tab.url });
            }
            const pw = await requirePwAi(res, `act:${kind}`);
            if (!pw) {
              return;
            }
            const clickRequest: Parameters<typeof pw.clickViaPlaywright>[0] = {
              cdpUrl,
              targetId: tab.targetId,
              doubleClick,
            };
            if (ref) {
              clickRequest.ref = ref;
            }
            if (selector) {
              clickRequest.selector = selector;
            }
            if (button) {
              clickRequest.button = button;
            }
            if (modifiers) {
              clickRequest.modifiers = modifiers;
            }
            if (delayMs) {
              clickRequest.delayMs = delayMs;
            }
            if (timeoutMs) {
              clickRequest.timeoutMs = timeoutMs;
            }
            await pw.clickViaPlaywright(clickRequest);
            return res.json({ ok: true, targetId: tab.targetId, url: tab.url });
          }
          case "type": {
            const ref = toStringOrEmpty(body.ref) || undefined;
            const selector = toStringOrEmpty(body.selector) || undefined;
            if (!ref && !selector) {
              return jsonError(res, 400, "ref or selector is required");
            }
            if (typeof body.text !== "string") {
              return jsonError(res, 400, "text is required");
            }
            const text = body.text;
            const submit = toBoolean(body.submit) ?? false;
            const slowly = toBoolean(body.slowly) ?? false;
            const timeoutMs = toNumber(body.timeoutMs);
            if (isExistingSession) {
              if (selector) {
                return jsonError(
                  res,
                  501,
                  "existing-session type does not support selector targeting yet; use ref.",
                );
              }
              if (slowly) {
                return jsonError(
                  res,
                  501,
                  "existing-session type does not support slowly=true; use fill/press instead.",
                );
              }
              await fillChromeMcpElement({
                profileName,
                userDataDir: profileCtx.profile.userDataDir,
                targetId: tab.targetId,
                uid: ref!,
                value: text,
              });
              if (submit) {
                await pressChromeMcpKey({
                  profileName,
                  userDataDir: profileCtx.profile.userDataDir,
                  targetId: tab.targetId,
                  key: "Enter",
                });
              }
              return res.json({ ok: true, targetId: tab.targetId });
            }
            const pw = await requirePwAi(res, `act:${kind}`);
            if (!pw) {
              return;
            }
            const typeRequest: Parameters<typeof pw.typeViaPlaywright>[0] = {
              cdpUrl,
              targetId: tab.targetId,
              text,
              submit,
              slowly,
            };
            if (ref) {
              typeRequest.ref = ref;
            }
            if (selector) {
              typeRequest.selector = selector;
            }
            if (timeoutMs) {
              typeRequest.timeoutMs = timeoutMs;
            }
            await pw.typeViaPlaywright(typeRequest);
            return res.json({ ok: true, targetId: tab.targetId });
          }
          case "press": {
            const key = toStringOrEmpty(body.key);
            if (!key) {
              return jsonError(res, 400, "key is required");
            }
            const delayMs = toNumber(body.delayMs);
            if (isExistingSession) {
              if (delayMs) {
                return jsonError(res, 501, "existing-session press does not support delayMs.");
              }
              await pressChromeMcpKey({
                profileName,
                userDataDir: profileCtx.profile.userDataDir,
                targetId: tab.targetId,
                key,
              });
              return res.json({ ok: true, targetId: tab.targetId });
            }
            const pw = await requirePwAi(res, `act:${kind}`);
            if (!pw) {
              return;
            }
            await pw.pressKeyViaPlaywright({
              cdpUrl,
              targetId: tab.targetId,
              key,
              delayMs: delayMs ?? undefined,
            });
            return res.json({ ok: true, targetId: tab.targetId });
          }
          case "hover": {
            const ref = toStringOrEmpty(body.ref) || undefined;
            const selector = toStringOrEmpty(body.selector) || undefined;
            if (!ref && !selector) {
              return jsonError(res, 400, "ref or selector is required");
            }
            const timeoutMs = toNumber(body.timeoutMs);
            if (isExistingSession) {
              if (selector) {
                return jsonError(
                  res,
                  501,
                  "existing-session hover does not support selector targeting yet; use ref.",
                );
              }
              if (timeoutMs) {
                return jsonError(
                  res,
                  501,
                  "existing-session hover does not support timeoutMs overrides.",
                );
              }
              await hoverChromeMcpElement({
                profileName,
                userDataDir: profileCtx.profile.userDataDir,
                targetId: tab.targetId,
                uid: ref!,
              });
              return res.json({ ok: true, targetId: tab.targetId });
            }
            const pw = await requirePwAi(res, `act:${kind}`);
            if (!pw) {
              return;
            }
            await pw.hoverViaPlaywright({
              cdpUrl,
              targetId: tab.targetId,
              ref,
              selector,
              timeoutMs: timeoutMs ?? undefined,
            });
            return res.json({ ok: true, targetId: tab.targetId });
          }
          case "scrollIntoView": {
            const ref = toStringOrEmpty(body.ref) || undefined;
            const selector = toStringOrEmpty(body.selector) || undefined;
            if (!ref && !selector) {
              return jsonError(res, 400, "ref or selector is required");
            }
            const timeoutMs = toNumber(body.timeoutMs);
            if (isExistingSession) {
              if (selector) {
                return jsonError(
                  res,
                  501,
                  "existing-session scrollIntoView does not support selector targeting yet; use ref.",
                );
              }
              if (timeoutMs) {
                return jsonError(
                  res,
                  501,
                  "existing-session scrollIntoView does not support timeoutMs overrides.",
                );
              }
              await evaluateChromeMcpScript({
                profileName,
                userDataDir: profileCtx.profile.userDataDir,
                targetId: tab.targetId,
                fn: `(el) => { el.scrollIntoView({ block: "center", inline: "center" }); return true; }`,
                args: [ref!],
              });
              return res.json({ ok: true, targetId: tab.targetId });
            }
            const pw = await requirePwAi(res, `act:${kind}`);
            if (!pw) {
              return;
            }
            const scrollRequest: Parameters<typeof pw.scrollIntoViewViaPlaywright>[0] = {
              cdpUrl,
              targetId: tab.targetId,
            };
            if (ref) {
              scrollRequest.ref = ref;
            }
            if (selector) {
              scrollRequest.selector = selector;
            }
            if (timeoutMs) {
              scrollRequest.timeoutMs = timeoutMs;
            }
            await pw.scrollIntoViewViaPlaywright(scrollRequest);
            return res.json({ ok: true, targetId: tab.targetId });
          }
          case "drag": {
            const startRef = toStringOrEmpty(body.startRef) || undefined;
            const startSelector = toStringOrEmpty(body.startSelector) || undefined;
            const endRef = toStringOrEmpty(body.endRef) || undefined;
            const endSelector = toStringOrEmpty(body.endSelector) || undefined;
            if (!startRef && !startSelector) {
              return jsonError(res, 400, "startRef or startSelector is required");
            }
            if (!endRef && !endSelector) {
              return jsonError(res, 400, "endRef or endSelector is required");
            }
            const timeoutMs = toNumber(body.timeoutMs);
            if (isExistingSession) {
              if (startSelector || endSelector) {
                return jsonError(
                  res,
                  501,
                  "existing-session drag does not support selector targeting yet; use startRef/endRef.",
                );
              }
              if (timeoutMs) {
                return jsonError(
                  res,
                  501,
                  "existing-session drag does not support timeoutMs overrides.",
                );
              }
              await dragChromeMcpElement({
                profileName,
                userDataDir: profileCtx.profile.userDataDir,
                targetId: tab.targetId,
                fromUid: startRef!,
                toUid: endRef!,
              });
              return res.json({ ok: true, targetId: tab.targetId });
            }
            const pw = await requirePwAi(res, `act:${kind}`);
            if (!pw) {
              return;
            }
            await pw.dragViaPlaywright({
              cdpUrl,
              targetId: tab.targetId,
              startRef,
              startSelector,
              endRef,
              endSelector,
              timeoutMs: timeoutMs ?? undefined,
            });
            return res.json({ ok: true, targetId: tab.targetId });
          }
          case "select": {
            const ref = toStringOrEmpty(body.ref) || undefined;
            const selector = toStringOrEmpty(body.selector) || undefined;
            const values = toStringArray(body.values);
            if ((!ref && !selector) || !values?.length) {
              return jsonError(res, 400, "ref/selector and values are required");
            }
            const timeoutMs = toNumber(body.timeoutMs);
            if (isExistingSession) {
              if (selector) {
                return jsonError(
                  res,
                  501,
                  "existing-session select does not support selector targeting yet; use ref.",
                );
              }
              if (values.length !== 1) {
                return jsonError(
                  res,
                  501,
                  "existing-session select currently supports a single value only.",
                );
              }
              if (timeoutMs) {
                return jsonError(
                  res,
                  501,
                  "existing-session select does not support timeoutMs overrides.",
                );
              }
              await fillChromeMcpElement({
                profileName,
                userDataDir: profileCtx.profile.userDataDir,
                targetId: tab.targetId,
                uid: ref!,
                value: values[0] ?? "",
              });
              return res.json({ ok: true, targetId: tab.targetId });
            }
            const pw = await requirePwAi(res, `act:${kind}`);
            if (!pw) {
              return;
            }
            await pw.selectOptionViaPlaywright({
              cdpUrl,
              targetId: tab.targetId,
              ref,
              selector,
              values,
              timeoutMs: timeoutMs ?? undefined,
            });
            return res.json({ ok: true, targetId: tab.targetId });
          }
          case "fill": {
            const rawFields = Array.isArray(body.fields) ? body.fields : [];
            const fields = rawFields
              .map((field) => {
                if (!field || typeof field !== "object") {
                  return null;
                }
                return normalizeBrowserFormField(field as Record<string, unknown>);
              })
              .filter((field): field is BrowserFormField => field !== null);
            if (!fields.length) {
              return jsonError(res, 400, "fields are required");
            }
            const timeoutMs = toNumber(body.timeoutMs);
            if (isExistingSession) {
              if (timeoutMs) {
                return jsonError(
                  res,
                  501,
                  "existing-session fill does not support timeoutMs overrides.",
                );
              }
              await fillChromeMcpForm({
                profileName,
                userDataDir: profileCtx.profile.userDataDir,
                targetId: tab.targetId,
                elements: fields.map((field) => ({
                  uid: field.ref,
                  value: String(field.value ?? ""),
                })),
              });
              return res.json({ ok: true, targetId: tab.targetId });
            }
            const pw = await requirePwAi(res, `act:${kind}`);
            if (!pw) {
              return;
            }
            await pw.fillFormViaPlaywright({
              cdpUrl,
              targetId: tab.targetId,
              fields,
              timeoutMs: timeoutMs ?? undefined,
            });
            return res.json({ ok: true, targetId: tab.targetId });
          }
          case "resize": {
            const width = toNumber(body.width);
            const height = toNumber(body.height);
            if (!width || !height) {
              return jsonError(res, 400, "width and height are required");
            }
            if (isExistingSession) {
              await resizeChromeMcpPage({
                profileName,
                userDataDir: profileCtx.profile.userDataDir,
                targetId: tab.targetId,
                width,
                height,
              });
              return res.json({ ok: true, targetId: tab.targetId, url: tab.url });
            }
            const pw = await requirePwAi(res, `act:${kind}`);
            if (!pw) {
              return;
            }
            await pw.resizeViewportViaPlaywright({
              cdpUrl,
              targetId: tab.targetId,
              width,
              height,
            });
            return res.json({ ok: true, targetId: tab.targetId, url: tab.url });
          }
          case "wait": {
            const timeMs = toNumber(body.timeMs);
            const text = toStringOrEmpty(body.text) || undefined;
            const textGone = toStringOrEmpty(body.textGone) || undefined;
            const selector = toStringOrEmpty(body.selector) || undefined;
            const url = toStringOrEmpty(body.url) || undefined;
            const loadStateRaw = toStringOrEmpty(body.loadState);
            const loadState =
              loadStateRaw === "load" ||
              loadStateRaw === "domcontentloaded" ||
              loadStateRaw === "networkidle"
                ? loadStateRaw
                : undefined;
            const fn = toStringOrEmpty(body.fn) || undefined;
            const timeoutMs = toNumber(body.timeoutMs) ?? undefined;
            if (fn && !evaluateEnabled) {
              return jsonError(res, 403, browserEvaluateDisabledMessage("wait"));
            }
            if (
              timeMs === undefined &&
              !text &&
              !textGone &&
              !selector &&
              !url &&
              !loadState &&
              !fn
            ) {
              return jsonError(
                res,
                400,
                "wait requires at least one of: timeMs, text, textGone, selector, url, loadState, fn",
              );
            }
            if (isExistingSession) {
              if (loadState === "networkidle") {
                return jsonError(
                  res,
                  501,
                  "existing-session wait does not support loadState=networkidle yet.",
                );
              }
              await waitForExistingSessionCondition({
                profileName,
                userDataDir: profileCtx.profile.userDataDir ?? "",
                targetId: tab.targetId,
                timeMs,
                text,
                textGone,
                selector,
                url,
                loadState,
                fn,
                timeoutMs,
              });
              return res.json({ ok: true, targetId: tab.targetId });
            }
            const pw = await requirePwAi(res, `act:${kind}`);
            if (!pw) {
              return;
            }
            await pw.waitForViaPlaywright({
              cdpUrl,
              targetId: tab.targetId,
              timeMs,
              text,
              textGone,
              selector,
              url,
              loadState,
              fn,
              timeoutMs,
            });
            return res.json({ ok: true, targetId: tab.targetId });
          }
          case "evaluate": {
            if (!evaluateEnabled) {
              return jsonError(res, 403, browserEvaluateDisabledMessage("evaluate"));
            }
            const fn = toStringOrEmpty(body.fn);
            if (!fn) {
              return jsonError(res, 400, "fn is required");
            }
            const ref = toStringOrEmpty(body.ref) || undefined;
            const evalTimeoutMs = toNumber(body.timeoutMs);
            if (isExistingSession) {
              if (evalTimeoutMs !== undefined) {
                return jsonError(
                  res,
                  501,
                  "existing-session evaluate does not support timeoutMs overrides.",
                );
              }
              const result = await evaluateChromeMcpScript({
                profileName,
                userDataDir: profileCtx.profile.userDataDir,
                targetId: tab.targetId,
                fn,
                args: ref ? [ref] : undefined,
              });
              return res.json({
                ok: true,
                targetId: tab.targetId,
                url: tab.url,
                result,
              });
            }
            const pw = await requirePwAi(res, `act:${kind}`);
            if (!pw) {
              return;
            }
            const evalRequest: Parameters<typeof pw.evaluateViaPlaywright>[0] = {
              cdpUrl,
              targetId: tab.targetId,
              fn,
              ref,
              signal: req.signal,
            };
            if (evalTimeoutMs !== undefined) {
              evalRequest.timeoutMs = evalTimeoutMs;
            }
            const result = await pw.evaluateViaPlaywright(evalRequest);
            return res.json({
              ok: true,
              targetId: tab.targetId,
              url: tab.url,
              result,
            });
          }
          case "close": {
            if (isExistingSession) {
              await closeChromeMcpTab(profileName, tab.targetId, profileCtx.profile.userDataDir);
              return res.json({ ok: true, targetId: tab.targetId });
            }
            const pw = await requirePwAi(res, `act:${kind}`);
            if (!pw) {
              return;
            }
            await pw.closePageViaPlaywright({ cdpUrl, targetId: tab.targetId });
            return res.json({ ok: true, targetId: tab.targetId });
          }
          case "batch": {
            if (isExistingSession) {
              return jsonError(
                res,
                501,
                "existing-session batch is not supported yet; send actions individually.",
              );
            }
            const pw = await requirePwAi(res, `act:${kind}`);
            if (!pw) {
              return;
            }
            let actions: BrowserActRequest[];
            try {
              actions = Array.isArray(body.actions) ? body.actions.map(normalizeBatchAction) : [];
            } catch (err) {
              return jsonError(res, 400, err instanceof Error ? err.message : String(err));
            }
            if (!actions.length) {
              return jsonError(res, 400, "actions are required");
            }
            if (countBatchActions(actions) > MAX_BATCH_ACTIONS) {
              return jsonError(res, 400, `batch exceeds maximum of ${MAX_BATCH_ACTIONS} actions`);
            }
            const targetIdError = validateBatchTargetIds(actions, tab.targetId);
            if (targetIdError) {
              return jsonError(res, 403, targetIdError);
            }
            const stopOnError = toBoolean(body.stopOnError) ?? true;
            const result = await pw.batchViaPlaywright({
              cdpUrl,
              targetId: tab.targetId,
              actions,
              stopOnError,
              evaluateEnabled,
            });
            return res.json({ ok: true, targetId: tab.targetId, results: result.results });
          }
          default: {
            return jsonError(res, 400, "unsupported kind");
          }
        }
      },
    });
  });

  registerBrowserAgentActHookRoutes(app, ctx);
  registerBrowserAgentActDownloadRoutes(app, ctx);

  app.post("/response/body", async (req, res) => {
    const body = readBody(req);
    const targetId = resolveTargetIdFromBody(body);
    const url = toStringOrEmpty(body.url);
    const timeoutMs = toNumber(body.timeoutMs);
    const maxChars = toNumber(body.maxChars);
    if (!url) {
      return jsonError(res, 400, "url is required");
    }

    await withRouteTabContext({
      req,
      res,
      ctx,
      targetId,
      run: async ({ profileCtx, cdpUrl, tab }) => {
        if (getBrowserProfileCapabilities(profileCtx.profile).usesChromeMcp) {
          return jsonError(
            res,
            501,
            "response body is not supported for existing-session profiles yet.",
          );
        }
        const pw = await requirePwAi(res, "response body");
        if (!pw) {
          return;
        }
        const result = await pw.responseBodyViaPlaywright({
          cdpUrl,
          targetId: tab.targetId,
          url,
          timeoutMs: timeoutMs ?? undefined,
          maxChars: maxChars ?? undefined,
        });
        res.json({ ok: true, targetId: tab.targetId, response: result });
      },
    });
  });

  app.post("/highlight", async (req, res) => {
    const body = readBody(req);
    const targetId = resolveTargetIdFromBody(body);
    const ref = toStringOrEmpty(body.ref);
    if (!ref) {
      return jsonError(res, 400, "ref is required");
    }

    await withRouteTabContext({
      req,
      res,
      ctx,
      targetId,
      run: async ({ profileCtx, cdpUrl, tab }) => {
        if (getBrowserProfileCapabilities(profileCtx.profile).usesChromeMcp) {
          await evaluateChromeMcpScript({
            profileName: profileCtx.profile.name,
            userDataDir: profileCtx.profile.userDataDir,
            targetId: tab.targetId,
            args: [ref],
            fn: `(el) => {
              if (!(el instanceof Element)) {
                return false;
              }
              el.scrollIntoView({ block: "center", inline: "center" });
              const previousOutline = el.style.outline;
              const previousOffset = el.style.outlineOffset;
              el.style.outline = "3px solid #FF4500";
              el.style.outlineOffset = "2px";
              setTimeout(() => {
                el.style.outline = previousOutline;
                el.style.outlineOffset = previousOffset;
              }, 2000);
              return true;
            }`,
          });
          return res.json({ ok: true, targetId: tab.targetId });
        }
        const pw = await requirePwAi(res, "highlight");
        if (!pw) {
          return;
        }
        await pw.highlightViaPlaywright({
          cdpUrl,
          targetId: tab.targetId,
          ref,
        });
        res.json({ ok: true, targetId: tab.targetId });
      },
    });
  });
}
