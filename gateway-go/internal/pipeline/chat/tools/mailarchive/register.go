package mailtool

import (
	"github.com/choiceoh/deneb/gateway-go/internal/domain/knowledge"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire/schema"
)

// RegisterMailArchiveTool registers the received-mail archive reader.
func RegisterMailArchiveTool(registry toolport.ToolRegistrar, deps *tooldeps.CoreToolDeps) {
	registry.RegisterTool(toolport.ToolDef{
		Name:        "mail_archive",
		Description: "받은 메일 조회 1순위 — 메일 분석·미팅 준비·프로젝트 과거 확인에 우선 사용. 자체 메일 아카이브(자동보관 수신 메일 + 과거 백필)를 조회해 ID/Locator를 얻고, 전체 스레드와 프로젝트 히스토리를 복원한다. action=list(오늘/최근 메일) | search(키워드) | read(Locator/ID 또는 query로 원문 열기) | thread(Message-ID/References 기반 전체 대화) | project_history(회사·프로젝트 키워드 시간선+스레드 후보).",
		InputSchema: schema.MailArchiveToolSchema(),
		Fn: ToolMailArchive(MailArchiveDeps{
			Wiki:     knowledge.NewWikiAdapter(deps.Wiki.Store),
			Calendar: &deps.Calendar,
			Store:    deps.MailStore,
		}),
	})
}
