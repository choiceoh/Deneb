# Agent log 변경 지도

이 패키지는 agent run과 background 행동을 append-only JSONL로 기록하고,
관찰·튜닝용 조회와 집계를 제공한다. 상위 runtime은 typed event data만 전달하며
파일 형식과 aggregate 의미는 이 패키지가 소유한다.

## 진입점과 소유권

- `agentlog.go`의 `LogEntry`, `RunStartData`, `TurnToolData`, `RunEndData`와
  event type 상수가 JSONL wire 계약이다.
- `writer.go`의 `Writer`, `NewWriter`, `Writer.Append`, `Writer.LogEvent`가
  per-session 파일 생성, 직렬 append, bounded rotation을 소유한다.
- `runlogger.go`의 `RunLogger`, `NewRunLogger`가 한 run의 ID/session/time을
  묶어 typed events를 기록한다.
- `reader.go`의 `Writer.Read`, `Writer.ReadRun`, `Writer.ToolProvenance`와
  `aggregate_model.go`의 `Writer.AggregateByModel`, `aggregate_served.go`의
  `Writer.AggregateByServedModel`이 파생 read model을 제공한다.

## 의존 방향과 불변조건

- 의존 방향은 `pipeline/runtime/tools → core/agentlog → stdlib`다. agentlog는
  chat, model tuner, observe tool을 import하지 않고 소비자가 집계 결과를 해석한다.
- 한 `Writer`의 append, read, prune은 동일 mutex 경계 안에서 실행되어 JSONL
  line이 동시 쓰기로 섞이면 안 된다. session key는 path separator와 NUL을 제거해
  base directory 밖으로 나가지 못해야 한다.
- 파일은 최근 3,000개 valid entry로 bounded rotation하며 tmp write+rename 실패가
  원본 log를 손상시키면 안 된다. malformed line은 해당 line만 건너뛰고 나머지
  관찰 가능성을 보존한다.
- tool input/output 원문과 파일 내용은 기록하지 않는다. hash, byte count,
  sanitized target, file-effect metadata만 남긴다.
- model 집계의 run ID 상관관계는 session 파일마다 격리하고 requested model에
  실패를 귀속한다. fallback model은 별도 counter로만 기록한다.
- `AggregateByModel`(requested 귀속)과 `AggregateByServedModel`(provider가 보고한
  served model 귀속)은 서로 다른 질문에 답한다. 튜너 scorecard는 전자, 사용량
  패널 라벨은 후자를 쓴다 — 엔드포인트가 핀을 alias 하면(예: `glm-5.1` 핀에
  glm-5.3이 응답) requested id는 실제로 돌지 않은 모델을 가리키기 때문이다.
  served 집계의 토큰은 turn.llm에서 오므로 run 중간에 fallback이 걸리면 두 모델로
  쪼개진다. 두 집계의 run 수와 token 합은 같은 window에서 항상 일치해야 한다
  (served는 라벨만 바꾸지 usage를 잃지 않는다). run ID는 session 파일 안에서도
  재사용되므로(게이트웨이 재시작 시 run_0000부터 다시) run.start가 해당 ID의
  scope를 새로 연다.

## 테스트와 집중 검증

- `contracts_test.go`의 `TestReadAllEntriesRecoveryContract`,
  `TestWriterConcurrentAppendPreservesEveryEntry`,
  `TestRunLoggerTruncatesAtUTF8Boundary`가 내구성과 privacy 경계를 검증한다.
- `aggregate_model_characterization_test.go`의
  `TestAggregateByModel_PreservesPerSessionCorrelationAndMalformedIsolation`과
  `aggregate_characterization_test.go`의
  `TestAggregate_PreservesFoldFilteringAndStableToolOrder`가 집계 의미를 고정한다.

`cd gateway-go && go test -count=1 ./internal/core/agentlog`
