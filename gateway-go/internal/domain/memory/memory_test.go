package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClassifyHeuristics_Targets(t *testing.T) {
	cases := []struct {
		msg    string
		target WriteTarget
		subj   string
	}{
		{"기억해: 나는 저녁을 늦게 먹는 걸 선호해", TargetProfile, SubjectSelf},
		{"앞으로는 이렇게 해줘 — 메일 오면 요약 먼저", TargetProcedure, SubjectSelf},
		{"오늘 회의에서 포트 이야기했어", TargetEpisodic, SubjectSelf},
		{"내 주민번호는 900101-1234567", TargetExclude, SubjectSelf},
		{"아내는 해산물을 못 먹어", TargetProfile, "other:"},
	}
	for _, c := range cases {
		got := ClassifyHeuristics(c.msg)
		if got.Target != c.target {
			t.Errorf("%q: target=%s want %s", c.msg, got.Target, c.target)
		}
		if c.subj == "other:" {
			if !strings.HasPrefix(got.SubjectID, "other:") {
				t.Errorf("%q: subject=%q want other:*", c.msg, got.SubjectID)
			}
		} else if NormalizeSubject(got.SubjectID) != c.subj {
			t.Errorf("%q: subject=%q want %s", c.msg, got.SubjectID, c.subj)
		}
	}
}

func TestFactKeyFromText_CorrectionsShareSemanticAxis(t *testing.T) {
	tests := []struct {
		name string
		old  string
		new  string
		key  string
		kind string
	}{
		{
			name: "known response length axis",
			old:  "기억해줘. 나는 답변을 길고 상세하게 원해",
			new:  "앞으로 답변은 짧고 간결하게 해줘",
			key:  "communication.response_length",
			kind: "preference",
		},
		{
			name: "generic preference polarity",
			old:  "기억해줘. 나는 커피를 좋아해",
			new:  "기억해줘. 나는 커피 싫어",
			key:  "커피",
			kind: "preference",
		},
		{
			name: "generic preference natural negation",
			old:  "기억해줘. 나는 커피를 좋아해",
			new:  "기억해줘. 나는 커피를 좋아하지 않아",
			key:  "커피",
			kind: "preference",
		},
		{
			name: "generic preference short negation",
			old:  "기억해줘. 나는 커피를 좋아해",
			new:  "기억해줘. 나는 커피를 안 좋아해",
			key:  "커피",
			kind: "preference",
		},
		{
			name: "generic preference positive degree",
			old:  "기억해줘. 나는 커피를 좋아해",
			new:  "기억해줘. 나는 커피를 정말 좋아해",
			key:  "커피",
			kind: "preference",
		},
		{
			name: "generic preference negative degree",
			old:  "기억해줘. 나는 커피를 좋아해",
			new:  "기억해줘. 나는 커피를 별로 안 좋아해",
			key:  "커피",
			kind: "preference",
		},
		{
			name: "generic preference strong negation",
			old:  "기억해줘. 나는 커피를 좋아해",
			new:  "기억해줘. 나는 커피를 전혀 좋아하지 않아",
			key:  "커피",
			kind: "preference",
		},
		{
			name: "generic preference much",
			old:  "기억해줘. 나는 커피를 좋아해",
			new:  "기억해줘. 나는 커피를 많이 좋아해",
			key:  "커피",
			kind: "preference",
		},
		{
			name: "language correction natural verb",
			old:  "수정: 한국어로 말해줘",
			new:  "아니, 영어로 답해줘",
			key:  "communication.language",
			kind: "preference",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldCandidate := ClassifyHeuristics(tt.old)
			newCandidate := ClassifyHeuristics(tt.new)
			if oldCandidate.FactKey != tt.key || newCandidate.FactKey != tt.key {
				t.Fatalf("keys old=%q new=%q, want %q", oldCandidate.FactKey, newCandidate.FactKey, tt.key)
			}
			if oldCandidate.FactKind != tt.kind || newCandidate.FactKind != tt.kind {
				t.Fatalf("kinds old=%q new=%q, want %q", oldCandidate.FactKind, newCandidate.FactKind, tt.kind)
			}
		})
	}
}

