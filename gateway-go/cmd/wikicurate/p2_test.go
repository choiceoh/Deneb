package main

import "testing"

func TestFoldMergeRemnantBody_CutsAtFirstHeading(t *testing.T) {
	body := "# 대표\n\n## 핵심 사실\n- keep\n\n## 병합된 중복 문서 (프로젝트/x/로그.md)\n\n# leftover\n"
	got, changed := foldMergeRemnantBody(body)
	if !changed {
		t.Fatal("want changed")
	}
	if got != "# 대표\n\n## 핵심 사실\n- keep\n" {
		t.Fatalf("got %q", got)
	}
	if _, again := foldMergeRemnantBody(got); again {
		t.Fatal("second fold must be a no-op")
	}
}

func TestFoldMergeRemnantBody_NoopWithoutHeading(t *testing.T) {
	body := "# 대표\n\n내용만\n"
	got, changed := foldMergeRemnantBody(body)
	if changed || got != body {
		t.Fatalf("noop failed: changed=%v got=%q", changed, got)
	}
}
