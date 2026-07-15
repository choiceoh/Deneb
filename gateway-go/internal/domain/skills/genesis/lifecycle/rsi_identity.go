// Package lifecycle defines L4 review/delivery state and the stable display
// identity of Deneb's operational recursive self-improvement layers.
package lifecycle

// Layer identifies one operational recursive self-improvement loop.
type Layer string

const (
	LayerL1 Layer = "L1"
	LayerL2 Layer = "L2"
	LayerL3 Layer = "L3"
	LayerL4 Layer = "L4"
)

// Identity contains only stable layer presentation. Each layer owns its
// specialized execution policy; this is not an orchestrator.
type Identity struct {
	Layer  Layer
	Title  string
	Detail string
}

var identities = [...]Identity{
	{
		Layer: LayerL1, Title: "스킬 진화",
		Detail: "저성과 스킬의 본문을 자동으로 다시 쓰고, 보류 검증과 롤백으로 회귀를 막는 기본 자가개선 루프입니다.",
	},
	{
		Layer: LayerL2, Title: "메타 진화",
		Detail: "스킬을 고치는 프롬프트(생성·판정) 자체를 주간 단위로 개정하는 메타 루프입니다. 벤치를 통과하면 자동 채택되고, 드리프트가 감지되면 스스로 동결합니다.",
	},
	{
		Layer: LayerL3, Title: "판정자 공진화",
		Detail: "판정자가 자신의 오판으로 학습하는 검증기 공진화 루프입니다. 판정 정확도 레인이 심은 결함을 재생해 오판 라벨을 만듭니다.",
	},
	{
		Layer: LayerL4, Title: "소스 자가편집",
		Detail: "게이트웨이 소스 자체를 고치는 자가편집 루프입니다. 근거 있는 후보만 코딩 레인에 배차되고, 게이트 통과와 배포 롤백 워치로 보호됩니다.",
	},
}

// IdentityFor returns one operational layer's display identity.
func IdentityFor(layer Layer) (Identity, bool) {
	for _, identity := range identities {
		if identity.Layer == layer {
			return identity, true
		}
	}
	return Identity{}, false
}