func TestClassifyHeuristics_ExplicitForgetUsesFactTombstoneIntent(t *testing.T) {
	got := ClassifyHeuristics("내 답변 길이 선호는 기억에서 지워줘")
	if got.Target != TargetProfile || !got.Forget {
		t.Fatalf("candidate=%+v, want profile forget", got)
	}
	if got.FactKey != "communication.response_length" || got.FactKind != "preference" {
		t.Fatalf("key=%q kind=%q", got.FactKey, got.FactKind)
	}

	assertion := ClassifyHeuristics("기억해줘. 나는 커피를 좋아해")
	forget := ClassifyHeuristics("내 커피 취향은 잊어줘")
	if !forget.Forget || assertion.FactKey != "커피" || forget.FactKey != assertion.FactKey {
		t.Fatalf("generic assertion=%+v forget=%+v", assertion, forget)
	}

	if remember := ClassifyHeuristics("이 내용은 기억하지 마"); !remember.Forget {
		t.Fatalf("candidate=%+v, explicit do-not-remember must still tombstone", remember)
	}
}

func TestClassifyHeuristics_NegatedForgetDoesNotTombstone(t *testing.T) {
	tests := []struct {
		name string
		msg  string
	}{
		{name: "korean do not delete", msg: "내 간결한 답변 선호를 삭제하지 마"},
		{name: "korean do not delete from memory", msg: "내 답변 길이 선호는 기억에서 삭제하지 마"},
		{name: "korean do not forget", msg: "내 커피 취향은 잊지 마"},
		{name: "korean topic particle", msg: "내 간결한 답변 선호는 삭제하지는 마"},
		{name: "korean limiting particle", msg: "내 커피 취향은 잊지만 마"},
		{name: "korean coordinated verbs", msg: "내 커피 취향은 잊거나 삭제하지 마"},
		{name: "english do not delete", msg: "Do not delete my response length preference from memory"},
		{name: "english do not forget", msg: "Please don't forget my coffee preference"},
		{name: "english adverb", msg: "Please do not permanently delete my preference from memory"},
		{name: "english coordinated verbs", msg: "Please don't remove or delete my preference from memory"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyHeuristics(tt.msg); got.Forget || got.Target != TargetEpisodic {
				t.Fatalf("candidate=%+v, negated retention request must leave the fact plane untouched", got)
			}
		})
	}
}

func TestClassifyHeuristics_EnglishForgetUsesFactTombstoneIntent(t *testing.T) {
	for _, msg := range []string{
		"Delete my response length preference from memory",
		"Please forget my coffee preference",
	} {
		if got := ClassifyHeuristics(msg); got.Target != TargetProfile || !got.Forget {
			t.Errorf("%q: candidate=%+v, want profile forget", msg, got)
		}
	}
}

func TestInduceFromTurnDoesNotTreatOrdinaryDeleteTargetsAsProfileFacts(t *testing.T) {
	for _, message := range []string{
		"내 목록은 삭제해줘",
		"내 목록 정보는 삭제해줘",
		"내 진행 상황은 삭제해줘",
		"내 부가세 정보는 삭제해줘",
		"내 결론부터 정보는 삭제해줘",
	} {
		induced := InduceFromTurn(message)
		if induced == nil || induced.Route == RouteMemory || induced.Candidate.Forget {
			t.Errorf("ordinary delete target %q induced %+v, want no fact mutation", message, induced)
		}
	}
}

