package server

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/groupware"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/proactive"
)

type boardRadarTestFeed struct {
	active map[string]bool
	err    error
	calls  int
}

func (f *boardRadarTestFeed) HasActiveSourceRef(source, refID string) (bool, error) {
	f.calls++
	if f.err != nil {
		return false, f.err
	}
	return f.active[source+"|"+refID], nil
}

type boardRadarTestRunner struct {
	ready  bool
	result *chatport.SyncResult
	err    error
	calls  int
	req    chatport.SyncRequest
}

func (r *boardRadarTestRunner) ChatReady() bool { return r.ready }

func (r *boardRadarTestRunner) RunSync(_ context.Context, req chatport.SyncRequest) (*chatport.SyncResult, error) {
	r.calls++
	r.req = req
	return r.result, r.err
}

func TestGroupwareBoardRadarCallbackNoOpsForActiveRef(t *testing.T) {
	feed := &boardRadarTestFeed{active: map[string]bool{
		workfeed.SourceGroupwareBoard + "|17106": true,
	}}
	callback := groupwareBoardRadarCallback(feed, groupware.Config{}, nil, nil, nil)
	if err := callback(context.Background(), groupware.BoardSummary{PostID: "17106"}); err != nil {
		t.Fatal(err)
	}
	if feed.calls != 1 {
		t.Fatalf("feed calls = %d, want 1", feed.calls)
	}
}

func TestGroupwareBoardRadarCallbackSilentJudgmentMarksSuccessWithoutRelay(t *testing.T) {
	feed := &boardRadarTestFeed{active: map[string]bool{}}
	runner := &boardRadarTestRunner{
		ready:  true,
		result: &chatport.SyncResult{Text: " \nNO_REPLY\n "},
	}
	readCalls := 0
	relayCalls := 0
	callback := groupwareBoardRadarCallback(
		feed,
		groupware.Config{User: "reader"},
		runner,
		func(_ context.Context, cfg groupware.Config, req groupware.Request) (string, error) {
			readCalls++
			if cfg.User != "reader" || req.Area != groupware.AreaBoard ||
				req.Action != groupware.ActionRead || req.Query != "17106" {
				t.Fatalf("read cfg=%+v req=%+v", cfg, req)
			}
			return "[그룹웨어 게시판]\n\n본문\n정기 식단표입니다.", nil
		},
		func(string, string, proactive.Options) (bool, error) {
			relayCalls++
			return true, nil
		},
	)
	post := groupware.BoardSummary{
		PostID: "17106", Title: "주간 식단", Author: "총무팀", Date: "2026-07-16", CategoryID: "42",
	}
	if err := callback(context.Background(), post); err != nil {
		t.Fatal(err)
	}
	if readCalls != 1 || runner.calls != 1 || relayCalls != 0 {
		t.Fatalf("read=%d run=%d relay=%d", readCalls, runner.calls, relayCalls)
	}
	req := runner.req
	if req.SessionKey != groupwareBoardRadarSessionKey || !req.EphemeralUser ||
		!req.EphemeralAssistant || !req.SkipRecall {
		t.Fatalf("sync request isolation = %+v", req)
	}
	if req.MaxTurns == nil || *req.MaxTurns != 1 ||
		req.MaxToolCallAttempts == nil || *req.MaxToolCallAttempts != 0 {
		t.Fatalf("sync request bounds = %+v", req)
	}
	for _, want := range []string{
		"회사 운영", "정책 변경", "일정 또는 휴무", "보안", "인사(HR) 마감",
		"프로젝트 또는 실제 업무 영향", "정확히 NO_REPLY", "쓰기·댓글·수정·삭제하지 마세요",
		"제목: 주간 식단", "작성: 총무팀", "일자: 2026-07-16", "정기 식단표입니다.",
	} {
		if !strings.Contains(req.Message, want) {
			t.Fatalf("prompt missing %q:\n%s", want, req.Message)
		}
	}
}

