package domain

import (
	"github.com/choiceoh/deneb/gateway-go/internal/domain/knowledge"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/groupwareops"
	mailtool "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/mailarchive"
	notebooktool "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/notebook"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/orgops"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/peopleops"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/personaops"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/skilltool"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/wikitool"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire/schema"
)

// Register wires domain tools that RegisterCoreTools owns (mail + skills deferred to chat wrapper).
func Register(registry toolport.ToolRegistrar, deps *tooldeps.CoreToolDeps) {
	RegisterMailArchiveTool(registry, deps)
}

// RegisterPeopleTool registers the unified person-lookup tool. It replaces the
// separate `contacts` and `org` tools (2026-08-29 audit): three stores answered
// person questions — the synced address book, the curated org.json tree, and
// Amaranth's live HR area — and the model had to guess which one held the
// answer before it could ask. `people` fans out instead, so one call covers all
// three and the caller never routes.
//
// Always registered, unlike the address-book tool it absorbed. That tool was
// gated on a wired contacts store, but the org chart never was — so gating the
// merged tool the same way would leave a gateway without a synced address book
// (a fresh install, or the dev instance) with no person surface at all. The
// facade degrades per source instead: a nil contacts leg drops its section and
// answers the two address-book-only actions with a clear reason.
//
// Eager, unlike the two deferred tools it replaces. Live check on the first
// deferred build: asked "김성훈 씨 연락처랑 어느 팀인지", the model never
// fetched people at all — it reached for eager `groupware` (which owns HR as
// one of nine areas), then code_action, and burned five turns. A person tool
// that has to be fetched loses every race against an eager tool that can half
// answer, so the facade only pays off on the wire. It is cheap enough to sit
// there: ~460 schema bytes, among the smallest eager tools.
//
// ASR hotword injection and wiki person enrichment read the store server-side
// and are unaffected either way.
func RegisterPeopleTool(registry toolport.ToolRegistrar, contactsDeps *tooldeps.ContactsDeps, wikiStore *wiki.Store) {
	var contacts toolport.ToolFunc
	if contactsDeps.Store != nil {
		contacts = wikitool.ToolContacts(contactsDeps)
	}
	registry.RegisterTool(toolport.ToolDef{
		Name: "people",
		Description: "사람 조회 한 입구 — '이 번호 누구?'·'OOO 연락처'·'1팀 팀장 누구'·'김○○ 부서/휴대폰'은 짐작 말고 호출. " +
			"find(기본)는 주소록·조직도·그룹웨어를 한 번에 훑어 세 출처를 함께 돌려준다 " +
			"(각각 번호·회사 / 조직 위치·직급 / 라이브 부서·휴대폰). " +
			"phone=번호→사람, company=회사 소속 전원, tree=조직도.",
		InputSchema: schema.PeopleToolSchema(),
		Fn: peopleops.ToolPeople(peopleops.Sources{
			Contacts:  contacts,
			Org:       orgops.ToolOrg(),
			Groupware: groupwareops.ToolGroupware(wikiStore),
		}),
	})
}