func TestGenericPreferenceForgetDoesNotAliasStableAxes(t *testing.T) {
	for _, tt := range []struct {
		message   string
		forbidKey string
	}{
		{message: "내 영어 공부 선호는 잊어줘", forbidKey: "communication.language"},
		{message: "내 비건 식당 선호는 잊어줘", forbidKey: "diet.vegan"},
		{message: "내 목록 정리 선호는 삭제해줘", forbidKey: "communication.format"},
		{message: "내 진행 상황 공유 선호는 잊어줘", forbidKey: "communication.progress_updates"},
		{message: "내가 비건 식당을 좋아한다는 사실을 잊어줘", forbidKey: "diet.vegan"},
		{message: "내가 목록 정리를 좋아한다는 사실을 삭제해줘", forbidKey: "communication.format"},
		{message: "내가 진행 상황 공유를 좋아한다는 사실을 잊어줘", forbidKey: "communication.progress_updates"},
	} {
		induced := InduceFromTurn(tt.message)
		if induced == nil || induced.Route != RouteMemory || !induced.Candidate.Forget {
			t.Errorf("generic preference forget %q = %+v, want an explicit generic tombstone", tt.message, induced)
			continue
		}
		if induced.Candidate.FactKey == tt.forbidKey {
			t.Errorf("generic preference forget %q aliased stable axis %q", tt.message, tt.forbidKey)
		}
	}
}

