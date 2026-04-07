package jsonutil

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestUnmarshal(t *testing.T) {
	type params struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	t.Run("valid JSON", func(t *testing.T) {
		p, err := Unmarshal[params]("test params", []byte(`{"name":"Alice","age":30}`))
		testutil.NoError(t, err)
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

func TestUnmarshalInto(t *testing.T) {
	t.Run("valid JSON", func(t *testing.T) {
		var p struct {
			Name string `json:"name"`
		}
		if err := UnmarshalInto("test params", []byte(`{"name":"Bob"}`), &p); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Name != "Bob" {
			t.Errorf("got %+v", p)
		}
	})

	t.Run("invalid JSON wraps error with context", func(t *testing.T) {
		var p struct{}
		err := UnmarshalInto("test params", []byte(`{bad`), &p)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "parse test params:") {
			t.Errorf("error should contain context, got: %v", err)
		}
	})
}

func TestStripTrailingCommas(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "trailing comma in object",
			input: `{"a": 1, "b": 2,}`,
			want:  `{"a": 1, "b": 2}`,
		},
		{
			name:  "trailing comma in array",
			input: `[1, 2, 3,]`,
			want:  `[1, 2, 3]`,
		},
		{
			name:  "nested trailing commas",
			input: `{"a": [1, 2,], "b": {"c": 3,},}`,
			want:  `{"a": [1, 2], "b": {"c": 3}}`,
		},
		{
			name:  "trailing comma with whitespace",
			input: "{\"a\": 1 ,\n}",
			want:  "{\"a\": 1 \n}",
		},
		{
			name:  "comma in string preserved",
			input: `{"msg": "hello, world,", "ok": true}`,
			want:  `{"msg": "hello, world,", "ok": true}`,
		},
		{
			name:  "no commas",
			input: `{"a": 1}`,
			want:  `{"a": 1}`,
		},
		{
			name:  "empty object",
			input: `{}`,
			want:  `{}`,
		},
		{
			name:  "comma before value not stripped",
			input: `{"a": 1, "b": 2}`,
			want:  `{"a": 1, "b": 2}`,
		},
		{
			name:  "string with escaped quote and comma",
			input: `{"msg": "say \"hi,\"",}`,
			want:  `{"msg": "say \"hi,\""}`,
		},
		{
			name:  "LLM facts with trailing comma",
			input: `{"facts": [{"content": "사용자가 Go를 선호", "importance": 0.8,},]}`,
			want:  `{"facts": [{"content": "사용자가 Go를 선호", "importance": 0.8}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripTrailingCommas(tt.input)
			if got != tt.want {
				t.Errorf("StripTrailingCommas()\n got: %s\nwant: %s", got, tt.want)
			}
			// Verify the result is valid JSON if the expected output is valid.
			if tt.want != "" {
				var v any
				if err := json.Unmarshal([]byte(tt.want), &v); err == nil {
					// Expected is valid JSON, so result must also be valid.
					if err := json.Unmarshal([]byte(got), &v); err != nil {
						t.Errorf("StripTrailingCommas() result is not valid JSON: %v", err)
					}
				}
			}
		})
	}
}

func TestUnmarshalLLM(t *testing.T) {
	type result struct {
		Answer string `json:"answer"`
	}

	t.Run("clean JSON", func(t *testing.T) {
		r, err := UnmarshalLLM[result](`{"answer": "42"}`)
		testutil.NoError(t, err)
		if r.Answer != "42" {
			t.Errorf("got %+v", r)
		}
	})

	t.Run("thinking tags + code fences", func(t *testing.T) {
		raw := "<thinking>Let me think...</thinking>\n```json\n{\"answer\": \"yes\"}\n```"
		r, err := UnmarshalLLM[result](raw)
		testutil.NoError(t, err)
		if r.Answer != "yes" {
			t.Errorf("got %+v", r)
		}
	})

	t.Run("prose wrapped", func(t *testing.T) {
		raw := "Here is my answer:\n{\"answer\": \"hello\"}\nThat's it."
		r, err := UnmarshalLLM[result](raw)
		testutil.NoError(t, err)
		if r.Answer != "hello" {
			t.Errorf("got %+v", r)
		}
	})

	t.Run("trailing comma auto-fix", func(t *testing.T) {
		raw := `{"answer": "yes",}`
		r, err := UnmarshalLLM[result](raw)
		testutil.NoError(t, err)
		if r.Answer != "yes" {
			t.Errorf("got %+v", r)
		}
	})

	t.Run("LLM facts with trailing commas", func(t *testing.T) {
		type facts struct {
			Facts []struct {
				Content    string  `json:"content"`
				Importance float64 `json:"importance"`
			} `json:"facts"`
		}
		raw := `<thinking>let me analyze</thinking>
{"facts": [{"content": "사용자가 Go를 선호", "importance": 0.8,}, {"content": "DGX Spark 사용", "importance": 0.7,},]}`
		r, err := UnmarshalLLM[facts](raw)
		testutil.NoError(t, err)
		if len(r.Facts) != 2 {
			t.Errorf("expected 2 facts, got %d", len(r.Facts))
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
		testutil.NoError(t, err)
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
