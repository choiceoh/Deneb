package groupwareops

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestToolGroupware_StatusWithoutCreds(t *testing.T) {
	t.Setenv("DENEB_GROUPWARE_USER", "")
	t.Setenv("DENEB_GROUPWARE_PASSWORD", "")
	fn := ToolGroupware(nil)
	out, err := fn(context.Background(), json.RawMessage(`{"action":"status"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "꺼짐") {
		t.Fatalf("got %q", out)
	}
}

func TestToolGroupware_RequiresAreaForList(t *testing.T) {
	t.Setenv("DENEB_GROUPWARE_USER", "alice")
	t.Setenv("DENEB_GROUPWARE_PASSWORD", "secret")
	fn := ToolGroupware(nil)
	out, err := fn(context.Background(), json.RawMessage(`{"action":"list"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "area") {
		t.Fatalf("got %q", out)
	}
}

func TestToolGroupware_ReadRequiresQuery(t *testing.T) {
	t.Setenv("DENEB_GROUPWARE_USER", "alice")
	t.Setenv("DENEB_GROUPWARE_PASSWORD", "secret")
	fn := ToolGroupware(nil)
	out, err := fn(context.Background(), json.RawMessage(`{"action":"read","area":"approval"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "query") {
		t.Fatalf("got %q", out)
	}
}

func TestToolGroupware_UnknownAction(t *testing.T) {
	fn := ToolGroupware(nil)
	_, err := fn(context.Background(), json.RawMessage(`{"action":"approve"}`))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestToolGroupware_FolderAlias(t *testing.T) {
	f, err := normalizeFolder("수신참조", "list", "approval")
	if err != nil || f != "cc" {
		t.Fatalf("got %q %v", f, err)
	}
	f, err = normalizeFolder("", "list", "approval")
	if err != nil || f != "all" {
		t.Fatalf("list default %q %v", f, err)
	}
	f, err = normalizeFolder("", "read", "approval")
	if err != nil || f != "pending" {
		t.Fatalf("read default %q %v", f, err)
	}
	f, err = normalizeFolder("전체결재문서", "list", "approval")
	if err != nil || f != "total" {
		t.Fatalf("total alias %q %v", f, err)
	}
}

func TestToolGroupware_AttachmentRequiresSelection(t *testing.T) {
	t.Setenv("DENEB_GROUPWARE_USER", "alice")
	t.Setenv("DENEB_GROUPWARE_PASSWORD", "secret")
	fn := ToolGroupware(nil)
	out, err := fn(context.Background(), json.RawMessage(`{"action":"attachment"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "doc_id") || !strings.Contains(out, "첨부") {
		t.Fatalf("got %q", out)
	}
}

func TestToolGroupware_AttachmentRejectsBoard(t *testing.T) {
	t.Setenv("DENEB_GROUPWARE_USER", "alice")
	t.Setenv("DENEB_GROUPWARE_PASSWORD", "secret")
	fn := ToolGroupware(nil)
	out, err := fn(context.Background(), json.RawMessage(`{"action":"attachment","area":"board","doc_id":"1","attachment":"1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "전자결재") {
		t.Fatalf("got %q", out)
	}
}

func TestToolGroupware_SalesFolderAlias(t *testing.T) {
	f, err := normalizeFolder("", "summary", "sales")
	if err != nil || f != "ytd" {
		t.Fatalf("default %q %v", f, err)
	}
	f, err = normalizeFolder("이번달", "summary", "sales")
	if err != nil || f != "month" {
		t.Fatalf("month %q %v", f, err)
	}
	f, err = normalizeFolder("작년", "list", "sales")
	if err != nil || f != "last_year" {
		t.Fatalf("last_year %q %v", f, err)
	}
}

func TestToolGroupware_SummaryRequiresSales(t *testing.T) {
	t.Setenv("DENEB_GROUPWARE_USER", "alice")
	t.Setenv("DENEB_GROUPWARE_PASSWORD", "secret")
	fn := ToolGroupware(nil)
	out, err := fn(context.Background(), json.RawMessage(`{"action":"summary","area":"approval"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "sales") {
		t.Fatalf("got %q", out)
	}
}

func TestToolGroupware_ErpPeriodDefaults(t *testing.T) {
	f, err := normalizeFolder("", "list", "po")
	if err != nil || f != "ytd" {
		t.Fatalf("po default %q %v", f, err)
	}
	f, err = normalizeFolder("", "list", "stock")
	if err != nil || f != "ytd" {
		t.Fatalf("stock default %q %v", f, err)
	}
	f, err = normalizeFolder("오늘", "list", "ship")
	if err != nil || f != "today" {
		t.Fatalf("ship today %q %v", f, err)
	}
	f, err = normalizeFolder("", "list", "receive")
	if err != nil || f != "month" {
		t.Fatalf("receive default %q %v", f, err)
	}
	f, err = normalizeFolder("", "list", "ship")
	if err != nil || f != "month" {
		t.Fatalf("ship default %q %v", f, err)
	}
}

func TestToolGroupware_StockAreaAlias(t *testing.T) {
	t.Setenv("DENEB_GROUPWARE_USER", "alice")
	t.Setenv("DENEB_GROUPWARE_PASSWORD", "secret")
	t.Setenv("DENEB_GROUPWARE_READER", "/nonexistent/read.mjs")
	fn := ToolGroupware(nil)
	out, err := fn(context.Background(), json.RawMessage(`{"action":"list","area":"재고","query":"모듈"}`))
	if err == nil && out == "" {
		t.Fatal("expected reader failure output")
	}
	if err == nil && !(strings.Contains(out, "실패") || strings.Contains(out, "찾지")) {
		t.Fatalf("got %q", out)
	}
}

func TestToolGroupware_PeopleRequiresQuery(t *testing.T) {
	t.Setenv("DENEB_GROUPWARE_USER", "alice")
	t.Setenv("DENEB_GROUPWARE_PASSWORD", "secret")
	fn := ToolGroupware(nil)
	out, err := fn(context.Background(), json.RawMessage(`{"action":"list","area":"사원"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "query") {
		t.Fatalf("got %q", out)
	}
}

func TestLinkPeopleWikiAndOrg_NilStoreNoPanic(t *testing.T) {
	raw := "사원 조회\n\n결과:\n1. 오선택\nDENEB_PEOPLE_JSON:[{\"name\":\"오선택\",\"dept\":\"기획조정실\",\"title\":\"전무\"}]"
	out := linkPeopleWikiAndOrg(nil, raw)
	if strings.Contains(out, "DENEB_PEOPLE_JSON") {
		t.Fatalf("marker not stripped: %q", out)
	}
	if !strings.Contains(out, "연계:") {
		t.Fatalf("missing 연계 block: %q", out)
	}
	if !strings.Contains(out, "위키: (미연결") {
		t.Fatalf("want nil-store wiki note: %q", out)
	}
	if strings.Contains(out, "rsrgNo") {
		t.Fatalf("rsrgNo leaked")
	}
}

func TestStripAndParsePeopleJSON(t *testing.T) {
	raw := "hello\nDENEB_PEOPLE_JSON:[{\"name\":\"김민준\",\"mobile\":\"010-1\"}]\n"
	body, cards := stripAndParsePeopleJSON(raw)
	if strings.Contains(body, "DENEB_PEOPLE_JSON") {
		t.Fatalf("marker remained in body")
	}
	if len(cards) != 1 || cards[0].Name != "김민준" {
		t.Fatalf("cards=%+v", cards)
	}
}
