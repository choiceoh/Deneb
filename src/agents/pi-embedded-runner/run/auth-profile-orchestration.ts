import type { ThinkLevel } from "../../../auto-reply/thinking.js";
import type { DenebConfig } from "../../../config/config.js";
import { prepareProviderRuntimeAuth } from "../../../plugins/provider-runtime.js";
import {
  isProfileInCooldown,
  markAuthProfileFailure,
  resolveProfilesUnavailableReason,
  type AuthProfileFailureReason,
} from "../../auth-profiles.js";
import type { AuthProfileStore } from "../../auth-profiles/types.js";
import { FailoverError, resolveFailoverStatus } from "../../failover-error.js";
import { getApiKeyForModel } from "../../models/model-auth.js";
import { classifyFailoverReason, type FailoverReason } from "../../pi-embedded-helpers.js";
import { log } from "../logger.js";
import { type ApiKeyInfo } from "../run-usage.js";
import { describeUnknownError } from "../utils.js";
import type { RuntimeAuthManager } from "./runtime-auth.js";

export type AuthProfileOrchestrationDeps = {
  config: DenebConfig | undefined;
  provider: string;
  modelId: string;
  agentDir: string;
  workspaceDir: string;
  runId?: string;
  fallbackConfigured: boolean;
  authStore: AuthProfileStore;
  runtimeAuthManager: RuntimeAuthManager;
};

/**
 * Manages auth profile selection, rotation, cooldown probing,
 * and failure tracking across run loop iterations.
 */
export class AuthProfileOrchestrator {
  private deps: AuthProfileOrchestrationDeps;
  readonly profileCandidates: Array<string | undefined>;
  private lockedProfileId: string | undefined;
  private initialThinkLevel: ThinkLevel;
  profileIndex = 0;
  apiKeyInfo: ApiKeyInfo | null = null;
  lastProfileId: string | undefined;
  thinkLevel: ThinkLevel;
  attemptedThinking: Set<ThinkLevel>;

  constructor(
    deps: AuthProfileOrchestrationDeps,
    profileCandidates: Array<string | undefined>,
    lockedProfileId: string | undefined,
    initialThinkLevel: ThinkLevel,
  ) {
    this.deps = deps;
    this.profileCandidates = profileCandidates;
    this.lockedProfileId = lockedProfileId;
    this.initialThinkLevel = initialThinkLevel;
    this.thinkLevel = initialThinkLevel;
    this.attemptedThinking = new Set();
  }

  /**
   * Resolve API key and prepare runtime auth for a given profile candidate.
   */
  async applyApiKeyInfo(candidate?: string): Promise<void> {
    const runtimeModel = this.deps.runtimeAuthManager.getRuntimeModel();
    this.apiKeyInfo = await getApiKeyForModel({
      model: runtimeModel,
      cfg: this.deps.config,
      profileId: candidate,
      store: this.deps.authStore,
      agentDir: this.deps.agentDir,
    });
    const resolvedProfileId = this.apiKeyInfo.profileId ?? candidate;
    if (!this.apiKeyInfo.apiKey) {
      if (this.apiKeyInfo.mode !== "aws-sdk") {
        throw new Error(
          `No API key resolved for provider "${runtimeModel.provider}" (auth mode: ${this.apiKeyInfo.mode}).`,
        );
      }
      this.lastProfileId = resolvedProfileId;
      return;
    }
    let runtimeAuthHandled = false;
    const preparedAuth = await prepareProviderRuntimeAuth({
      provider: runtimeModel.provider,
      config: this.deps.config,
      workspaceDir: this.deps.workspaceDir,
      env: process.env,
      context: {
        config: this.deps.config,
        agentDir: this.deps.agentDir,
        workspaceDir: this.deps.workspaceDir,
        env: process.env,
        provider: runtimeModel.provider,
        modelId: this.deps.modelId,
        model: runtimeModel,
        apiKey: this.apiKeyInfo.apiKey,
        authMode: this.apiKeyInfo.mode,
        profileId: this.apiKeyInfo.profileId,
      },
    });
    const ram = this.deps.runtimeAuthManager;
    if (preparedAuth?.baseUrl) {
      const updatedRuntime = { ...ram.getRuntimeModel(), baseUrl: preparedAuth.baseUrl };
      const updatedEffective = { ...ram.getEffectiveModel(), baseUrl: preparedAuth.baseUrl };
      ram.updateModels(updatedRuntime, updatedEffective);
    }
    if (preparedAuth?.apiKey) {
      ram.initState({
        sourceApiKey: this.apiKeyInfo.apiKey,
        authMode: this.apiKeyInfo.mode,
        profileId: this.apiKeyInfo.profileId,
        expiresAt: preparedAuth.expiresAt,
        exchangedApiKey: preparedAuth.apiKey,
      });
      runtimeAuthHandled = true;
    }
    if (!runtimeAuthHandled) {
      ram.storeRawApiKey(this.apiKeyInfo.apiKey);
    }
    this.lastProfileId = this.apiKeyInfo.profileId;
  }

