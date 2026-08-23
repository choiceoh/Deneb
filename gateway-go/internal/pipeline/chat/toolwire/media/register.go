package media

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/artifact"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire/schema"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/web"
)

// SpilloverReadTool returns the read_spillover executor for large tool results.
// ask is the optional local-model delegate behind read_spillover(question=);
// nil leaves the tool paging-only.
func SpilloverReadTool(spill tooldeps.SpilloverStore, ask tooldeps.LocalAIFunc) toolport.ToolFunc {
	if spill == nil {
		return nil
	}
	return artifact.ToolSpilloverRead(spill, ask)
}

// RegisterExtractionTools registers document/audio extraction tools over files
// on disk.
func RegisterExtractionTools(registry toolport.ToolRegistrar, asrHotwords func() string) {
	// Audio transcription: resident MOSS-Transcribe-Diarize ASR sidecar over a file on disk.
	// Deferred — capture RPCs cover app-shared audio; this is for files the
	// agent encounters itself (downloads, exec artifacts, file store).
	registry.RegisterTool(toolport.ToolDef{
		Name:        "transcribe",
		Description: "디스크의 오디오 파일(회의 녹음·음성 메모, m4a/mp3/oga/wav 등 최대 60분)을 화자분리+타임스탬프로 전사한다 — '이 녹음 정리해줘'에 사용. hotwords로 거래처·인명 교정 힌트 추가 가능(주소록/위키 힌트 자동 병합). 앱에서 공유된 오디오는 이미 자동 전사되므로 이 도구는 경로로 받은 파일용.",
		InputSchema: schema.TranscribeToolSchema(),
		Fn:          artifact.ToolTranscribe(asrHotwords),
		Deferred:    true,
	})

	// Document/image text extraction over a file on disk (PaddleOCR-VL +
	// tesseract fallback; born-digital PDFs via pdftotext). Deferred.
	registry.RegisterTool(toolport.ToolDef{
		Name:        "ocr",
		Description: "디스크의 이미지·스캔 PDF·오피스 문서에서 텍스트를 추출한다(OCR) — 영수증 사진·스캔 계약서·팩스 PDF를 읽어야 할 때 사용. read 도구는 바이너리를 그대로 덤프하므로 이미지/스캔물은 반드시 이 도구로. 파일스토어 파일은 files action=analyze로도 가능.",
		InputSchema: schema.OcrToolSchema(),
		Fn:          artifact.ToolOCR(),
		Deferred:    true,
	})
}

// RegisterMediaTools registers media tools: file delivery (send_file) and
// video watching (watch). workspaceDir bounds the watch tool's local-file
// access; an empty string restricts watch to YouTube URLs only.
func RegisterMediaTools(registry toolport.ToolRegistrar, workspaceDir string, spill tooldeps.SpilloverStore) {
	registry.RegisterTool(toolport.ToolDef{
		Name:        "send_file",
		Description: "Send a file to the user (auto-detects: photo/video/audio/document). Max 50 MB",
		InputSchema: schema.SendFileToolSchema(),
		Fn:          artifact.ToolSendFile(),
		Deferred:    true,
	})
	registry.RegisterTool(toolport.ToolDef{
		Name: "chart",
		Description: "숫자 데이터를 보기 좋은 차트 이미지(PNG)로 그린다 — 추이(line)·누적(area)·비교(bar)·구성비(doughnut). " +
			"표로 나열하기보다 한눈에 들어오는 게 나을 때(월별 추이, 거래처별 비교, 단계별 비율 등) 사용하라. " +
			"막대 위에 추세선을 얹는 콤보도 가능(한 시리즈에 type:line). " +
			"렌더된 PNG 경로를 돌려주므로, 그 경로를 send_file(type:\"photo\")로 사용자에게 전송해야 실제로 보인다.",
		InputSchema: schema.ChartToolSchema(),
		Fn:          artifact.ToolChart(),
		Deferred:    true,
	})
	registry.RegisterTool(toolport.ToolDef{
		Name: "diagram",
		Description: "구조·흐름·일정을 다이어그램 이미지(PNG)로 그린다 — 절차/관계/상태도는 flowchart(노드+화살표), 일정은 gantt(작업별 기간 막대), 연혁/이력/로드맵은 timeline(시점별 사건). " +
			"인허가 절차, 결재 흐름, 프로젝트 일정, 회사 연혁처럼 말이나 표보다 그림이 나은 걸 설명할 때 쓴다. " +
			"숫자 비교·추이는 diagram이 아니라 chart를 써라. " +
			"렌더된 PNG 경로를 돌려주므로, 그 경로를 send_file(type:\"photo\")로 사용자에게 전송해야 실제로 보인다.",
		InputSchema: schema.DiagramToolSchema(),
		Fn:          artifact.ToolDiagram(),
		Deferred:    true,
	})
	registry.RegisterTool(toolport.ToolDef{
		Name: "watch",
		Description: "YouTube 영상·로컬 영상 파일을 다루는 단일 도구 (유튜브는 web이 아니라 이 도구로 온다). " +
			"**기본은 자막 요약** (detail 생략 = transcript: 자막/전사 기반 상세 요약, 영상 다운로드 없이 가볍고 빠름 — '이 영상 리뷰/요약해줘'는 이걸로 충분). " +
			"화면을 직접 봐야 할 때만 detail=\"frames\" (프레임 추출 + 비전 분석 — 벤치마크 화면·UI·시연·화면녹화·버그 진단 등 시각 정보가 중요할 때). " +
			"start/end로 구간을 좁힐 수 있다.",
		InputSchema: schema.WatchToolSchema(),
		Fn: artifact.ToolWatch(workspaceDir, func(ctx context.Context, url string) (string, error) {
			return web.FetchYouTube(ctx, spill, url)
		}),
		Deferred: true,
	})
}
