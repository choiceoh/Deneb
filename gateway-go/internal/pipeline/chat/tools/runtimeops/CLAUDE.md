# Runtime operations tool 변경 지도

이 패키지는 agent가 현재 runtime을 관찰·제어하는 tool 구현을 소유한다. schema와
등록 정책은 `toolreg`, 공용 실행 계약과 context 값은 `toolport`, 실제 process와
session 상태는 주입된 infra/domain service가 소유한다.

## 진입점과 소유권

- `exec.go`의 `ToolExec`, `ToolProcess`가 foreground/background command와 managed
  process 제어를 제공한다. `exec_safety.go`의 `CheckCatastrophicCommand`,
  `CheckDestructiveCommand`가 실행 전 안전 판정을 소유한다.
- `exec_safety.go`의 `ExecCommandPreservesRunCache`는 shell pipeline을
  `parseExecCacheStages`로 분류하고 각 stage의 cache 보존 가능성을 판정한다.
  빈 stage·chaining·substitution·미인식 command는 무효화 방향으로 fail-closed한다.
- Session tools (`sessions` / `sessions_spawn` / `subagents`) live in sibling
  `tools/sessionops`.
- Deferred schema activation (`fetch_tools`) lives in sibling `tools/fetchops`.
  Observe (`ToolObserve`) lives in leaf package `observeops/` and is wired via
  `server/toolbind/observebind`.
- `browser.go`의 `ToolBrowser`는 agent-facing wrapper만 소유한다. Page Agent
  bridge client와 전자결재 enrichment는 `platform/browserbridge`가 소유한다.
- Phone tools (`phone_read`/`phone_write`) live in sibling `tools/phoneops`.
  Native app dispatch (`PhoneActionFunc`, `ErrPhoneActionUnconfirmed`) stays
  on `tooldeps`.
- `gateway.go`의 `GatewayDeps`, `ToolGatewayWithDeps`가 status,
  config, restart, update approval 흐름을 소유한다.

## 의존 방향과 불변조건

- 의존 방향은 `toolreg → tools/runtimeops → toolport + domain/infra services`다.
  runtimeops는 상위 `tools`나 `pipeline/chat`를 import하거나 자신을 등록하지 않는다.
- `ToolExec`는 catastrophic command를 process-manager와 fallback 양쪽에서 실행 전에
  반드시 거절한다. in-place write는 checkpoint하고, workdir를 검증하며, child env는
  secret이 제거된 환경 위에 명시 인자만 합친다. timeout은 10분 상한이다.
- output 원본의 truncation/spillover는 registry 한 곳에서만 적용한다. 이 패키지에서
  먼저 잘라 중간 결과를 유실시키면 안 된다.
- exec run-cache는 pipeline의 모든 stage가 읽기 전용으로 증명될 때만
  보존한다. parser 실패나 모호한 flag를 실행 실패와 혼동해 cache를
  남기지 말고, workspace mutation 가능성으로 처리한다.
- session spawn은 depth, 동시성, role 가용성, tool preset을 검증하고 terminal child를
  cap에 포함하지 않는다. deferred activation은 등록된 deferred tool과 허용된 preset만
  노출한다.
- config/restart/update mutation은 payload에 결합된 단일사용·만료 approval token 없이는
  실행하면 안 되며 secret path와 dirty/non-main update를 거절한다.

## 테스트와 집중 검증

- `exec_safety_test.go`의 `TestCheckCatastrophicCommandRejectsCatastrophicPaths`와
  `TestExecCacheClassificationPreservesCacheOnlyForReadOnlyStages`,
  `contracts_test.go`의 `TestToolExecFallbackValidationStructuredAndHints`가 두 실행
  경로의 안전성을 검증한다.
- `fetch_tools` 검증은 `tools/fetchops`, session 검증은 `tools/sessionops`로 옮겼다.
- `gateway_test.go`의 `TestGatewayConfigSetReturnsApprovalThenWritesOnConfirm`과
  `TestGatewayConfirmedWithoutApprovalRejected`가 mutation 승인 계약을 확인한다.

`cd gateway-go && go test -count=1 ./internal/pipeline/chat/tools/runtimeops`
