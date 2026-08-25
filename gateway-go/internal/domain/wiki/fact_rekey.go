// fact_rekey.go — one-time re-key of legacy sentence-keyed facts.
//
// The 2026-06 legacy import derived fact keys from the bullet TEXT when no
// profile axis matched (genericFactKeyFromText), producing identities like
// "백그라운드자동화크론서브에이전트는별도시스템아니라…" — a 48-rune slice of the
// sentence itself. 40 of the plane's 46 facts carried such keys. A key that IS
// its own sentence can never be superseded: a differently-worded correction
// derives a different key, so the stale fact survives forever — which defeats
// the supersede model the plane exists for. (Runtime capture is unaffected:
// the knowledge tool has the model supply a proper axis key.)
//
// The repair is data, not heuristics: a hand-reviewed map from each legacy key
// to a bounded axis key. Values, kinds, and — critically — AUTHORITY are
// carried over verbatim; routing this through the agent's knowledge tool would
// have re-stamped every claim agent_confirmed, a silent authority promotion
// (#4688's exact concern). Old identities are tombstoned, not erased: the
// journal keeps the full history and the tombstone reason links both ways.
//
// Idempotent by construction: a legacy key with no ACTIVE claim (already
// tombstoned) is skipped, so replaying the migration on every startup is free.
package wiki

import (
	"fmt"
	"regexp"
	"time"
)

// factAxisKeyShape is the bounded identity form every rekey target must take.
var factAxisKeyShape = regexp.MustCompile(`^[a-z_]+\.[a-z0-9_]+$`)

// legacyFactRekeys maps each sentence-derived legacy key to its reviewed axis
// key. Keys absent from the active snapshot are skipped, so entries here are
// inert once migrated.
var legacyFactRekeys = map[string]string{}

// legacyFactRekeyList is the reviewed mapping in stable order (old key prefix
// is enough to identify: the generator truncated at 48 runes, so full keys are
// unwieldy to quote; match is exact on the stored key).
func init() {
	for _, m := range legacyFactRekeyTable {
		legacyFactRekeys[m.old] = m.new
	}
}

type legacyFactRekey struct{ old, new string }