func TestInduceFromTurnRequiresDirectProfileMutationIntent(t *testing.T) {
	t.Run("payload cues stay episodic", func(t *testing.T) {
		messages := []string{
			`다음 문장을 영어로 번역해줘: "앞으로 답변은 길고 상세하게 해줘"`,
			"다음을 번역해줘: 나는 답변을 길고 상세하게 원해",
			"다음을 요약해줘:\n> 앞으로 답변은 짧고 간결하게 해줘",
			"메일 원문:\n나는 커피를 좋아해",
			"```text\n앞으로 답변은 길고 상세하게 해줘\n```",
			`"Please forget my coffee preference"`,
			"내가 받은 이메일: 앞으로 답변은 길고 상세하게 해줘",
			"my email says: Please forget my coffee preference",
			"수정할 문장: 앞으로 답변은 길고 상세하게 해줘",
			"기억해줘라는 문구를 검토해줘: 앞으로 답변은 길고 상세하게 해줘",
			"앞으로의 문장을 번역해줘: 나는 답변을 길게 원해",
			"다음부터라는 표현을 설명해줘: 앞으로 답변은 길게 해줘",
			"내 답변을 번역해줘: 앞으로 답변은 길고 상세하게 해줘",
			"내 정보를 포함한 문장을 요약해줘: 앞으로 답변은 짧게 해줘",
			"나를 포함한 문장을 번역해줘: 앞으로 나를 대장이라고 불러줘",
			"내 짧은 답변 선호 문서에서 삭제라는 표현을 검토해줘",
			`나는 "커피"를 좋아해`,
			"수정: 답변 문장을 번역해줘: 앞으로 답변은 길고 상세하게 해줘",
			"수정: 앞으로라는 표현을 설명해줘: 앞으로 답변은 짧게 해줘",
			"기억해줘. 철수는 비건이야",
			"앞으로 철수는 비건이라고 기억해줘",
			"내 기억에서 철수의 비건 정보를 지워줘",
			"Please forget Alice's response length preference",
			"내 짧은 답변 선호 문서를 삭제해줘",
			"내 선호 문서에서 커피를 지워줘",
			"Please delete my preference document",
			"Please delete the sentence about my preference",
			"Please forget my coffee preference",
			"Please forget my response length preference",
			"Please forget my friend's preference",
			"수정: 앞으로 철수는 비건이라고 기억해줘",
			"기억해줘. 나는 철수가 비건이라고 들었어",
			"기억해줘. 나는 친구 철수가 비건이라고 들었어",
			"기억해줘. 나는 철수가 비건이야",
			"기억해줘. 나는 친구 철수가 비건이야",
			"기억해줘. 철수는 내 답변을 길게 고쳐줬어",
			"기억해줘. 나는 철수를 비건이라고 알고 있어",
			"기억해줘. 나는 철수도 비건이라고 생각해",
			"기억해줘. 저는 친구를 채식주의자로 알고 있어요",
			"기억해줘. 나는 철수의 땅콩 알레르기가 심해",
			"기억해줘. 나는 철수가 우유를 못 먹어",
			"기억해줘. 나는 오늘 우유를 못 먹어",
			"기억해줘. 나는 지금 우유를 못 먹어",
			"기억해줘. 나는 당분간 우유를 못 먹어",
			"기억해줘. 나는 이번주 우유를 못 먹어",
			"기억해줘. 나는 친구 철수가 커피를 좋아한다고 생각해",
			"기억해줘. 나는 철수도 커피를 좋아한다고 생각해",
			"기억해줘. 나는 보고서를 길게 원해",
			"기억해줘. 나는 회의록을 짧게 원해",
			"기억해줘. 나는 제안서를 상세하게 원해",
			"기억해줘. 나는 철수의 답변을 길게 원해",
			"기억해줘. 나는 이 영상은 짧게 원해",
			"나는 철수를 비건이라고 기억해줘",
			"나는 철수도 비건이라고 기억해줘",
			"내 친구 철수는 비건이라고 기억해줘",
			"내 친구를 채식주의자로 기억해줘",
			"나는 보고서를 길게 원한다고 기억해줘",
			"내 회의록은 짧게 원한다고 기억해줘",
			"앞으로는 보고서를 길게 해줘",
			"앞으로는 회의록을 짧게 해줘",
			"앞으로는 철수에게만 답변을 짧게 해줘",
			"앞으로는 오늘만 답변을 짧게 해줘",
			"수정: 앞으로는 보고서를 상세하게 해줘",
			"잠깐, 앞으로는 이 영상만 짧게 해줘",
			"기억해줘. 나는 답변을 오늘만 길게 원해",
			"나는 답변을 오늘만 길게 원해, 기억해줘",
			"기억해줘. 내 호칭은 오늘만 대장",
			"기억해줘. 나를 오늘만 대장이라고 불러줘",
			"앞으로 나를 오늘만 대장이라고 불러줘",
			"기억해줘. 나는 답변을 이번 질문만 길게 원해",
			"기억해줘. 나는 답변을 다음 답변만 길게 원해",
			"기억해줘. 나는 답변을 이번 회의에서만 길게 원해",
			"기억해줘. 나는 답변을 이번에만 길게 원해",
			"기억해줘. 나는 답변을 코드 리뷰에서만 상세하게 원해",
			"기억해줘. 나는 답변을 철수에게만 짧게 원해",
			"기억해줘. 나는 답변을 이번 세션에서만 길게 원해",
			"기억해줘. 나를 회사에서만 대장이라고 불러줘",
			"앞으로 나를 이번에만 대장이라고 불러줘",
			"앞으로 나를 이번 메시지만 대장이라고 불러줘",
			"기억해줘. 나는 커피를 좋아한다고 적혀 있었어",
			"기억해줘. 나는 커피를 좋아해라고 적혀 있었어",
			"기억해줘. 나는 이번 주만 커피를 좋아해",
			"기억해줘. 나는 오늘 동안만 커피를 좋아해",
			"기억해줘. 나는 오늘 하루만 커피를 좋아해",
			"기억해줘. 나는 한 시간만 커피를 좋아해",
			"기억해줘. 나는 이번 달만 커피를 좋아해",
			"기억해줘. 나는 이번 주에만 커피를 좋아해",
			"기억해줘. 나는 이번 달에만 커피를 좋아해",
			"기억해줘. 나는 내일만 커피를 좋아해",
			"기억해줘. 나는 일주일만 커피를 좋아해",
		}
		for _, message := range messages {
			if raw := ClassifyHeuristics(message); raw.Target != TargetProfile {
				t.Fatalf("test precondition: raw classifier did not see embedded profile cue for %q: %+v", message, raw)
			}
			induced := InduceFromTurn(message)
			if induced == nil || induced.Route != RouteDiaryOnly || induced.Candidate.Forget {
				t.Errorf("framed payload %q induced %+v, want diary-only", message, induced)
			}
		}
	})

	t.Run("top level commands remain canonical", func(t *testing.T) {
		messages := []string{
			"기억해줘. 나는 비건이야",
			`기억해줘: "나는 비건이야"`,
			`내 호칭은 "대장"으로 기억해줘`,
			"수정: 앞으로 답변은 짧고 간결하게 해줘",
			"아니, 짧게 답해줘",
			"잠깐, 앞으로는 답변을 짧고 간결하게 해줘",
			"내 커피 취향은 잊어줘",
			"기억해줘. 나는 만두를 좋아해",
			"기억해줘. 나는 만년필을 좋아해",
			"기억해줘. 나는 커피를 많이 좋아해",
			"내 호칭은 만수로 기억해줘",
			"앞으로 나를 만수라고 불러줘",
			"기억해줘. 나는 우유를 못 먹어",
		}
		for _, message := range messages {
			induced := InduceFromTurn(message)
			if induced == nil || induced.Route != RouteMemory {
				t.Errorf("direct command %q induced %+v, want memory", message, induced)
			}
		}
		address := InduceFromTurn(`내 호칭은 "대장"으로 기억해줘`)
		if address == nil || address.Route != RouteMemory || address.Candidate.FactKey != "identity.address" {
			t.Fatalf("address command = %+v, want memory identity.address", address)
		}
		intolerance := InduceFromTurn("기억해줘. 나는 우유를 못 먹어")
		if intolerance == nil || intolerance.Route != RouteMemory || intolerance.Candidate.FactKey != "health.allergy" || intolerance.Candidate.FactKind != "identity" {
			t.Fatalf("intolerance command = %+v, want memory health.allergy identity", intolerance)
		}
	})

	t.Run("explicit English commands bind canonical keys", func(t *testing.T) {
		tests := []struct {
			message string
			key     string
			kind    string
		}{
			{message: "Please remember I prefer short answers", key: "communication.response_length", kind: "preference"},
			{message: "From now on, answer in English", key: "communication.language", kind: "preference"},
			{message: "Remember: I'm vegan", key: "diet.vegan", kind: "identity"},
			{message: "Remember I like coffee", key: "coffee", kind: "preference"},
			{message: "Remember I am allergic to peanuts", key: "health.allergy", kind: "identity"},
			{message: "Remember my name is Boss", key: "identity.address", kind: "identity"},
			{message: "Remember I prefer bullet lists", key: "communication.format", kind: "preference"},
			{message: "Remember I prefer markdown answers", key: "communication.format", kind: "preference"},
			{message: "Remember I prefer answers in bullet lists", key: "communication.format", kind: "preference"},
			{message: "Remember my long-term goal is founding a company", key: "goals.long_term", kind: "preference"},
			{message: "From now on, use bullet lists", key: "communication.format", kind: "preference"},
			{message: "From now on, call me Boss", key: "identity.address", kind: "identity"},
		}
		for _, tt := range tests {
			induced := InduceFromTurn(tt.message)
			if induced == nil || induced.Route != RouteMemory || induced.Candidate.FactKey != tt.key || induced.Candidate.FactKind != tt.kind {
				t.Errorf("English command %q induced %+v, want memory key=%q kind=%q", tt.message, induced, tt.key, tt.kind)
			}
		}
	})

	t.Run("English payload and temporary frames stay diary only", func(t *testing.T) {
		messages := []string{
			`Translate: "Please remember I prefer short answers"`,
			"Please remember this sentence: I like coffee",
			"Please remember my email says I like coffee",
			"Please remember I like coffee today",
			"Remember I prefer short answers for one answer only",
			"Remember I prefer short answers for the next answer only",
			"Remember I prefer short answers this week only",
			"Remember I like coffee next week only",
			"Remember I like coffee this year only",
			"Remember I like coffee for the next week only",
			"Remember I like coffee temporarily",
			"Remember I like coffee until Friday",
			"Remember I like coffee for one week only",
			"Remember I like coffee just this once",
			"Please remember my wife is vegan",
			"Remember I like coffee, according to my wife",
			"Remember I prefer short answers, my wife says",
			"Remember I like coffee, I was told",
			"Remember I like coffee, my wife told me",
			"Remember I like coffee, Alice told me",
			"Remember I like coffee, as my wife told me",
			"Remember I like coffee, I guess",
			"Remember I like coffee, I think",
			"Remember I like coffee, I believe",
			"Remember I like coffee, apparently",
			"Remember I like coffee, supposedly",
			"Remember I like coffee, maybe",
			"Remember I like coffee?",
			"From now on, answer in English for this answer only",
		}
		for _, message := range messages {
			induced := InduceFromTurn(message)
			if induced == nil || induced.Route != RouteDiaryOnly || induced.Candidate.Forget {
				t.Errorf("English framed payload %q induced %+v, want diary-only", message, induced)
			}
		}
	})

	if induced := InduceFromTurn("아내는 해산물을 못 먹어"); induced == nil || induced.Route != RouteLedger {
		t.Fatalf("third-party propose-only route changed: %+v", induced)
	}
	if induced := InduceFromTurn("나는 아내가 비건이라고 들었어"); induced == nil || induced.Route == RouteMemory || induced.Candidate.SubjectID == SubjectSelf {
		t.Fatalf("first-person third-party report became a self fact: %+v", induced)
	}
}

