package prompt

import "strings"

// presetImplementer mirrors toolpreset.PresetImplementer — the one spawn preset
// that mutates source, and so the one that gets the editing procedure below.
const presetImplementer = "implementer"

// spawnPresets mirrors toolpreset.SpawnPresets() — the presets sessions_spawn
// accepts. Copied rather than imported to keep this leaf package free of the
// pipeline dependency; spawn_preset_test.go fails if the two ever drift.
//
// A run under one of these presets reports to its PARENT agent, not to a
// client: no card renders, so the prompt hands it a reporting rule instead of
// the rich-answer grammar.
var spawnPresets = map[string]struct{}{
	"researcher":      {},
	presetImplementer: {},
	"verifier":        {},
}

func isSpawnPreset(preset string) bool {
	_, ok := spawnPresets[preset]
	return ok
}

// writeImplementerCodegraphContract writes the impact-first editing procedure
// for the implementer spawn preset.
//
// Why a procedure and not a suggestion: the failure mode this closes is not
// "the model chose grep" but "the model read whole files". A 128K window spent
// on file bodies leaves the earlier context evicted, and the model then edits
// against the loudest thing still in the window. codegraph turns that into a
// 5–8KB assembly of exactly the symbols the change touches — the effective
// capability gain comes from shrinking the problem, not from a better model.
//
// The tool names here are real for this preset: toolpreset.codegraphTools puts
// them in the allow-list and preloadedDeferred makes impact/node callable from
// turn 1. If the codegraph MCP server is not configured on the host the tools
// simply never register, and this block degrades to advice — which is why it
// names the CLI fallback too (implementer has exec).
func writeImplementerCodegraphContract(s *strings.Builder, preset string) {
	if preset != presetImplementer {
		return
	}
	s.WriteString("### 코드 수정 절차 (첫 단계 고정)\n")
	s.WriteString("소스 심볼을 편집하기 전에 **`codegraph_impact`를 최소 1회** 호출해 변경 파급 범위를 먼저 본다. 이건 권고가 아니라 순서다 — 파일을 통째로 읽어 창을 채우면 앞 맥락이 밀려나고, 남아 있는 조각에 끌려 잘못된 심볼을 고치게 된다.\n")
	s.WriteString("1. `codegraph_impact(symbol=\"…\")` — 이 심볼을 바꾸면 무엇이 깨지나. **편집 전 필수.**\n")
	s.WriteString("2. `codegraph_node(symbol=\"…\")` — 심볼의 정확한 본문·멤버. 파일 전체 `read` 대신 이걸 쓴다.\n")
	s.WriteString("3. `codegraph_callers` / `codegraph_callees` — 호출 관계(동적 디스패치 포함). 시그니처를 바꿀 때.\n")
	s.WriteString("바꿀 심볼 이름을 아직 모르면 `fetch_tools`로 `codegraph_explore`를 열어 다중 토큰으로 지형부터 잡고, 이름이 잡히면 위 순서로 돌아온다. `grep`은 리터럴 텍스트(문자열·주석·로그·설정값)에만 쓴다.\n")
	s.WriteString("도구가 보이지 않으면 `exec`로 `codegraph impact <심볼>` CLI를 쓴다. 둘 다 불가능하면 보고에 그 사실을 명시하라 — 확인하지 않은 파급 범위를 확인한 것처럼 쓰지 마라.\n")
	s.WriteString("부모에게 보고할 때 impact가 짚은 영향 심볼 중 실제로 검토한 것을 한 줄로 남겨라.\n\n")
}
