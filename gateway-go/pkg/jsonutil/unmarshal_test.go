package jsonutil

import (
	"strings"
	"testing"
)

func TestUnmarshal(t *testing.T) {
	type params struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	t.Run("valid JSON", func(t *testing.T) {
		p, err := Unmarshal[params]("test params", []byte(`{"name":"Alice","age":30}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Name != "Alice" || p.Age != 30 {
			t.Errorf("got %+v", p)
		}
	})

	t.Run("invalid JSON wraps error with context", func(t *testing.T) {
		_, err := Unmarshal[params]("test params", []byte(`{invalid`))
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "parse test params:") {
			t.Errorf("error should contain context, got: %v", err)
		}
	})

	t.Run("empty input", func(t *testing.T) {
		_, err := Unmarshal[params]("empty", nil)
		if err == nil {
			t.Fatal("expected error for nil input")
		}
	})
}

func TestUnmarshalLLM(t *testing.T) {
	type result struct {
		Answer string `json:"answer"`
	}

	t.Run("clean JSON", func(t *testing.T) {
		r, err := UnmarshalLLM[result](`{"answer": "42"}`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if r.Answer != "42" {
			t.Errorf("got %+v", r)
		}
	})

	t.Run("thinking tags + code fences", func(t *testing.T) {
		raw := "<thinking>Let me think...</thinking>\n```json\n{\"answer\": \"yes\"}\n```"
		r, err := UnmarshalLLM[result](raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if r.Answer != "yes" {
			t.Errorf("got %+v", r)
		}
	})

	t.Run("prose wrapped", func(t *testing.T) {
		raw := "Here is my answer:\n{\"answer\": \"hello\"}\nThat's it."
		r, err := UnmarshalLLM[result](raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if r.Answer != "hello" {
			t.Errorf("got %+v", r)
		}
	})

	t.Run("truncated recovery", func(t *testing.T) {
		type results struct {
			Items []struct {
				ID int `json:"id"`
			} `json:"items"`
		}
		raw := `{"items": [{"id": 1}, {"id": 2}, {"id": 3, "val`
		r, err := UnmarshalLLM[results](raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(r.Items) != 2 {
			t.Errorf("expected 2 recovered items, got %d", len(r.Items))
		}
	})

	t.Run("invalid input", func(t *testing.T) {
		_, err := UnmarshalLLM[result]("no json here at all")
		if err == nil {
			t.Fatal("expected error for non-JSON input")
		}
		if !strings.Contains(err.Error(), "unmarshal LLM output") {
			t.Errorf("error should mention LLM output, got: %v", err)
		}
	})
}