func TestEmbeddedSelfFactForgetBindsTheActiveCanonicalKey(t *testing.T) {
	tests := []struct {
		assertion string
		forget    string
		key       string
	}{
		{
			assertion: "기억해줘. 나는 커피를 좋아해",
			forget:    "내가 커피를 좋아한다는 사실을 기억에서 지워줘",
			key:       "커피",
		},
		{
			assertion: "기억해줘. 나는 비건이야",
			forget:    "내가 비건이라는 사실을 잊어줘",
			key:       "diet.vegan",
		},
		{
			assertion: "수정: 한국어로 말해줘",
			forget:    "내 언어 선호는 잊어줘",
			key:       "communication.language",
		},
		{
			assertion: "앞으로 나를 대장이라고 불러줘",
			forget:    "내 호칭 정보는 삭제해줘",
			key:       "identity.address",
		},
		{
			assertion: "앞으로 나를 대장이라고 불러줘",
			forget:    "내 호칭은 잊어줘",
			key:       "identity.address",
		},
		{
			assertion: "앞으로 불릿으로 답해줘",
			forget:    "내 형식 선호는 잊어줘",
			key:       "communication.format",
		},
		{
			assertion: "앞으로 부가세는 별도로 기재해줘",
			forget:    "내 부가세 별도 선호는 잊어줘",
			key:       "wiki.amount_vat_policy",
		},
		{
			assertion: "앞으로 결론부터 답해줘",
			forget:    "내 결론부터 선호는 잊어줘",
			key:       "communication.answer_first",
		},
		{
			assertion: "기억해줘. 나는 땅콩 알레르기가 있어",
			forget:    "내 알레르기 정보는 삭제해줘",
			key:       "health.allergy",
		},
		{
			assertion: "기억해줘. 나는 땅콩 알레르기가 있어",
			forget:    "내 땅콩 알레르기 정보는 삭제해줘",
			key:       "health.allergy",
		},
		{
			assertion: "기억해줘. 내 장기 목표는 창업이야",
			forget:    "내 장기 목표 정보는 삭제해줘",
			key:       "goals.long_term",
		},
		{
			assertion: "기억해줘. 나는 아이스 아메리카노를 좋아해",
			forget:    "내 아이스 아메리카노 취향은 잊어줘",
			key:       "아이스아메리카노",
		},
		{
			assertion: "기억해줘. 나는 다크 초콜릿을 좋아해",
			forget:    "내 다크 초콜릿 선호는 삭제해줘",
			key:       "다크초콜릿",
		},
		{
			assertion: "기억해줘. 나는 커피를 좋아해",
			forget:    "제가 커피를 좋아한다는 사실을 삭제해줘",
			key:       "커피",
		},
	}
	for _, tt := range tests {
		t.Run(tt.forget, func(t *testing.T) {
			assertion := ClassifyHeuristics(tt.assertion)
			forget := InduceFromTurn(tt.forget)
			if assertion.FactKey != tt.key || forget == nil || forget.Route != RouteMemory ||
				!forget.Candidate.Forget || forget.Candidate.FactKey != assertion.FactKey {
				t.Fatalf("assertion=%+v forget=%+v, want canonical key %q", assertion, forget, tt.key)
			}
		})
	}
}

