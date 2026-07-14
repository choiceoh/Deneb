# Skill generation 변경 지도

이 패키지는 관측된 session/dream 신호를 후보 `SKILL.md`로 생성하고 품질·중복·
rate gate를 통과한 결과만 영속한다. runtime scheduling과 전체 evolution 상태는
상위 genesis가 소유하며 여기서는 생성·검증·persist 경계에 집중한다.

## 진입점과 소유권

- `service.go`의 `Config`, `Service`, `NewService`, `Service.Evaluate`,
  `Service.Generate`, `Service.GenerateFromDream`, `Service.Persist`가 genesis
  pipeline을 소유한다.
- `service.go`의 `GeneratedSkill`, `SessionContext`, `ToolActivity`가 LLM
  입력·출력 전의 typed 경계다.
- `meta_artifacts.go`의 `MetaArtifacts`, `NewMetaArtifacts`,
  `MetaArtifacts.MaterializeDefaults`가 versioned prompt artifact와 compiled
  fallback의 provenance를 관리한다. 기본 prompt 정본은 `prompts.go`다.

## 의존 방향과 불변조건

- 의존 방향은 `runtime/genesis → generation → llm + skills catalog + common`이다.
  generation은 runtime/server, chat tool, tracker 저장소를 import하지 않고 LLM과
  catalog를 주입받는다.
- `Evaluate`는 최소 turn/tool diversity와 재시작에도 보존되는 일일 cap을 반드시
  통과시킨다. daily-cap 파일은 tmp+rename으로 원자적으로 갱신한다.
- LLM 결과는 제안일 뿐이다. parse 후 description 단일행 정규화, actionable
  section specificity, optional independent judge, name 정규화, catalog dedup,
  cooldown 순서를 건너뛰고 `Persist`하면 안 된다.
- `Persist`는 완성된 `SKILL.md`를 atomic write한 다음 catalog와 rate state를
  갱신한다. `ErrSkillDeduped`는 의도된 no-op이며 장애로 재시도하지 않는다.
- `MetaArtifacts`는 짧거나 읽을 수 없는 artifact를 compiled fallback으로
  degrade하고, provenance가 다른 operator/slow-loop 수정 파일을 덮어쓰지 않는다.

## 테스트와 집중 검증

- `service_test.go`의 `TestEvaluateAllowsSessionMeetingAllCriteria`, `TestSkillSpecificityIssuesPassesWellFormedAndRejectsVagueSkills`,
  `TestBuildSkillMDReturnsFrontmatterOriginAndBody`가 생성 gate와 wire 형식을 검증한다.
- `daily_cap_test.go`의 `TestGenesis_DailyCapPersistsAcrossRestart`,
  `dedup_test.go`의 `TestIsDuplicateSkillReturnsTrueForExactAndNearMatchesButNotUnrelated`, `meta_artifacts_test.go`의
  `TestMetaArtifacts_MaterializeIsWriteIfAbsent`가 재시작·중복·provenance 계약을
  고정한다.

`cd gateway-go && go test -count=1 ./internal/domain/skills/genesis/generation`