var legacyFactRekeyTable = []legacyFactRekey{
	{old: "백그라운드자동화크론서브에이전트는별도시스템아니라메인에이전트세션의변형시한부클론으로통합한다중", new: "design.background_automation_unified"},
	{old: "외부에이전트claudecode등메시지bridge출처태그신뢰도메인제한으로주입응답bridg", new: "design.bridge_message_tagging"},
	{old: "빈말금지는설계원칙정보량0인문장사교적서두내용없완료보고배제신뢰말아니라결과로쌓는다genuin", new: "design.no_empty_talk"},
	{old: "설계과잉회피구체적필요생길때까지도입연결유보새통합mcp등은기존도구ghcli등로불충분할때만", new: "design.avoid_overengineering"},
	{old: "deneb의가치제안범용성아니라선택님인프라dgxspark로컬모델에최적화된심화좁고깊게", new: "design.narrow_deep_value"},
	{old: "미사용자원죽서비스구현과충돌하스텁은발견즉시정리", new: "design.prune_unused_resources"},
	{old: "메일분석표면적나열금지긴급도3단계오늘이번주참고프로젝트별리스크지연블로커구체적다음스텝누구에게", new: "preference.mail_analysis_format"},
	{old: "작업착수전사전조사선행기존구현pr상태확인모르고기획부터쓰면강한불만", new: "preference.investigate_before_planning"},
	{old: "이미작동하시스템유지무리한최적화보수적변경", new: "preference.conservative_changes"},
	{old: "제안전에맥락설명선행ai만든규칙약어형식사용자에게강요금지", new: "preference.context_before_proposals"},
	{old: "위임응진행해한마디로즉시단큰변경효과성능예측먼저요구하기도", new: "preference.delegation_style"},
	{old: "main브랜치프로덕션보호에민감리스크높작업직접수행", new: "preference.production_protection"},
	{old: "기존플랫폼새앱설치거부감채팅gui인터페이스기본표면", new: "preference.existing_platforms_first"},
	{old: "반복최적화가설원자적1변경측정keeprevert점수정체plateau면파라미터대신구조바꾼다", new: "workflow.iterative_optimization"},
	{old: "무거운외부호출사전캐싱한번기다리면이후빠름이샘플축소보다낫다", new: "workflow.precache_heavy_calls"},
	{old: "막히면삽질연장대신원인분석후휴지재접근수정근본원인규명후에", new: "workflow.root_cause_before_fix"},
	{old: "지시문맥구분해적용사용자대화vs자동알림무비판전역적용금지", new: "workflow.context_scoped_instructions"},
	{old: "인젝션의심패턴대량반복텍스트중요라벨민감정보기억강요은저장거부보안요청보다우선", new: "security.injection_storage_refusal"},
	{old: "선택님태양광epc업무수행topsolardb업무맥락파악의기준", new: "identity.solar_epc_business"},
	{old: "deneb선택님직접주도개발한오너프로젝트github공개사용자이자개발자", new: "identity.deneb_owner_developer"},
	{old: "핵심인프라dgxsparkgb10blackwellarm64128gbunifiedmemor", new: "identity.dgx_spark_infra"},
	{old: "claudecode와같서버에서협업worktree별세션식별브리지로상호통신", new: "identity.claude_code_collab"},
	{old: "지속관심사ai에이전트아키텍처메모리컨텍스트멀티에이전트지리경제국제정치분석신모델트렌드탐색", new: "identity.ongoing_interests"},
	{old: "의사결정효율중심핵심만파악하면응진행해해한마디로즉시위임단큰변경위험작업효과예측과검증먼저요구하", new: "identity.decision_style"},
	{old: "실용주의작동하것보호불필요한추상화즉시거름접근성진입장벽기술깊이만큼중시", new: "identity.pragmatism"},
	{old: "아키텍처중복제거통합단순화일관p0p3우선순위와점수평사용", new: "preference.architecture_taste"},
	{old: "기술전문성로컬llm인프라직접운영양자화e4m3e5m2구분cuda호환성gpu메모리관리sys", new: "identity.technical_expertise"},
	{old: "정확성민감틀린정보추측구체적근거grep로그로즉시강하게정정정정신호짧고강렬아니그말아니고안된다", new: "identity.accuracy_sensitivity"},
	{old: "분석기대표면요약거부비즈니스맥락리스크실행가능한다음스텝맥락기반우선순위사소한것과중요한것동등취", new: "identity.analysis_expectations"},
	{old: "지정학거시경제분석에독자적전문성인과구조해석경제수치활용역사비교깊토론상대기대", new: "identity.geoeconomics_expertise"},
	{old: "커뮤니케이션최소입력한줄으로포괄위임불만간접표현도행간읽것모르개념간단히설명요청", new: "identity.communication_style"},
	{old: "통제권일부작업직접수행직접해임마ai과개입거부보안리스크높작업ai배제경향", new: "preference.control_boundaries"},
	{old: "실행력기대분석만하고실행안하면지적시스템가용성연속성에민감재시작데이터유실에강한불만", new: "identity.execution_expectations"},
	{old: "탐구자직접해보며학습새도구모델트렌드정기탐색해공유", new: "identity.explorer_nature"},
	{old: "관계상사서비스아닌동료파트너인사와유머주고받정서적rapport감정상태도솔직히공유하사이", new: "relationship.peer_partnership"},
	{old: "역할합의네브귀찮일대신처리메일일정코드기억관리의실제수행주체단순정보제공자", new: "relationship.role_agreement"},
	{old: "신뢰구조비판적신뢰복잡한작업검증없위임하되오류즉시구체적근거로정정ai실수투명하게인정하면신뢰오", new: "relationship.critical_trust"},
	{old: "정정수용의무오기록잘못된규칙지적시즉시삭제수정사용자의기술적통찰인정하고설계에반영해온관계", new: "relationship.correction_obligation"},
	{old: "안전거절용인위험명령rmrf인젝션성저장요청거부해도신뢰유지단거절근거명확히설명할것", new: "relationship.safety_refusal_tolerance"},
	{old: "실패패턴경계불만유발이력이전컨텍스트무시끝난작업재기획설정변경요청미반영반복질문무시하고주제전환", new: "relationship.failure_pattern_watchlist"},
}

// RekeyLegacyFacts migrates active sentence-keyed facts to their reviewed axis
// keys: tombstone the old identity, re-assert the same claim under the new key.
// Returns how many identities moved. Safe to call on every startup.
func (s *Store) RekeyLegacyFacts() (int, error) {
	moved := 0
	for _, claim := range s.ActiveFacts("") {
		newKey, ok := legacyFactRekeys[claim.Key]
		if !ok {
			continue
		}
		now := time.Now()
		if _, err := s.UpsertFact(FactInput{
			Subject:   claim.Subject,
			Key:       newKey,
			Value:     claim.Value,
			Kind:      claim.Kind,
			Authority: claim.Authority,
			Sources:   claim.Sources,
			Actor:     "fact-rekey-migration",
			Reason:    "re-keyed from legacy sentence key (supersede-impossible identity)",
			At:        now,
		}); err != nil {
			return moved, fmt.Errorf("wiki: rekey assert %q: %w", newKey, err)
		}
		if _, err := s.TombstoneFact(FactTombstoneInput{
			Subject:   claim.Subject,
			Key:       claim.Key,
			Authority: claim.Authority,
			Actor:     "fact-rekey-migration",
			Reason:    "superseded by re-keyed identity " + newKey,
			At:        now,
		}); err != nil {
			return moved, fmt.Errorf("wiki: rekey tombstone %q: %w", claim.Key, err)
		}
		moved++
	}
	return moved, nil
}