// RegisterWikiTools registers wiki knowledge base tools for long-term knowledge
// access (search, read, write, log). Project-specific tools provide structured
// access to the "프로젝트" wiki category.
func RegisterWikiTools(registry toolport.ToolRegistrar, wikiDeps *tooldeps.WikiDeps, workspaceDir string, sessionCacheFlush wikitool.SessionCacheFlushFn) {
	// Wiki: unified knowledge base tool (search, read, write, log, daily, index, status).
	if wikiDeps.Store != nil {
		registry.RegisterTool(toolport.ToolDef{
			Name:        "wiki",
			Description: "LLM 위키 지식베이스: search (검색), read (페이지 읽기), index (목차), write (작성/수정), log (일지), daily (최근 일지), status (통계). 과거 결정/맥락/인물/프로젝트 등 장기 지식을 마크다운 위키로 관리. write 시 related/[[wikilink]]로 연결하고, 새 사실이 기존 페이지를 대체하면 supersedes로 stale 페이지를 표시한다. 본문에서 인물을 [[이름]]으로 링크하면 주소록에 있는 사람은 인물 페이지가 자동 생성·연락처 기록된다(인물 페이지를 직접 쓰면 그 사람 연락처도 자동 채워짐). ★프로젝트 문서 구조(고정): 프로젝트/<이름>/대표.md(대표페이지)·로그.md(진행 로그)·기자재/(자재 문서)·메일분석/(자동 생성). 사건·회의·결재 소식은 새 페이지를 만들지 말고 해당 프로젝트 로그.md에 날짜와 함께 append하고, 항상 write 전에 search로 기존 문서를 확인한다. 끝난 프로젝트는 사용자가 요청하면 close로 종결(보관+활성 목록 제외, 삭제 아님), reopen으로 재개",
			InputSchema: schema.WikiToolSchema(),
			Fn:          wikitool.ToolWiki(wikiDeps, workspaceDir),
		})

		// deal_ledger: deterministic list/sum over the typed deal-record ledger
		// (wiki/deal_records.go) — 합계·건수·기간 질문을 모델 눈대중 대신 코드
		// 계산으로. The ledger itself is teed on every UpsertDealPage filing.
		// Deferred (2026-07-09): niche direct use, and code_action's bridge already
		// exposes it zero-hop as "deals", so deferring strands nothing — a direct
		// 거래 집계 turn fetches it. Description leads with the trigger phrases.
		registry.RegisterTool(toolport.ToolDef{
			Name:        "deal_ledger",
			Description: "'총 거래액'·'올해 견적 몇 건'·'거래처별 합계'처럼 **합산**이 필요한 거래 질문에 쓰는 정형 거래 원장 (단건 조회 — '그 견적 얼마였지'·'이 거래처 단가' — 는 counterparty/project 필터로 list하면 그 건의 금액·일자·조건이 나온다) — 메일 분석이 파일한 거래 문서(견적·계약·세금계산서 등)의 타입드 기록에서 합계·건수·통화별 집계·기간 필터를 코드로 계산한다(위키 산문 눈대중 금지). 금액 미파싱 건은 합계에서 제외되고 원문과 함께 표기된다",
			InputSchema: schema.DealLedgerToolSchema(),
			Fn:          wikitool.ToolDealLedger(wikiDeps.Store),
			Deferred:    true,
		})

		// wiki_forget: standalone HARD delete of a page (privacy/correctness).
		// Separate tool (not a wiki action) so it stays out of the autonomous
		// background presets' name-based allow-lists — a destructive delete must
		// not be reachable from untrusted-content turns. Deferred: rare + destructive.
		registry.RegisterTool(toolport.ToolDef{
			Name: "wiki_forget",
			Description: "위키 페이지를 영구 삭제 — \"잊어버려\"·\"잊어줘\"·\"그 페이지 지워줘\". 오정보·프라이버시로 사실을 지운다. path=페이지 경로, reason=사유(감사 로그 기록). " +
				"close(아카이브)·supersedes(소프트 강등)와 달리 실제 제거해 검색·회상에서 사라진다. 파괴적이므로 먼저 wiki search로 정확한 경로를 확인하라. " +
				"거래 원장 페이지(프로젝트/거래/…)는 재무 감사 기록이라 거부된다.",
			InputSchema: schema.WikiForgetToolSchema(),
			Fn:          wikitool.ToolWikiForget(wikiDeps, sessionCacheFlush),
			Deferred:    true,
		})
	}
}

// RegisterPersonaTools registers the `preference` tool: an append-only path for
// the agent to persist a durable standing preference / behavior rule into the
// workspace SOUL.md. Append-only by contract (the agent can add but never
// delete a rule — only the human operator rewrites SOUL.md), so the agent
// cannot quietly erase its own standing constraints. Deferred so it stays out
// of the eager prompt (persisting a preference is a deliberate, occasional act).
func RegisterPersonaTools(registry toolport.ToolRegistrar, workspaceDir string) {
	registry.RegisterTool(toolport.ToolDef{
		Name: "preference",
		Description: "사용자의 서 있는 선호·행동 규칙을 SOUL.md(페르소나)에 영구 저장한다 (append-only) — \"앞으로 이렇게 해줘\"·\"계속 지켜\"·\"기억해서 항상\"·\"다음부터는\". " +
			"사용자가 '앞으로는 …해줘/…하지 마'처럼 지속 적용될 행동 방침을 말하면 이걸로 rule 한 줄을 남긴다. " +
			"추가만 가능하고 삭제·수정은 사용자만 SOUL.md 편집으로 할 수 있다 — 에이전트가 자기 규칙을 지우지 못하게 하는 의도적 비대칭. " +
			"반영은 다음 세션부터. 일회성 사실은 wiki, 사용자 개인정보는 wiki 사용자 카테고리를 쓰고, 이건 '어떻게 행동할지'에만 쓴다.",
		InputSchema: schema.PreferenceToolSchema(),
		Fn:          personaops.ToolPersonaPref(workspaceDir),
		Deferred:    true,
	})
}

