# Step 2: 핸들러 Deps → GatewayHub 마이그레이션 상세 계획

## 목표

28개 RPC 핸들러 Deps 구조체 중 Hub 필드로 대체 가능한 ~15개를 마이그레이션하여,
서버 와이어링 측(server_rpc*.go)에서 Deps 구조체 조립 코드를 대폭 축소한다.

## 현황 분석

### Deps 구조체 분류

**그룹 A — Hub.Broadcast() 메서드로 대체 가능 (7개)**

BroadcastFunc만 사용하거나 Hub 필드 1-2개 + BroadcastFunc 조합인 핸들러:

| Deps 구조체 | 필드 수 | Hub 필드 | Local 필드 | 전략 |
|---|---|---|---|---|
| `agent.AgentsDeps` | 2 | Agents | Broadcaster | Hub만으로 가능 |
| `skill.Deps` | 2 | Skills | Broadcaster | Hub만으로 가능 |
| `node.DeviceDeps` | 2 | Devices | Broadcaster | Hub만으로 가능 |
| `system.ConfigAdvancedDeps` | 1 | - | Broadcaster | Hub만으로 가능 |
| `skill.ToolDeps` | 1 | Processes | - | Hub만으로 가능 |
| `channel.MessagingDeps` | 1 | Telegram | - | Hub만으로 가능 |
| `channel.EventsDeps` | 2 | - | Broadcaster, Logger | Hub만으로 가능 (Logger도 Hub에 있음) |

**그룹 B — Hub + 소수 Local 필드 (6개)**

Hub 필드가 대부분이지만 1-2개 Local 필드가 필요한 핸들러:

| Deps 구조체 | 필드 수 | Hub 필드 | Local 필드 | 전략 |
|---|---|---|---|---|
| `agent.ExtendedDeps` | 7 | Sessions, GatewaySubs, Processes, Cron, Hooks (5) | TelegramPlugin, Broadcaster (2) | Hub + TelegramPlugin |
| `channel.LifecycleDeps` | 3 | Hooks (1) | TelegramPlugin, Broadcaster (2) | Hub만으로 가능 (Telegram도 Hub에 있음) |
| `process.CronAdvancedDeps` | 3 | Cron (1) | RunLog, Broadcaster (2) | Hub + RunLog (CronRunLog도 Hub에 있음) |
| `node.Deps` | 3 | Nodes (1) | Broadcaster, CanvasHost (2) | Hub + CanvasHost string |
| `chat.BtwDeps` | 2 | Chat interface | Broadcaster | Hub만으로 가능 |
| `session.ExecDeps` | 3 | Agents, JobTracker | Chat | Hub만으로 가능 (Chat도 Hub에 있음) |

**그룹 C — Local 전용, 유지 (15개)**

Hub와 무관하거나 도메인 특화된 Deps:

| Deps 구조체 | 이유 |
|---|---|
| `session.Deps` | Transcripts, Compressor는 Hub에 없음 |
| `chat.Deps` | Chat 1필드, 변경 이점 없음 |
| `process.ACPDeps` | 9필드 중 7개가 ACP 전용 |
| `process.CronServiceDeps` | Service 1필드 |
| `process.ApprovalDeps` | Store는 Hub에 없음 (별도 생성) |
| `presence.Deps` / `HeartbeatDeps` | Store/State는 inline 생성 |
| `platform.WizardDeps/TalkDeps/SecretDeps` | 각 1필드, 독립 도메인 |
| `provider.Deps/ModelsDeps` | Provider 전용 |
| `gateway.Deps` | 13개 func 필드, 서버 상태 질의 전용 |
| `system.*Deps` (6개) | 각각 고유 서비스 (Runner, Tracker, LogDir 등) |
| `ffi.VegaDeps` | Backend 전용 |

## 구현 방식

### 접근법: `Methods(hub *GatewayHub)` 오버로드

각 핸들러 패키지에서:
1. 기존 `Methods(deps Deps)` → 유지 (하위 호환)
2. `MethodsFromHub(hub *GatewayHub)` 추가 — Hub에서 Deps를 조립하여 `Methods()` 호출
3. 서버 측에서 `MethodsFromHub(hub)` 호출로 교체

```go
// handler/agent/agent.go
func CRUDMethodsFromHub(hub *rpcutil.GatewayHub) map[string]rpcutil.HandlerFunc {
    return CRUDMethods(AgentsDeps{
        Agents:      hub.Agents,
        Broadcaster: hub.Broadcast,
    })
}
```

### 대안: 인터페이스 기반 (비권장)