  /**
   * Try advancing to the next non-cooldowned auth profile candidate.
   * Returns true if a new profile was activated.
   */
  async advanceAuthProfile(): Promise<boolean> {
    if (this.lockedProfileId) {
      return false;
    }
    let nextIndex = this.profileIndex + 1;
    while (nextIndex < this.profileCandidates.length) {
      const candidate = this.profileCandidates[nextIndex];
      if (candidate && isProfileInCooldown(this.deps.authStore, candidate)) {
        nextIndex += 1;
        continue;
      }
      try {
        await this.applyApiKeyInfo(candidate);
        this.profileIndex = nextIndex;
        this.thinkLevel = this.initialThinkLevel;
        this.attemptedThinking.clear();
        return true;
      } catch (err) {
        if (candidate && candidate === this.lockedProfileId) {
          throw err;
        }
        nextIndex += 1;
      }
    }
    return false;
  }

  /**
   * Run the initial auth profile selection loop, including cooldown probing.
   */
  async selectInitialProfile(opts: { allowTransientCooldownProbe?: boolean }): Promise<void> {
    const autoProfileCandidates = this.profileCandidates.filter(
      (candidate): candidate is string =>
        typeof candidate === "string" && candidate.length > 0 && candidate !== this.lockedProfileId,
    );
    const allAutoProfilesInCooldown =
      autoProfileCandidates.length > 0 &&
      autoProfileCandidates.every((candidate) =>
        isProfileInCooldown(this.deps.authStore, candidate),
      );
    const unavailableReason = allAutoProfilesInCooldown
      ? (resolveProfilesUnavailableReason({
          store: this.deps.authStore,
          profileIds: autoProfileCandidates,
        }) ?? "unknown")
      : null;
    const allowTransientCooldownProbe =
      opts.allowTransientCooldownProbe === true &&
      allAutoProfilesInCooldown &&
      (unavailableReason === "rate_limit" ||
        unavailableReason === "overloaded" ||
        unavailableReason === "billing" ||
        unavailableReason === "unknown");
    let didTransientCooldownProbe = false;

    try {
      while (this.profileIndex < this.profileCandidates.length) {
        const candidate = this.profileCandidates[this.profileIndex];
        const inCooldown =
          candidate &&
          candidate !== this.lockedProfileId &&
          isProfileInCooldown(this.deps.authStore, candidate);
        if (inCooldown) {
          if (allowTransientCooldownProbe && !didTransientCooldownProbe) {
            didTransientCooldownProbe = true;
            log.warn(
              `probing cooldowned auth profile for ${this.deps.provider}/${this.deps.modelId} due to ${unavailableReason ?? "transient"} unavailability`,
            );
          } else {
            this.profileIndex += 1;
            continue;
          }
        }
        await this.applyApiKeyInfo(this.profileCandidates[this.profileIndex]);
        break;
      }
      if (this.profileIndex >= this.profileCandidates.length) {
        this.throwFailover({ allInCooldown: true });
      }
    } catch (err) {
      if (err instanceof FailoverError) {
        throw err;
      }
      if (this.profileCandidates[this.profileIndex] === this.lockedProfileId) {
        this.throwFailover({ allInCooldown: false, error: err });
      }
      const advanced = await this.advanceAuthProfile();
      if (!advanced) {
        this.throwFailover({ allInCooldown: false, error: err });
      }
    }
  }

  resolveFailoverReason(params: {
    allInCooldown: boolean;
    message: string;
    profileIds?: Array<string | undefined>;
  }): FailoverReason {
    if (params.allInCooldown) {
      const profileIds = (params.profileIds ?? this.profileCandidates).filter(
        (id): id is string => typeof id === "string" && id.length > 0,
      );
      return (
        resolveProfilesUnavailableReason({
          store: this.deps.authStore,
          profileIds,
        }) ?? "unknown"
      );
    }
    const classified = classifyFailoverReason(params.message);
    return classified ?? "auth";
  }

  throwFailover(params: { allInCooldown: boolean; message?: string; error?: unknown }): never {
    const fallbackMessage = `No available auth profile for ${this.deps.provider} (all in cooldown or unavailable).`;
    const message =
      params.message?.trim() ||
      (params.error ? describeUnknownError(params.error).trim() : "") ||
      fallbackMessage;
    const reason = this.resolveFailoverReason({
      allInCooldown: params.allInCooldown,
      message,
      profileIds: this.profileCandidates,
    });
    if (this.deps.fallbackConfigured) {
      throw new FailoverError(message, {
        reason,
        provider: this.deps.provider,
        model: this.deps.modelId,
        status: resolveFailoverStatus(reason),
        cause: params.error,
      });
    }
    if (params.error instanceof Error) {
      throw params.error;
    }
    throw new Error(message);
  }

  async markProfileFailure(failure: {
    profileId?: string;
    reason?: AuthProfileFailureReason | null;
  }): Promise<void> {
    const { profileId, reason } = failure;
    if (!profileId || !reason || reason === "timeout") {
      return;
    }
    await markAuthProfileFailure({
      store: this.deps.authStore,
      profileId,
      reason,
      cfg: this.deps.config,
      agentDir: this.deps.agentDir,
      runId: this.deps.runId,
    });
  }

  resolveProfileFailureReason(
    failoverReason: FailoverReason | null,
  ): AuthProfileFailureReason | null {
    // Timeouts are transport/model-path failures, not auth health signals.
    if (!failoverReason || failoverReason === "timeout") {
      return null;
    }
    return failoverReason;
  }
}
