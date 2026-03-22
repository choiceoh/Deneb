import type { Model, Api } from "@mariozechner/pi-ai";
import type { AuthStorage } from "@mariozechner/pi-coding-agent";
import type { DenebConfig } from "../../../config/config.js";
import { prepareProviderRuntimeAuth } from "../../../plugins/provider-runtime.js";
import { classifyFailoverReason, isFailoverErrorMessage } from "../../pi-embedded-helpers.js";
import { log } from "../logger.js";
import {
  RUNTIME_AUTH_REFRESH_MARGIN_MS,
  RUNTIME_AUTH_REFRESH_MIN_DELAY_MS,
  RUNTIME_AUTH_REFRESH_RETRY_MS,
  type RuntimeAuthState,
} from "../run-usage.js";
import { describeUnknownError } from "../utils.js";

export type RuntimeAuthDeps = {
  config: DenebConfig | undefined;
  workspaceDir: string;
  agentDir: string;
  modelId: string;
  authStorage: AuthStorage;
};

/**
 * Encapsulates runtime auth lifecycle: credential refresh, scheduling,
 * and auth-error recovery for provider-level token exchange flows.
 */
export class RuntimeAuthManager {
  private state: RuntimeAuthState | null = null;
  private cancelled = false;
  private runtimeModel: Model<Api>;
  private effectiveModel: Model<Api>;
  private deps: RuntimeAuthDeps;

  constructor(runtimeModel: Model<Api>, effectiveModel: Model<Api>, deps: RuntimeAuthDeps) {
    this.runtimeModel = runtimeModel;
    this.effectiveModel = effectiveModel;
    this.deps = deps;
  }

  get authState(): RuntimeAuthState | null {
    return this.state;
  }

  getRuntimeModel(): Model<Api> {
    return this.runtimeModel;
  }

  getEffectiveModel(): Model<Api> {
    return this.effectiveModel;
  }

  updateModels(runtimeModel: Model<Api>, effectiveModel: Model<Api>): void {
    this.runtimeModel = runtimeModel;
    this.effectiveModel = effectiveModel;
  }

  hasRefreshableAuth(): boolean {
    return Boolean(this.state?.sourceApiKey.trim());
  }

  /** Initialize runtime auth state after first API key application. */
  initState(init: {
    sourceApiKey: string;
    authMode: string;
    profileId?: string;
    expiresAt?: number;
    exchangedApiKey: string;
  }): void {
    this.deps.authStorage.setRuntimeApiKey(this.runtimeModel.provider, init.exchangedApiKey);
    this.state = {
      sourceApiKey: init.sourceApiKey,
      authMode: init.authMode,
      profileId: init.profileId,
      expiresAt: init.expiresAt,
    };
    if (init.expiresAt) {
      this.scheduleRefresh();
    }
  }

  /** Store a raw API key (no plugin-owned runtime auth exchange). */
  storeRawApiKey(apiKey: string): void {
    this.deps.authStorage.setRuntimeApiKey(this.runtimeModel.provider, apiKey);
    this.state = null;
  }

  stop(): void {
    this.cancelled = true;
    this.clearRefreshTimer();
  }

  private clearRefreshTimer(): void {
    if (!this.state?.refreshTimer) {
      return;
    }
    clearTimeout(this.state.refreshTimer);
    this.state.refreshTimer = undefined;
  }