Hub에 인터페이스를 정의하면 rpcutil에 무거운 import가 생겨 순환 의존성 위험.
위의 오버로드 방식이 안전.

## 파일별 변경 상세

### 1. GatewayHub에 Chat, Telegram 추가 확인

`gateway_hub.go`에 이미 Chat, Telegram 필드가 있음. ✅

### 2. rpcutil에 GatewayHub 인터페이스 정의 (순환 의존성 방지)

rpcutil은 lightweight 패키지여야 하므로 GatewayHub를 직접 넣을 수 없음.
대신 server 패키지 내부에서 Hub→Deps 변환을 수행.

```go
// server/hub_adapters.go (신규)
func agentCRUDDepsFromHub(hub *GatewayHub) handleragent.AgentsDeps { ... }
func skillDepsFromHub(hub *GatewayHub) handlerskill.Deps { ... }
func channelEventsDepsFromHub(hub *GatewayHub) handlerchannel.EventsDeps { ... }
// ... 13개 어댑터 함수
```

### 3. server_rpc.go + server_rpc_channel.go 와이어링 축소

**Before (registerAgentMethods)**:
```go
func (s *Server) registerAgentMethods() {
    s.dispatcher.RegisterDomain(handlerprocess.ACPMethods(s.acpDeps))
    s.dispatcher.RegisterDomain(handleragent.ExtendedMethods(handleragent.ExtendedDeps{
        Sessions:       s.sessions,
        TelegramPlugin: s.telegramPlug,
        GatewaySubs:    s.gatewaySubs,
        Processes:      s.processes,
        Cron:           s.cron,
        Hooks:          s.hooks,
        Broadcaster:    s.broadcaster,
    }))
}
```

**After**:
```go
func (s *Server) registerAgentMethods(hub *GatewayHub) {
    s.dispatcher.RegisterDomain(handlerprocess.ACPMethods(s.acpDeps))
    s.dispatcher.RegisterDomain(handleragent.ExtendedMethods(agentExtendedDepsFromHub(hub)))
}
```

### 4. 변경 파일 목록

| 파일 | 변경 |
|---|---|
| `server/hub_adapters.go` | **신규** — Hub→Deps 변환 어댑터 13개 (~80줄) |
| `server/server_rpc.go` | register* 메서드에 hub 파라미터 추가, Deps 인라인 구성 제거 |
| `server/server_rpc_channel.go` | 같은 패턴 적용 |
| `server/server_rpc_session.go` | registerApprovalAgentMethods에 hub 파라미터 추가 |
| `server/server.go` | New()에서 buildHub() 호출 시점 조정 |

## 예상 효과

- server_rpc.go + server_rpc_channel.go에서 Deps 구조체 인라인 조립 코드 **~100줄 제거**
- broadcastFn 클로저 **4개 → 0개** (hub.Broadcast 메서드 사용)
- 새 핸들러 추가 시 Hub 필드 참조만으로 와이어링 완료

## 위험 요소

- `GatewayHub`는 `chatHandler` 생성 전에 빌드되므로 `Chat` 필드가 nil
  → `registerSessionRPCMethods()` 이후에 `hub.Chat = s.chatHandler` 설정 필요
- 테스트에서 Hub 전체를 모킹하면 무겁다
  → 기존 Deps 기반 `Methods()` 유지하여 테스트에서는 Deps 직접 전달

---

# Step 4 (신규): 테이블 기반 RPC 등록 상세 계획

## 목표

`server_rpc.go`의 8개 register* 메서드 + `server_rpc_channel.go`의 10개 register* 메서드를
하나의 `registerAllMethods(hub)` 테이블로 통합하여 등록 코드를 **~150줄 → ~60줄**로 축소.

## 현황 분석

### 현재 등록 흐름 (server.go:New() 내부)

