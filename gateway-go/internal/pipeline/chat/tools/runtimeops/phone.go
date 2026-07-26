// phone.go — phone_read / phone_write tools backed entirely by the native
// client's own app permissions (Termux/SSH retired, 2026-07-05).
//
// Reads come from the state the app pushes: the location sensor forwards a
// FusedLocation fix — with battery status embedded — as a `location_update`
// event, and usage rhythm arrives as `usage_update`. phone_read serves those
// caches; when stale it dispatches a `sync_state` P1 action so the app pushes
// fresh state, and tells the agent to retry shortly. Writes go through
// the same P1 action channel the Intent actions already use (SSE foreground /
// FCM data background): notify / speak / clipboard are executed in-app with
// NotificationManager / the app's TTS engine / ClipboardManager.
//
// Retired with the SSH path: clipboard READ (Android 10+ blocks background
// clipboard reads for every app — Termux included) and live contacts (the synced
// `contacts` tool already mirrors the address book). The switch keeps guidance
// cases for those values so a model holding the old schema gets a pointer
// instead of an error.
//
// Call log came back (2026-07-26) on the same push-and-cache path as usage: it
// was retired as "low value" with Termux, but who the operator spoke to and for
// how long is exactly the 인물/미팅 context this assistant reasons about, and the
// address book it already syncs is what turns a number into a person.
package runtimeops

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// ToolPhoneRead queries the phone via the app-pushed state cache:
// what = location | battery | usage | calllog | messages. send dispatches a sync_state refresh request when
// the cache is stale; nil means no app channel (report unavailable).
func ToolPhoneRead(send tooldeps.PhoneActionFunc) toolport.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			What string `json:"what"`
		}
		if err := jsonutil.UnmarshalInto("phone_read params", input, &p); err != nil {
			return "", err
		}
		switch strings.ToLower(strings.TrimSpace(p.What)) {
		case "location":
			if cached, ok := readCachedPhoneLocation(phoneLocationMaxAge); ok {
				return cached, nil
			}
			return phoneStateStaleReply(ctx, send, "위치")
		case "battery":
			if battery, ok := readCachedPhoneBattery(phoneLocationMaxAge); ok {
				return battery, nil
			}
			return phoneStateStaleReply(ctx, send, "배터리")
		case "usage", "app_usage":
			if usage, ok := readCachedPhoneUsage(phoneUsageMaxAge); ok {
				return usage, nil
			}
			return phoneStateStaleReply(ctx, send, "사용 리듬")
		case "clipboard":
			return "클립보드 읽기는 지원이 종료되었습니다 — Android 10+ 정책상 백그라운드 앱은 클립보드를 읽을 수 없습니다. 사용자가 직접 붙여넣도록 요청하세요.", nil
		case "calllog", "calls":
			if calls, ok := readCachedPhoneCallLog(phoneCallLogMaxAge); ok {
				return calls, nil
			}
			return phoneStateStaleReply(ctx, send, "통화기록")
		case "messages", "talk":
			return formatPhoneMessageThreads(readPhoneMessageThreads(time.Now(), phoneMessagesDays)), nil
		case "contacts", "addressbook":
			return "폰 주소록 라이브 조회는 지원이 종료되었습니다 — 동기화된 주소록인 `contacts` 도구를 사용하세요 (이름/회사/전화 검색 지원).", nil
		default:
			return "", fmt.Errorf("phone_read: unknown what=%q (use location|battery|usage|calllog|messages)", p.What)
		}
	}
}

// phoneStateStaleReply asks the app for a fresh state push (sync_state) and
// tells the agent how to proceed. Best-effort: a missing channel or failed
// dispatch degrades to an explanation, never an opaque error — the agent can
// still answer from conversation context.
func phoneStateStaleReply(ctx context.Context, send tooldeps.PhoneActionFunc, what string) (string, error) {
	if send == nil {
		return fmt.Sprintf("최근 %s 정보가 없습니다 (네이티브 앱 채널 미연결).", what), nil
	}
	if err := send(ctx, "sync_state", map[string]string{}); err != nil {
		return fmt.Sprintf("최근 %s 정보가 없고 갱신 요청도 실패했습니다: %v", what, err), nil
	}
	return fmt.Sprintf("최근 %s 정보가 없어 앱에 갱신을 요청했습니다. 잠시(수 초) 후 phone_read를 다시 호출하세요.", what), nil
}

// ToolPhoneWrite acts on the phone. Every operation is an in-app P1 action
// now: notify / speak / clipboard plus the Intent actions
// (open_url/open_app/share/message/dial/photo/alarm/timer). Legacy names from
// the SSH era (notification, tts) are normalized so older transcripts keep
// working.
func ToolPhoneWrite(send tooldeps.PhoneActionFunc) toolport.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p phoneWriteParams
		if err := jsonutil.UnmarshalInto("phone_write params", input, &p); err != nil {
			return "", err
		}
		p.To = normalizePhoneWriteTo(p.To)
		if !isPhoneAction(p.To) {
			return "", fmt.Errorf("phone_write: unknown to=%q (notify|speak|clipboard|reply | open_url|open_app|share|message|dial|photo|alarm|timer)", p.To)
		}
		return dispatchPhoneAction(ctx, send, p)
	}
}

// normalizePhoneWriteTo maps the SSH-era operation names onto their P1 action
// successors.
func normalizePhoneWriteTo(to string) string {
	switch strings.ToLower(strings.TrimSpace(to)) {
	case "notification":
		return "notify"
	case "tts":
		return "speak"
	default:
		return strings.ToLower(strings.TrimSpace(to))
	}
}