func TestRouteFor_SplitsProfileProcedureAndSubjects(t *testing.T) {
	if RouteFor(TargetProfile, SubjectSelf) != RouteMemory {
		t.Fatal("self profile → memory")
	}
	if RouteFor(TargetProfile, "other:아내") != RouteLedger {
		t.Fatal("other profile → ledger, not MEMORY")
	}
	if RouteFor(TargetProcedure, SubjectSelf) != RouteLedger {
		t.Fatal("procedure → ledger")
	}
	if RouteFor(TargetExclude, SubjectSelf) != RouteDrop {
		t.Fatal("exclude → drop")
	}
}

func TestCrossSubjectBlocked(t *testing.T) {
	if CrossSubjectBlocked(SubjectSelf, nil) {
		t.Fatal("self never blocked")
	}
	if !CrossSubjectBlocked("p-pl2-001", nil) {
		t.Fatal("foreign subject blocked when query names nobody")
	}
	if CrossSubjectBlocked("p-pl2-001", []string{"p-pl2-001"}) {
		t.Fatal("named subject allowed")
	}
	if CrossSubjectBlocked("인물/김민수", []string{"김민수"}) {
		t.Fatal("path/name overlap allowed")
	}
}

func TestApply_MemoryAndLedger(t *testing.T) {
	dir := t.TempDir()
	ws := filepath.Join(dir, "ws")
	ledger := filepath.Join(dir, "data", "memory_induction.jsonl")
	fixed := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)

	profile := InduceFromTurn("기억해줘. 나는 비건이야.")
	res, err := Apply(profile, ApplyOptions{
		WorkspaceDir:    ws,
		LedgerPath:      ledger,
		SessionKey:      "client:main",
		Now:             func() time.Time { return fixed },
		MainSessionOnly: true,
	})
	if err != nil || !res.Wrote || res.Route != RouteMemory {
		t.Fatalf("profile apply: %+v err=%v", res, err)
	}
	raw, err := os.ReadFile(filepath.Join(ws, "MEMORY.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "write_target=profile") || !strings.Contains(string(raw), "비건") {
		t.Fatalf("MEMORY.md content: %s", raw)
	}

	res2, err := Apply(profile, ApplyOptions{
		WorkspaceDir:    ws,
		SessionKey:      "client:main:thread",
		MainSessionOnly: true,
		Now:             func() time.Time { return fixed },
	})
	if err != nil || res2.Wrote || res2.Dropped != "not_main_session" {
		t.Fatalf("sub-session: %+v err=%v", res2, err)
	}

	proc := InduceFromTurn("앞으로는 이렇게 해 — 주간보고는 월요일 아침에")
	res3, err := Apply(proc, ApplyOptions{
		LedgerPath: ledger,
		SessionKey: "client:main",
		Now:        func() time.Time { return fixed },
	})
	if err != nil || !res3.Wrote || res3.Route != RouteLedger {
		t.Fatalf("procedure: %+v err=%v", res3, err)
	}
	lb, err := os.ReadFile(ledger)
	if err != nil {
		t.Fatal(err)
	}
	var row map[string]string
	if err := json.Unmarshal(lb[:len(lb)-1], &row); err != nil {
		t.Fatal(err)
	}
	if row["target"] != "procedure" {
		t.Fatalf("ledger row: %#v", row)
	}
}

func TestInduceExcludeDrops(t *testing.T) {
	ind := InduceFromTurn("password: hunter2 기억해")
	if ind.Route != RouteDrop {
		t.Fatalf("route=%s", ind.Route)
	}
}