```
s.registerBuiltinMethods()                     // server_rpc.go:142
rpc.RegisterBuiltinMethods(...)                // rpc/register.go
s.registerExtendedMethods()                    // server_rpc.go:57
  ├─ registerAgentMethods()                    //   :66
  ├─ registerProviderMethods()                 //   :81
  ├─ registerToolMethods()                     //   :88
  ├─ registerAuroraMethods()                   //   :94
  ├─ registerAuthRPCMethods()                  //   server_rpc_auth.go
  └─ registerSessionRPCMethods()               //   server_rpc_session.go (큰 함수)
s.registerPhase2Methods()                      // server_rpc.go:101
  ├─ registerPhase2ChannelMethods(broadcastFn) //   :109
  │   ├─ registerEventsBroadcastMethods()      //   server_rpc_channel.go
  │   ├─ registerConfigLifecycleMethods()      //   server_rpc_channel.go
  │   ├─ registerSubscriptionMethods()         //   server_rpc_channel.go
  │   └─ registerHeartbeatMethods(broadcastFn) //   server_rpc_channel.go
  └─ registerPhase2SystemMethods(broadcastFn)  //   :116
      ├─ registerMonitoringMethods()           //   server_rpc_channel.go
      ├─ registerIdentityMethods()             //   server_rpc_channel.go
      ├─ registerPresenceMethods(broadcastFn)  //   server_rpc_channel.go
      └─ registerModelsMethods()               //   server_rpc_channel.go
s.registerAdvancedWorkflowMethods()            // server_rpc.go:125
  ├─ registerApprovalAgentMethods(broadcastFn) //   server_rpc_session.go
  ├─ registerAdvancedChannelMethods(broadcastFn)// server_rpc_channel.go
  └─ initGmailPoll()                          //   server_chat_config.go
s.registerNativeSystemMethods(denebDir)        // server_rpc.go:137
  └─ registerSystemServiceMethods(denebDir)    //   server_rpc_channel.go
```

### 문제

1. **18개의 register* wrapper 메서드** — 대부분 1-3줄로 dispatcher.RegisterDomain() 호출만 수행
2. **broadcastFn 클로저 4회 생성** — registerPhase2Methods, registerAdvancedWorkflowMethods, registerSessionRPCMethods, registerApprovalAgentMethods
3. **3단계 중첩** — registerExtendedMethods → registerAgentMethods → inline Deps 구성
4. **Phase 분리가 더 이상 필요없음** — 원래 Phase 분리는 순서 의존성 때문이었으나, 현재 대부분의 핸들러는 독립적

## 구현 방식

### 접근법: 2단계 등록 (Early + Late)

모든 RegisterDomain 호출을 정리하되, **순서 의존성**을 존중:

1. **Early Registration** — chatHandler 생성 전에 등록 가능한 도메인
2. **Late Registration** — chatHandler 생성 후에만 등록 가능한 도메인 (Chat 의존)

```go
// server/method_registry.go (신규)

// registerEarlyMethods registers RPC domains that don't depend on chatHandler.
func (s *Server) registerEarlyMethods(hub *GatewayHub) {
    domains := []map[string]rpcutil.HandlerFunc{
        // Gateway runtime
        handlergateway.RuntimeMethods(s.buildGatewayDeps()),

        // Session state
        handlersession.Methods(sessionDepsFromHub(hub, s.transcript, s.sessionCompressor)),

        // Agent orchestration
        handleragent.ExtendedMethods(agentExtendedDepsFromHub(hub)),
        handlerprocess.ACPMethods(s.acpDeps),

        // Channel & events
        handlerchannel.BroadcastMethods(channelEventsDepsFromHub(hub)),
        handlerchannel.EventsMethods(channelEventsDepsFromHub(hub)),
        handlerchannel.LifecycleMethods(channelLifecycleDepsFromHub(hub)),
        handlerchannel.MessagingMethods(channelMessagingDepsFromHub(hub)),

        // Presence & heartbeat
        handlerpresence.Methods(presenceDepsFromHub(hub, s.presenceStore)),
        handlerpresence.HeartbeatMethods(heartbeatDepsFromHub(hub, s.heartbeatState)),

        // System
        handlersystem.IdentityMethods(hub.Version),
        handlersystem.MonitoringMethods(s.buildMonitoringDeps()),
        handlersystem.ConfigReloadMethods(s.buildConfigReloadDeps()),
        handlersystem.ConfigAdvancedMethods(configAdvancedDepsFromHub(hub)),
        handlersystem.UsageMethods(handlersystem.UsageDeps{Tracker: s.usageTracker}),
        handlersystem.LogsMethods(handlersystem.LogsDeps{LogDir: denebDir + "/logs"}),
        handlersystem.DoctorMethods(s.buildDoctorDeps()),
        handlersystem.MaintenanceMethods(handlersystem.MaintenanceDeps{Runner: s.maintRunner}),
        handlersystem.UpdateMethods(handlersystem.UpdateDeps{DenebDir: denebDir}),

        // Provider & skills
        handlerskill.ToolMethods(toolDepsFromHub(hub)),
        handlerskill.Methods(skillDepsFromHub(hub)),
        handlerskill.PluginMethods(handlerskill.PluginDeps{PluginRegistry: s.pluginRegistryAdapter()}),

        // Advanced workflow
        handlerprocess.ApprovalMethods(approvalDepsFromHub(hub)),
        handleragent.CRUDMethods(agentCRUDDepsFromHub(hub)),
        handlernode.Methods(nodeDepsFromHub(hub, canvasHost)),
        handlernode.DeviceMethods(deviceDepsFromHub(hub)),
        handlerprocess.CronAdvancedMethods(cronAdvancedDepsFromHub(hub)),
        handlerprocess.CronServiceMethods(handlerprocess.CronServiceDeps{Service: hub.CronSvc}),

        // Platform
        handlerplatform.WizardMethods(handlerplatform.WizardDeps{Engine: hub.Wizard}),
        handlerplatform.TalkMethods(handlerplatform.TalkDeps{Talk: hub.Talk}),
    }
    for _, d := range domains {
        if d != nil {
            s.dispatcher.RegisterDomain(d)
        }
    }
}

// registerLateMethodsは chatHandler 생성 후 호출.
func (s *Server) registerLateMethods(hub *GatewayHub) {
    hub.Chat = s.chatHandler // late-bind

    domains := []map[string]rpcutil.HandlerFunc{
        handlerchat.Methods(handlerchat.Deps{Chat: hub.Chat}),
        handlerchat.BtwMethods(btwDepsFromHub(hub)),
        handlersession.ExecMethods(execDepsFromHub(hub)),
        handleraurorachannel.Methods(handleraurorachannel.Deps{Chat: hub.Chat}),
    }
    for _, d := range domains {
        if d != nil {
            s.dispatcher.RegisterDomain(d)
        }
    }
}
```

