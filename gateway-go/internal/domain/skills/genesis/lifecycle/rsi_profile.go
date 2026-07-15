// Package lifecycle defines the shared change lifecycle and per-layer policy
// profiles used by Deneb's recursive self-improvement loops.
package lifecycle

// Layer identifies one recursive self-improvement policy profile.
type Layer string

const (
	LayerL1 Layer = "L1"
	LayerL2 Layer = "L2"
	LayerL3 Layer = "L3"
	LayerL4 Layer = "L4"
	LayerL5 Layer = "L5"
)

// Stage is the common three-stage lifecycle shared by every RSI layer.
type Stage string

const (
	StageObservePropose  Stage = "observe_propose"
	StageEvaluateExecute Stage = "evaluate_execute"
	StageVerifyLearn     Stage = "verify_learn"
)

// ChangeKind names the artifact controlled by one layer profile.
type ChangeKind string

const (
	ChangeSkill      ChangeKind = "skill"
	ChangeProcedure  ChangeKind = "procedure"
	ChangeVerifier   ChangeKind = "verifier"
	ChangeSource     ChangeKind = "source"
	ChangeGovernance ChangeKind = "governance"
)

// Profile keeps stable layer identity separate from each layer's specialized
// producer, evaluator, and executor. Every profile uses the same lifecycle;
// only policy differs.
type Profile struct {
	Layer              Layer
	Title              string
	Detail             string
	ChangeKind         ChangeKind
	AutomaticExecution bool
	Frozen             bool
}

var profiles = [...]Profile{
	{
		Layer: LayerL1, Title: "스킬 진화", ChangeKind: ChangeSkill, AutomaticExecution: true,
		Detail: "저성과 스킬의 본문을 자동으로 다시 쓰고, 보류 검증과 롤백으로 회귀를 막는 기본 자가개선 루프입니다.",
	},
	{
		Layer: LayerL2, Title: "메타 진화", ChangeKind: ChangeProcedure, AutomaticExecution: true,
		Detail: "스킬을 고치는 프롬프트(생성·판정) 자체를 주간 단위로 개정하는 메타 루프입니다. 벤치를 통과하면 자동 채택되고, 드리프트가 감지되면 스스로 동결합니다.",
	},
	{
		Layer: LayerL3, Title: "판정자 공진화", ChangeKind: ChangeVerifier, AutomaticExecution: true,
		Detail: "판정자가 자신의 오판으로 학습하는 검증기 공진화 루프입니다. 판정 정확도 레인이 심은 결함을 재생해 오판 라벨을 만듭니다.",
	},
	{
		Layer: LayerL4, Title: "소스 자가편집", ChangeKind: ChangeSource, AutomaticExecution: true,
		Detail: "게이트웨이 소스 자체를 고치는 자가편집 루프입니다. 근거 있는 후보만 코딩 레인에 배차되고, 게이트 통과와 배포 롤백 워치로 보호됩니다.",
	},
	{
		Layer: LayerL5, Title: "메타 거버너", ChangeKind: ChangeGovernance, Frozen: true,
		Detail: "자가개선의 수용 정책과 안전 경계를 다루는 거버넌스 계층입니다. 검증기 보정이 입증되기 전까지 실행과 자기편집이 동결됩니다.",
	},
}

// Stages returns the one lifecycle shared by every policy profile.
func Stages() []Stage {
	return []Stage{StageObservePropose, StageEvaluateExecute, StageVerifyLearn}
}

// Profiles returns a copy ordered from the fast loop to governance.
func Profiles() []Profile {
	out := make([]Profile, len(profiles))
	copy(out, profiles[:])
	return out
}

// ProfileFor returns one policy profile by layer.
func ProfileFor(layer Layer) (Profile, bool) {
	for _, profile := range profiles {
		if profile.Layer == layer {
			return profile, true
		}
	}
	return Profile{}, false
}
