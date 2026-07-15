package web

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestWebSearchFallsThroughSerperFailureToBrave(t *testing.T) {
	t.Setenv("SERPER_API_KEY", "serper-test")
	t.Setenv("BRAVE_SEARCH_API_KEY", "brave-test")
	t.Setenv("BRAVE_API_KEY", "")

	origSerper := serperSearchRawFn
	origBrave := braveSearchRawFn
	origDDG := duckDuckGoSearchFn
	t.Cleanup(func() {
		serperSearchRawFn = origSerper
		braveSearchRawFn = origBrave
		duckDuckGoSearchFn = origDDG
	})

	serperSearchRawFn = func(context.Context, string, string, int) ([]searchResult, serperAnswerBox, error) {
		return nil, serperAnswerBox{}, errors.New("serper HTTP 500")
	}
	braveSearchRawFn = func(context.Context, string, string, int) ([]searchResult, error) {
		return []searchResult{{Title: "Brave Hit", URL: "https://brave.example/a", Description: "ok"}}, nil
	}
	duckDuckGoSearchFn = func(context.Context, string) (string, error) {
		t.Fatal("duckduckgo should not run when Brave succeeds")
		return "", nil
	}

	out, err := webSearch(context.Background(), "q", 3)
	if err != nil {
		t.Fatalf("webSearch: %v", err)
	}
	if !strings.Contains(out, "Brave Hit") || !strings.Contains(out, "https://brave.example/a") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

func TestWebSearchWithURLsFallsThroughSerperFailureToBrave(t *testing.T) {
	t.Setenv("SERPER_API_KEY", "serper-test")
	t.Setenv("BRAVE_SEARCH_API_KEY", "brave-test")

	origSerper := serperSearchRawFn
	origBrave := braveSearchRawFn
	t.Cleanup(func() {
		serperSearchRawFn = origSerper
		braveSearchRawFn = origBrave
	})

	serperSearchRawFn = func(context.Context, string, string, int) ([]searchResult, serperAnswerBox, error) {
		return nil, serperAnswerBox{}, errors.New("serper down")
	}
	braveSearchRawFn = func(context.Context, string, string, int) ([]searchResult, error) {
		return []searchResult{{Title: "B", URL: "https://b.example/", Description: "d"}}, nil
	}

	out, urls, err := webSearchWithURLs(context.Background(), "q", 2)
	if err != nil {
		t.Fatalf("webSearchWithURLs: %v", err)
	}
	if len(urls) != 1 || urls[0] != "https://b.example/" {
		t.Fatalf("urls=%v", urls)
	}
	if !strings.Contains(out, "B") || !strings.Contains(out, "https://b.example/") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

func TestWebSearchFallsThroughToDuckDuckGoWhenKeyedProvidersFail(t *testing.T) {
	t.Setenv("SERPER_API_KEY", "serper-test")
	t.Setenv("BRAVE_SEARCH_API_KEY", "brave-test")

	origSerper := serperSearchRawFn
	origBrave := braveSearchRawFn
	origDDG := duckDuckGoSearchFn
	t.Cleanup(func() {
		serperSearchRawFn = origSerper
		braveSearchRawFn = origBrave
		duckDuckGoSearchFn = origDDG
	})

	serperSearchRawFn = func(context.Context, string, string, int) ([]searchResult, serperAnswerBox, error) {
		return nil, serperAnswerBox{}, errors.New("serper fail")
	}
	braveSearchRawFn = func(context.Context, string, string, int) ([]searchResult, error) {
		return nil, errors.New("brave fail")
	}
	duckDuckGoSearchFn = func(context.Context, string) (string, error) {
		return "ddg-ok", nil
	}

	out, err := webSearch(context.Background(), "q", 3)
	if err != nil {
		t.Fatalf("webSearch: %v", err)
	}
	if out != "ddg-ok" {
		t.Fatalf("got %q", out)
	}
}
