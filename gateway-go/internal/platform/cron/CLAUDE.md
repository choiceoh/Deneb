# Cron platform change map

This package owns durable schedules, scheduler lifecycle, one-run execution,
delivery resolution, retry accounting, and the persistent run log. Runtime
adapters inject the agent runner and channel handoff; the cron package does not
depend on a concrete chat handler.

## Entry points

- `service.go`: `Service`, `NewService`, and typed setter ports define runtime
  composition. CRUD and manual-run methods share the store state machine.
- `service_lifecycle.go`: `Service.Start`, `StopCtx`, and `Status` own loop and
  in-flight cancellation lifecycle.
- `service_execution.go` and `service_job_run.go` own one-run ordering: started
  event, agent/retry, delivery, state/run-log commit, then finished event.
- `schedule.go`: `ComputeNextRunAtMs`, `ParseSmartScheduleWithOpts`, and
  `FormatHumanSchedule` own schedule semantics.
- `delivery.go` owns normalized channel targets. `runlog.go` owns append and
  paged query persistence.

## Dependency direction and invariants

- Cron consumes `AgentRunner`, `SubagentPoller`, and transcript/delivery ports;
  it must not import the chat implementation or runtime server.
- Scheduler-triggered runs always advance the recurring schedule. A manual run
  preserves a future next-run and advances an overdue one.
- Caller cancellation is recorded once and is never retried. Overlap or abort
  may queue exactly one pending rerun.
- State and run-log commits happen before the terminal event so observers can
  immediately read the completed result.
- `StopCtx` owns cancellation and bounded wait; no job goroutine may outlive a
  successful stop.

## Focused verification

Use `service_execution_test.go`, `service_scheduler_test.go`,
`pending_rerun_test.go`, and `runlog_test.go` for matching changes.

`cd gateway-go && go test -count=1 ./internal/platform/cron`

Lifecycle changes also require `go test -race -count=1 ./internal/platform/cron`.