// RegisterNotebookTool registers the notebook tool — NotebookLM-style scoped
// source collections for grounded, cited synthesis (딜/프로젝트 브리핑). Skipped
// when the notebook store is unavailable.
func RegisterNotebookTool(registry toolport.ToolRegistrar, deps *tooldeps.NotebookDeps) {
	if deps == nil || deps.Store == nil {
		return
	}
	// Deferred (2026-07-09): the notebook is a deliberate multi-step workflow
	// (create → add_source → brief) needed only when the user explicitly asks for
	// a grounded/cited briefing over pinned sources — rare per interactive turn,
	// yet its schema was the 4th-largest eager tool (~2.6KB). Not on code_action's
	// bridge and not named by any autonomous trigger, so a notebook turn fetches
	// it. Description front-loads the WHEN so the 80-rune deferred summary is useful.
	registry.RegisterTool(toolport.ToolDef{
		Name:        "notebook",
		Description: "거래처·프로젝트별 딜 증거 묶음(노트북) — \"선킨 견적 이력\"·\"이 거래처 계약 조건\"·\"이 자료들로 노트북 만들어줘\"·\"이 문서들만 근거로 브리핑\". 메일 분석이 거래처마다 견적·계약·세금계산서 증거를 자동으로 핀해 둔다(금액·일자·품목·납기가 추출된 상태). 그 자료만으로 출처 추적 가능한 인용 브리핑을 만드는 NotebookLM식 묶음. action=create (노트북 생성) | list (목록) | show (자료 보기) | add_source (자료 핀: kind=wiki 위키페이지 또는 kind=note 붙여넣기 텍스트) | remove_source (자료 제거) | delete (노트북 삭제) | brief (핀된 자료에만 근거해 [S1] 형식 인용 브리핑 생성).",
		InputSchema: schema.NotebookToolSchema(),
		Fn:          notebooktool.ToolNotebook(deps),
		Deferred:    true,
	})
}

// RegisterSkillsTools registers the unified skills tool
// (list/create/patch/delete/read/list_files/write_file/remove_file).
func RegisterSkillsTools(registry toolport.ToolRegistrar, getSnapshot skilltool.SkillsSnapshotProvider, workspaceDir, bundledSkillsDir string, invalidateCache skilltool.SkillManageInvalidateFn) {
	registry.RegisterTool(toolport.ToolDef{
		Name: "skills",
		Description: "Skill management: list (browse/search), create, patch, read, delete, list_files, write_file, remove_file. " +
			"Use list when the current task might match a skill. Create reusable workflows from complex tasks.",
		InputSchema: schema.SkillsToolSchema(),
		Fn:          skilltool.ToolSkills(getSnapshot, workspaceDir, bundledSkillsDir, invalidateCache),
		Deferred:    true,
	})
}

// RegisterMailArchiveTool registers the received-mail archive reader.
func RegisterMailArchiveTool(registry toolport.ToolRegistrar, deps *tooldeps.CoreToolDeps) {
	// Mail archive reader — the received-mail hand. Eager (2026-07-09): received
	// mail is the most common 업무 surface (analysis, meeting prep, project
	// history), so it earns its schema every turn. Left deferred, the model routed
	// mail reads through code_action's eager bridge (deneb.mail_archive) to dodge
	// the fetch_tools hop — overusing code_action even for plain reads and
	// dead-ending on attachments (the attachment action is off the bridge allowlist).
	// Reads the on-box deneb-mailarchive store over loopback IMAP.
	registry.RegisterTool(toolport.ToolDef{
		Name:        "mail_archive",
		Description: "받은 메일 조회 1순위 — 메일 분석·미팅 준비·프로젝트 과거 확인에 우선 사용. 자체 메일 아카이브(자동보관 수신 메일 + 과거 백필)를 조회해 ID/Locator를 얻고, 전체 스레드와 프로젝트 히스토리를 복원한다. action=list(오늘/최근 메일) | search(키워드) | read(Locator/ID 또는 query로 원문 열기) | thread(Message-ID/References 기반 전체 대화) | project_history(회사·프로젝트 키워드 시간선+스레드 후보).",
		InputSchema: schema.MailArchiveToolSchema(),
		Fn: mailtool.ToolMailArchive(mailtool.MailArchiveDeps{
			Wiki:     knowledge.NewWikiAdapter(deps.Wiki.Store),
			Calendar: &deps.Calendar,
			Store:    deps.MailStore,
		}),
	})
}

// SkillsSnapshotProvider aliases the skilltool callback so the toolwire facade
// can re-export RegisterSkillsTools without importing tools/skilltool.
type SkillsSnapshotProvider = skilltool.SkillsSnapshotProvider

// SkillManageInvalidateFn aliases the skilltool invalidation callback.
type SkillManageInvalidateFn = skilltool.SkillManageInvalidateFn

// ToolMaxOutputs returns per-tool output character budgets from tool_schemas.json.
func ToolMaxOutputs() map[string]int { return schema.ToolMaxOutputs() }
