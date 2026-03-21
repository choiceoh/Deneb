import type { Command } from "commander";
import { theme } from "../../terminal/theme.js";

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

function registerStatusCommand(parent: Command) {
  parent
    .command("status")
    .description("Show current autonomous system status")
    .action(async () => {
      const { loadAutonomousState } = await import("../../autonomous/state-store.js");
      const state = await loadAutonomousState();

      const activeGoals = state.goals.filter((g) => g.status === "active");
      const activePlans = state.plans.filter((p) => p.status === "active");
      const unprocessedObs = state.observations.filter((o) => !o.processed);

      console.log(theme.heading("Autonomous System Status"));
      console.log(`  Cycles run:          ${state.cycleCount}`);
      console.log(
        `  Last cycle:          ${state.lastCycleAt ? new Date(state.lastCycleAt).toLocaleString() : "never"}`,
      );
      console.log(
        `  Next cycle:          ${state.nextCycleAt ? new Date(state.nextCycleAt).toLocaleString() : "not scheduled"}`,
      );
      console.log(`  Active goals:        ${activeGoals.length}`);
      console.log(`  Active plans:        ${activePlans.length}`);
      console.log(`  Pending observations: ${unprocessedObs.length}`);
      console.log(`  Social entries:      ${state.socialContext.length}`);

      if (activeGoals.length > 0) {
        console.log(`\n${theme.heading("Active Goals:")}`);
        for (const goal of activeGoals) {
          const due = goal.dueAt ? ` (due: ${new Date(goal.dueAt).toLocaleString()})` : "";
          const progress = goal.progress ? ` — ${goal.progress}` : "";
          console.log(`  [${goal.priority}] ${goal.description}${due}${progress}`);
          console.log(`          id: ${goal.id}`);
        }
      }
    });
}

function registerGoalCommands(parent: Command) {
  const goal = parent.command("goal").description("Manage autonomous goals");

  goal
    .command("list")
    .description("List all goals")
    .action(async () => {
      const { loadAutonomousState } = await import("../../autonomous/state-store.js");
      const state = await loadAutonomousState();

      if (state.goals.length === 0) {
        console.log("No goals.");
        return;
      }

      for (const g of state.goals) {
        const status = g.status === "active" ? theme.success(g.status) : theme.muted(g.status);
        const due = g.dueAt ? ` (due: ${new Date(g.dueAt).toLocaleString()})` : "";
        console.log(`  [${g.priority}] ${status} ${g.description}${due}`);
        if (g.progress) {
          console.log(`          progress: ${g.progress}`);
        }
        console.log(`          id: ${g.id}`);
      }
    });

  goal
    .command("add <description>")
    .description("Add a new goal")
    .option("-p, --priority <priority>", "Goal priority (high/medium/low)", "medium")
    .option("-d, --due <date>", "Due date (ISO string or relative)")
    .action(async (description: string, opts: { priority?: string; due?: string }) => {
      const { loadAutonomousState, saveAutonomousState, addGoal } =
        await import("../../autonomous/state-store.js");
      const state = await loadAutonomousState();
      const priority = (opts.priority ?? "medium") as "high" | "medium" | "low";
      const dueAt = opts.due ? new Date(opts.due).getTime() : undefined;
      const goal = addGoal(state, description, priority, dueAt);
      await saveAutonomousState(state);
      console.log(`Goal added: ${goal.id}`);
      console.log(`  ${goal.description} [${goal.priority}]`);
    });

  goal
    .command("remove <goalId>")
    .description("Remove a goal by ID")
    .action(async (goalId: string) => {
      const { loadAutonomousState, saveAutonomousState } =
        await import("../../autonomous/state-store.js");
      const state = await loadAutonomousState();
      const index = state.goals.findIndex((g) => g.id === goalId);
      if (index === -1) {
        console.error(`Goal not found: ${goalId}`);
        process.exitCode = 1;
        return;
      }
      const removed = state.goals.splice(index, 1)[0];
      await saveAutonomousState(state);
      console.log(`Removed goal: ${removed?.description}`);
    });
}

function registerCycleCommand(parent: Command) {
  parent
    .command("cycle")
    .description("Trigger an immediate autonomous cycle (via gateway)")
    .action(async () => {
      // This sends a system event to trigger the cycle via the gateway.
      const { requestHeartbeatNow } = await import("../../infra/heartbeat-wake.js");
      requestHeartbeatNow({ reason: "autonomous:cycle-now" });
      console.log("Autonomous cycle triggered.");
    });
}
