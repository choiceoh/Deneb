package secretref

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type fakeRunner struct {
	out  []byte
	err  error
	name string
	args []string
}

func (r *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.name = name
	r.args = append([]string(nil), args...)
	return r.out, r.err
}

func TestIsReference(t *testing.T) {
	if !IsReference(" op://Deneb/OpenRouter/api_key ") {
		t.Fatal("expected op:// reference to be recognized")
	}
	if IsReference("${OPENROUTER_API_KEY}") {
		t.Fatal("env var expansion should not be treated as a secret reference")
	}
}

func TestResolveRequiredReadsOnePasswordRef(t *testing.T) {
	runner := &fakeRunner{out: []byte("sk-test\n")}
	resolver := Resolver{Runner: runner, Timeout: time.Second}

	got, err := resolver.ResolveRequired(context.Background(), " op://Deneb/OpenRouter/api_key ")
	if err != nil {
		t.Fatalf("ResolveRequired: %v", err)
	}
	if got != "sk-test" {
		t.Fatalf("got %q, want sk-test", got)
	}
	if runner.name != "op" {
		t.Fatalf("command = %q, want op", runner.name)
	}
	wantArgs := []string{"read", "op://Deneb/OpenRouter/api_key"}
	if !reflect.DeepEqual(runner.args, wantArgs) {
		t.Fatalf("args = %#v, want %#v", runner.args, wantArgs)
	}
}

func TestResolveRequiredRejectsUnsupportedScheme(t *testing.T) {
	_, err := (Resolver{}).ResolveRequired(context.Background(), "env:OPENAI_API_KEY")
	if err == nil {
		t.Fatal("expected unsupported scheme error")
	}
}

func TestResolveRequiredDoesNotIncludeCommandOutputInError(t *testing.T) {
	runner := &fakeRunner{
		out: []byte("raw-secret"),
		err: errors.New("exit status 1"),
	}
	_, err := (Resolver{Runner: runner}).ResolveRequired(context.Background(), "op://Deneb/OpenRouter/api_key")
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); got == "raw-secret" {
		t.Fatal("error leaked command output")
	}
}