func TestGroupwareBoardRadarCallbackRelaysSubstantiveReadOnlyCard(t *testing.T) {
	feed := &boardRadarTestFeed{active: map[string]bool{}}
	runner := &boardRadarTestRunner{
		ready:  true,
		result: &chatport.SyncResult{BestText: "## 전사 휴무 일정\n7월 31일 전사 휴무입니다."},
	}
	relayCalls := 0
	callback := groupwareBoardRadarCallback(
		feed,
		groupware.Config{},
		runner,
		func(context.Context, groupware.Config, groupware.Request) (string, error) {
			return "전사 휴무 안내", nil
		},
		func(sessionKey, content string, opts proactive.Options) (bool, error) {
			relayCalls++
			if sessionKey != "" || !strings.Contains(content, "전사 휴무") {
				t.Fatalf("relay session=%q content=%q", sessionKey, content)
			}
			if opts.WorkFeedSource != workfeed.SourceGroupwareBoard || opts.RefID != "9" ||
				opts.ForceQuestion || len(opts.Actions) != 0 || opts.MirrorTranscript {
				t.Fatalf("relay opts = %+v", opts)
			}
			feed.active[workfeed.SourceGroupwareBoard+"|9"] = true
			return true, nil
		},
	)
	post := groupware.BoardSummary{PostID: "9", Title: "전사 휴무"}
	if err := callback(context.Background(), post); err != nil {
		t.Fatal(err)
	}
	if relayCalls != 1 {
		t.Fatalf("relay calls = %d, want 1", relayCalls)
	}

	// Once the durable active RefID exists, repeated/changed polling is a no-op.
	if err := callback(context.Background(), post); err != nil {
		t.Fatal(err)
	}
	if runner.calls != 1 || relayCalls != 1 {
		t.Fatalf("active duplicate run=%d relay=%d", runner.calls, relayCalls)
	}
}

func TestGroupwareBoardRadarCallbackRetriesWhenRelayCreatesNoCard(t *testing.T) {
	feed := &boardRadarTestFeed{active: map[string]bool{}}
	runner := &boardRadarTestRunner{
		ready:  true,
		result: &chatport.SyncResult{BestText: "## 보안 정책\n즉시 비밀번호를 변경하세요."},
	}
	callback := groupwareBoardRadarCallback(
		feed,
		groupware.Config{},
		runner,
		func(context.Context, groupware.Config, groupware.Request) (string, error) {
			return "보안 정책 본문", nil
		},
		func(string, string, proactive.Options) (bool, error) {
			return true, nil
		},
	)
	err := callback(context.Background(), groupware.BoardSummary{PostID: "11"})
	if err == nil || !strings.Contains(err.Error(), "without active card") {
		t.Fatalf("error = %v, want missing-card retry", err)
	}
}

func TestGroupwareBoardRadarCallbackPropagatesReadAndJudgmentFailures(t *testing.T) {
	t.Run("read", func(t *testing.T) {
		want := errors.New("read failed")
		callback := groupwareBoardRadarCallback(
			&boardRadarTestFeed{active: map[string]bool{}},
			groupware.Config{},
			&boardRadarTestRunner{ready: true},
			func(context.Context, groupware.Config, groupware.Request) (string, error) {
				return "", want
			},
			nil,
		)
		if err := callback(context.Background(), groupware.BoardSummary{PostID: "1"}); !errors.Is(err, want) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("judgment", func(t *testing.T) {
		want := errors.New("model failed")
		callback := groupwareBoardRadarCallback(
			&boardRadarTestFeed{active: map[string]bool{}},
			groupware.Config{},
			&boardRadarTestRunner{ready: true, err: want},
			func(context.Context, groupware.Config, groupware.Request) (string, error) {
				return "body", nil
			},
			nil,
		)
		if err := callback(context.Background(), groupware.BoardSummary{PostID: "1"}); !errors.Is(err, want) {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestGroupwareBoardRadarOutputUsesExactSilentToken(t *testing.T) {
	if output, silent, err := groupwareBoardRadarOutput(&chatport.SyncResult{Text: "NO_REPLY"}); err != nil || !silent || output != "" {
		t.Fatalf("silent output=%q silent=%v err=%v", output, silent, err)
	}
	if output, silent, err := groupwareBoardRadarOutput(&chatport.SyncResult{
		BestText: "중요 요약", Text: "중요 요약\nNO_REPLY",
	}); err != nil || silent || output != "중요 요약" {
		t.Fatalf("mixed output=%q silent=%v err=%v", output, silent, err)
	}
	if _, _, err := groupwareBoardRadarOutput(&chatport.SyncResult{}); err == nil {
		t.Fatal("empty non-silent output must retry")
	}
}

func TestGroupwareBoardRadarMaxPerCycleOverride(t *testing.T) {
	t.Setenv("DENEB_GROUPWARE_BOARD_RADAR_MAX_PER_CYCLE", "5")
	if got := groupwareBoardRadarMaxPerCycle(); got != 5 {
		t.Fatalf("max = %d, want 5", got)
	}
	t.Setenv("DENEB_GROUPWARE_BOARD_RADAR_MAX_PER_CYCLE", "0")
	if got := groupwareBoardRadarMaxPerCycle(); got != groupware.DefaultBoardRadarMaxPerCycle {
		t.Fatalf("invalid max = %d, want default", got)
	}
}
