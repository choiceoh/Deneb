# Gateway Config 변경 지도

이 패키지는 `deneb.json`의 typed view, 검증·기본값, startup bootstrap과
최종 gateway runtime 설정을 소유한다. runtime/server는 완성된 설정을
소비하며 파일 형식이나 보안 제약을 다시 해석하지 않는다.

## 진입점과 책임

- `types.go`의 `DenebConfig`, `GatewayConfig`, `AgentsConfig`가
  typed JSON 계약과 enum 상수를 정의한다.
- `loader.go`의 `ConfigSnapshot`, `LoadConfig`,
  `ValidateRawConfig`가 parse, 구조 검증, 기본값 적용을 소유한다.
- `bootstrap.go`의 `BootstrapGatewayConfig`와
  `ResolvedGatewayAuth`가 startup auth·환경 fallback·생성 token 수명주기를
  소유한다.
- `runtime.go`의 `GatewayRuntimeConfig`와
  `ResolveGatewayRuntimeConfig`가 bind, Tailscale, trusted proxy,
  Control UI의 최종 보안 제약을 적용한다.
- `paths.go`의 `StateDirPolicy`, `ConfigPathPolicy`,
  `GatewayPortPolicy`가 경로·포트 precedence를 소유한다.
- `raw_config.go`의 raw map helper는 custom model/role 설정을 저장할 때
  알려지지 않은 JSON key를 보존하는 쓰기 경계다.

## 의존 방향과 불변조건

- 의존 방향은 `runtime/commands → infra/config`다. config에서 server,
  RPC handler, pipeline을 import하지 않는다.
- 새 설정 필드는 `types.go`만 바꾸지 않는다. 필요에 따라
  `loader.go`의 validation/default, `schema.go`, bootstrap/runtime
  소비자와 각 계약 테스트를 한 변경으로 맞춘다.
- 설정을 수정해 저장할 때 typed `DenebConfig` 전체를 다시 marshal하지
  않는다. `prepareConfigMap`/`writeConfigMap` 경로로 미인식 key와
  plugin 설정을 보존한다.
- non-loopback 무인증 bind, 잘못된 trusted proxy, Tailscale funnel의 auth
  제약을 완화하지 않는다. `ResolveGatewayRuntimeConfig`가 최종 gate다.
- secret resolution과 token persist를 호출자에 복제하지 않는다. 로그나
  validation issue에 token/password 값을 포함하지 않는다.
- 기본값은 load 이후 일관되게 적용되어야 한다. missing file과 빈 JSON이
  서로 다른 runtime 결과를 내지 않게 테스트한다.

## Local change scope

설정 계약 변경은 typed config 경계에 가둔다. 런타임 배선을 여기로 끌어오지 않는다.

- 함께 바꿔도 되는 이웃: `internal/runtime/commands`(bootstrap 소비자),
  `internal/infra/secret`(credential resolution). `LoadConfig`/
  `BootstrapGatewayConfig`/`ResolveGatewayRuntimeConfig`가 바뀌면
  `loader_test.go`와 `runtime_test.go`를 먼저 본다.
- 건드리지 말 것: `internal/runtime/server` method registry,
  `internal/pipeline/chat` prompt cache, client wire 생성물. config에서
  server·RPC·pipeline을 import하지 않는다.
- 집중 검증: `cd gateway-go && go test -count=1 ./internal/infra/config`

## 집중 검증

설정 변경은 missing/invalid/valid JSON, default 적용, raw-key 보존과 보안
거부 경로를 함께 검증한다. 결정적 패키지 검증 명령은:

`cd gateway-go && go test -count=1 ./internal/infra/config`
