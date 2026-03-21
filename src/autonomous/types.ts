// Re-export config type for convenience.
export type { AutonomousConfig } from "../config/types.autonomous.js";

// -- State types --

export type AutonomousState = {
  version: 1;
  goals: Goal[];
  observations: Observation[];
  plans: Plan[];
  socialContext: SocialEntry[];
  lastCycleAt: number;
  nextCycleAt: number;
  cycleCount: number;
};

export type GoalPriority = "high" | "medium" | "low";
export type GoalStatus = "active" | "paused" | "completed";

export type Goal = {
  id: string;
  description: string;
  priority: GoalPriority;
  status: GoalStatus;
  createdAt: number;
  progress?: string;
  dueAt?: number;
};

export type ObservationRelevance = "high" | "medium" | "low";

export type Observation = {
  id: string;
  source: string;
  content: string;
  observedAt: number;
  processed: boolean;
  relevance?: ObservationRelevance;
};

export type PlanStatus = "active" | "blocked" | "completed";

export type Plan = {
  id: string;
  goalId?: string;
  steps: string[];
  currentStep: number;
  status: PlanStatus;
};

export type SocialEntry = {
  id: string;
  channel: string;
  peerId: string;
  lastInteraction: number;
  context: string;
  followUpAt?: number;
};

// -- Attention types --

export type AttentionSignalType = "message" | "event" | "schedule" | "followup" | "goal-deadline";

export type AttentionSignal = {
  source: string;
  type: AttentionSignalType;
  content: string;
  urgency: number;
  timestamp: number;
};

// -- Cycle types --

export type CycleOutcome = {
  cycleNumber: number;
  startedAt: number;
  finishedAt: number;
  actionsTaken: string[];
  nextCycleRequestedMs?: number;
  error?: string;
};

// -- Store file type --

export type AutonomousStoreFile = {
  version: 1;
  state: AutonomousState;
};
