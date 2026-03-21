import type { Command } from "commander";
import { theme } from "../../terminal/theme.js";

const VALID_PRIORITIES = new Set(["high", "medium", "low"]);

export function registerAutonomousCli(program: Command) {
  const auto = program
    .command("autonomous")
    .description("Manage the autonomous agent loop")
    .addHelpText(
      "after",
      () =>
        `\n${theme.muted("The autonomous loop runs LLM-driven decision cycles continuously.")}\n`,
    );

  registerStatusCommand(auto);
  registerGoalCommands(auto);
  registerCycleCommand(auto);
}

function safeDate(ts: unknown): string {
  if (typeof ts !== "number" || !Number.isFinite(ts) || ts <= 0) {
    return "never";
  }
  try {
    return new Date(ts).toLocaleString();
  } catch {
    return "invalid";
  }
}

function registerStatusCommand(parent: Command) {
  parent
    .command("status")
    .description("Show current autonomous system status")
    .action(async () => {
      try {
        const { loadAutonomousState } = await import("../../autonomous/state-store.js");
        const state = await loadAutonomousState();

        const activeGoals = state.goals.filter((g) => g.status === "active");
        const activePlans = state.plans.filter((p) => p.status === "active");
        const unprocessedObs = state.observations.filter((o) => !o.processed);

        console.log(theme.heading("Autonomous System Status"));
        console.log(`  Cycles run:          ${state.cycleCount}`);
        console.log(`  Last cycle:          ${safeDate(state.lastCycleAt)}`);
        console.log(
          `  Next cycle:          ${state.nextCycleAt ? safeDate(state.nextCycleAt) : "not scheduled"}`,
        );
        console.log(`  Active goals:        ${activeGoals.length}`);
        console.log(`  Active plans:        ${activePlans.length}`);
        console.log(`  Pending observations: ${unprocessedObs.length}`);
        console.log(`  Social entries:      ${state.socialContext.length}`);

        if (activeGoals.length > 0) {
          console.log(`\n${theme.heading("Active Goals:")}`);
          for (const goal of activeGoals) {
            const due = goal.dueAt ? ` (due: ${safeDate(goal.dueAt)})` : "";
            const progress = goal.progress ? ` — ${goal.progress}` : "";
            console.log(`  [${goal.priority}] ${goal.description}${due}${progress}`);
            console.log(`          id: ${goal.id}`);
          }
        }
      } catch (err) {
        console.error(
          `Failed to load autonomous state: ${err instanceof Error ? err.message : String(err)}`,
        );
        process.exitCode = 1;
      }
    });
}

function registerGoalCommands(parent: Command) {
  const goal = parent.command("goal").description("Manage autonomous goals");

  goal
    .command("list")
    .description("List all goals")
    .action(async () => {
      try {
        const { loadAutonomousState } = await import("../../autonomous/state-store.js");
        const state = await loadAutonomousState();

        if (state.goals.length === 0) {
          console.log("No goals.");
          return;
        }

        for (const g of state.goals) {
          const status = g.status === "active" ? theme.success(g.status) : theme.muted(g.status);
          const due = g.dueAt ? ` (due: ${safeDate(g.dueAt)})` : "";
          console.log(`  [${g.priority}] ${status} ${g.description}${due}`);
          if (g.progress) {
            console.log(`          progress: ${g.progress}`);
          }
          console.log(`          id: ${g.id}`);
        }
      } catch (err) {
        console.error(`Failed to load goals: ${err instanceof Error ? err.message : String(err)}`);
        process.exitCode = 1;
      }
    });

  goal
    .command("add <description>")
    .description("Add a new goal")
    .option("-p, --priority <priority>", "Goal priority (high/medium/low)", "medium")
    .option("-d, --due <date>", "Due date (ISO string)")
    .action(async (description: string, opts: { priority?: string; due?: string }) => {
      try {
        const trimmed = (description ?? "").trim();
        if (trimmed.length === 0) {
          console.error("Goal description cannot be empty.");
          process.exitCode = 1;
          return;
        }
        const rawPriority = (opts.priority ?? "medium").toLowerCase();
        const priority = VALID_PRIORITIES.has(rawPriority)
          ? (rawPriority as "high" | "medium" | "low")
          : "medium";

        let dueAt: number | undefined;
        if (opts.due) {
          const parsed = new Date(opts.due).getTime();
          if (!Number.isFinite(parsed)) {
            console.error(`Invalid date: ${opts.due}`);
            process.exitCode = 1;
            return;
          }
          dueAt = parsed;
        }

        const { loadAutonomousState, saveAutonomousState, addGoal } =
          await import("../../autonomous/state-store.js");
        const state = await loadAutonomousState();
        const goal = addGoal(state, trimmed, priority, dueAt);
        await saveAutonomousState(state);
        console.log(`Goal added: ${goal.id}`);
        console.log(`  ${goal.description} [${goal.priority}]`);
      } catch (err) {
        console.error(`Failed to add goal: ${err instanceof Error ? err.message : String(err)}`);
        process.exitCode = 1;
      }
    });

  goal
    .command("remove <goalId>")
    .description("Remove a goal by ID")
    .action(async (goalId: string) => {
      try {
        const trimmedId = (goalId ?? "").trim();
        if (trimmedId.length === 0) {
          console.error("Goal ID is required.");
          process.exitCode = 1;
          return;
        }
        const { loadAutonomousState, saveAutonomousState } =
          await import("../../autonomous/state-store.js");
        const state = await loadAutonomousState();
        const index = state.goals.findIndex((g) => g.id === trimmedId);
        if (index === -1) {
          console.error(`Goal not found: ${trimmedId}`);
          process.exitCode = 1;
          return;
        }
        const removed = state.goals.splice(index, 1)[0];
        await saveAutonomousState(state);
        console.log(`Removed goal: ${removed?.description ?? "(unknown)"}`);
      } catch (err) {
        console.error(`Failed to remove goal: ${err instanceof Error ? err.message : String(err)}`);
        process.exitCode = 1;
      }
    });
}

function registerCycleCommand(parent: Command) {
  parent
    .command("cycle")
    .description("Trigger an immediate autonomous cycle (via gateway)")
    .action(async () => {
      try {
        const { requestHeartbeatNow } = await import("../../infra/heartbeat-wake.js");
        requestHeartbeatNow({ reason: "autonomous:cycle-now" });
        console.log("Autonomous cycle triggered.");
      } catch (err) {
        console.error(
          `Failed to trigger cycle: ${err instanceof Error ? err.message : String(err)}`,
        );
        process.exitCode = 1;
      }
    });
}
