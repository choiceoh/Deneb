package telegram

import (
	"context"
	"testing"
)

func TestInitDataContext_RoundTrip(t *testing.T) {
	want := &InitData{QueryID: "AAH1", User: &WebAppUser{ID: 42, Username: "peter"}}
	ctx := WithInitDataContext(context.Background(), want)
	got := InitDataFromContext(ctx)
	if got != want {
		t.Fatalf("InitDataFromContext returned %v, want %v", got, want)
	}
}

func TestInitDataContext_NilData(t *testing.T) {
	ctx := WithInitDataContext(context.Background(), nil)
	if got := InitDataFromContext(ctx); got != nil {
		t.Fatalf("InitDataFromContext on nil-store returned %v, want nil", got)
	}
}

func TestInitDataContext_BackgroundContext(t *testing.T) {
	if got := InitDataFromContext(context.Background()); got != nil {
		t.Fatalf("InitDataFromContext on bare Background returned %v, want nil", got)
	}
}