### 변경 파일

| 파일 | 변경 |
|---|---|
| `server/method_registry.go` | **신규** — registerEarlyMethods() + registerLateMethods() (~80줄) |
| `server/server_rpc.go` | 대폭 축소: registerExtendedMethods/Phase2/Advanced/Native 모두 삭제 → method_registry 호출 (~195줄 → ~40줄) |
| `server/server_rpc_channel.go` | register* wrapper 18개 함수 삭제 → 특수 로직만 유지 (~251줄 → ~80줄) |
| `server/server.go:New()` | registerBuiltin + registerEarly + registerSession + registerLate 순서로 단순화 |
| `server/server_rpc_session.go` | registerApprovalAgentMethods의 RPC 등록 부분 → registerEarlyMethods로 이동, 비즈니스 로직(autonomous 등)만 유지 |

## 순서 의존성 매트릭스

```
server.New()
  ├─ 1. Core subsystem 초기화 (broadcaster, sessions, processes, cron, hooks, etc.)
  ├─ 2. registerEarlyMethods(hub)  ← chatHandler 불필요한 ~30개 도메인
  ├─ 3. registerSessionRPCMethods()  ← chatHandler 생성
  ├─ 4. registerLateMethods(hub)  ← chatHandler 의존 ~4개 도메인
  ├─ 5. registerApprovalAgentMethods(hub)  ← autonomous, dreaming (비 RPC 로직)
  └─ 6. Telegram plugin, Gmail poll (비 RPC 로직)
```

## 예상 효과

| 지표 | Before | After |
|---|---|---|
| register* wrapper 메서드 수 | 18개 | 2개 (Early + Late) |
| broadcastFn 클로저 생성 | 4회 | 0회 (hub.Broadcast) |
| server_rpc.go 줄 수 | 237줄 | ~40줄 |
| server_rpc_channel.go 줄 수 | 251줄 | ~80줄 |
| RegisterDomain 호출 산재 | 4개 파일 | 1개 파일 (method_registry.go) |

## 위험 요소

1. **Provider conditional**: `s.providers != nil` 조건으로 등록되는 도메인 → nil 체크 후 테이블에 추가
2. **ConfigReload special**: `propagateConfigReload()` 로직은 등록이 아니라 콜백 와이어링 → method_registry에서 제외, 별도 유지
3. **Telegram creation timing**: telegramPlug은 `registerNativeSystemMethods` 안에서 생성 → Early 등록 전에 생성 순서 조정 필요
4. **ApprovalMethods 부수 효과**: `processes.SetApprover()` 콜백은 RPC 등록이 아닌 비즈니스 로직 → method_registry에서 분리

## 구현 순서

1. `hub_adapters.go` 생성 (Hub→Deps 변환)
2. `method_registry.go` 생성 (registerEarlyMethods + registerLateMethods)
3. server_rpc.go의 register* 메서드 → method_registry로 이동
4. server_rpc_channel.go의 register* 메서드 → method_registry로 이동
5. server.go:New()에서 호출 순서 업데이트
6. 빌드 + 테스트 검증