  async refresh(reason: string): Promise<void> {
    if (!this.state) {
      return;
    }
    if (this.state.refreshInFlight) {
      await this.state.refreshInFlight;
      return;
    }
    this.state.refreshInFlight = (async () => {
      const sourceApiKey = this.state?.sourceApiKey.trim() ?? "";
      if (!sourceApiKey) {
        throw new Error("Runtime auth refresh requires a source credential.");
      }
      log.debug(`Refreshing runtime auth for ${this.runtimeModel.provider} (${reason})...`);
      const preparedAuth = await prepareProviderRuntimeAuth({
        provider: this.runtimeModel.provider,
        config: this.deps.config,
        workspaceDir: this.deps.workspaceDir,
        env: process.env,
        context: {
          config: this.deps.config,
          agentDir: this.deps.agentDir,
          workspaceDir: this.deps.workspaceDir,
          env: process.env,
          provider: this.runtimeModel.provider,
          modelId: this.deps.modelId,
          model: this.runtimeModel,
          apiKey: sourceApiKey,
          authMode: this.state?.authMode ?? "unknown",
          profileId: this.state?.profileId,
        },
      });
      if (!preparedAuth?.apiKey) {
        throw new Error(
          `Provider "${this.runtimeModel.provider}" does not support runtime auth refresh.`,
        );
      }
      this.deps.authStorage.setRuntimeApiKey(this.runtimeModel.provider, preparedAuth.apiKey);
      if (preparedAuth.baseUrl) {
        this.runtimeModel = { ...this.runtimeModel, baseUrl: preparedAuth.baseUrl };
        this.effectiveModel = { ...this.effectiveModel, baseUrl: preparedAuth.baseUrl };
      }
      this.state = {
        ...this.state!,
        expiresAt: preparedAuth.expiresAt,
      };
      if (preparedAuth.expiresAt) {
        const remaining = preparedAuth.expiresAt - Date.now();
        log.debug(
          `Runtime auth refreshed for ${this.runtimeModel.provider}; expires in ${Math.max(0, Math.floor(remaining / 1000))}s.`,
        );
      }
    })()
      .catch((err) => {
        log.warn(
          `Runtime auth refresh failed for ${this.runtimeModel.provider}: ${describeUnknownError(err)}`,
        );
        throw err;
      })
      .finally(() => {
        if (this.state) {
          this.state.refreshInFlight = undefined;
        }
      });
    await this.state.refreshInFlight;
  }

  scheduleRefresh(): void {
    if (!this.state || this.cancelled) {
      return;
    }
    if (!this.hasRefreshableAuth()) {
      log.warn(
        `Skipping runtime auth refresh scheduling for ${this.runtimeModel.provider}; source credential missing.`,
      );
      return;
    }
    if (!this.state.expiresAt) {
      return;
    }
    this.clearRefreshTimer();
    const now = Date.now();
    const refreshAt = this.state.expiresAt - RUNTIME_AUTH_REFRESH_MARGIN_MS;
    const delayMs = Math.max(RUNTIME_AUTH_REFRESH_MIN_DELAY_MS, refreshAt - now);
    const timer = setTimeout(() => {
      if (this.cancelled) {
        return;
      }
      this.refresh("scheduled")
        .then(() => this.scheduleRefresh())
        .catch(() => {
          if (this.cancelled) {
            return;
          }
          const retryTimer = setTimeout(() => {
            if (this.cancelled) {
              return;
            }
            this.refresh("scheduled-retry")
              .then(() => this.scheduleRefresh())
              .catch(() => undefined);
          }, RUNTIME_AUTH_REFRESH_RETRY_MS);
          if (this.state) {
            this.state.refreshTimer = retryTimer;
          }
          if (this.cancelled && this.state) {
            clearTimeout(retryTimer);
            this.state.refreshTimer = undefined;
          }
        });
    }, delayMs);
    this.state.refreshTimer = timer;
    if (this.cancelled) {
      clearTimeout(timer);
      this.state.refreshTimer = undefined;
    }
  }

  /**
   * Attempt to recover from an auth error by refreshing runtime auth.
   * Returns true if refresh succeeded and the caller should retry.
   */
  async maybeRefreshForAuthError(errorText: string, alreadyRetried: boolean): Promise<boolean> {
    if (!this.state || alreadyRetried) {
      return false;
    }
    if (!isFailoverErrorMessage(errorText)) {
      return false;
    }
    if (classifyFailoverReason(errorText) !== "auth") {
      return false;
    }
    try {
      await this.refresh("auth-error");
      this.scheduleRefresh();
      return true;
    } catch {
      return false;
    }
  }
}
