package runtimeops

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestToolGroupware_StatusWithoutCreds(t *testing.T) {
	t.Setenv("DENEB_GROUPWARE_USER", "")
	t.Setenv("DENEB_GROUPWARE_PASSWORD", "")
	fn := ToolGroupware()
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
	fn := ToolGroupware()
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
	fn := ToolGroupware()
	out, err := fn(context.Background(), json.RawMessage(`{"action":"read","area":"approval"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "query") {
		t.Fatalf("got %q", out)
	}
}

func TestToolGroupware_UnknownAction(t *testing.T) {
	fn := ToolGroupware()
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
	fn := ToolGroupware()
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
	fn := ToolGroupware()
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
	fn := ToolGroupware()
	out, err := fn(context.Background(), json.RawMessage(`{"action":"summary","area":"approval"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "sales") {
		t.Fatalf("got %q", out)
	}
}
