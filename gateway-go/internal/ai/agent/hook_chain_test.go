package agent

import (
	"reflect"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// runChain builds the chain, invokes the composed hook once, and returns the
// order the registered hooks ran in (each hook records its label).
func runChain(t *testing.T, build func(c *BeforeAPICallChain, rec func(string) func([]llm.Message) []llm.Message)) []string {
	t.Helper()
	var order []string
	rec := func(label string) func([]llm.Message) []llm.Message {
		return func(m []llm.Message) []llm.Message {
			order = append(order, label)
			return m
		}
	}
	var c BeforeAPICallChain
	build(&c, rec)
	fn := c.Build(nil)
	if fn != nil {
		fn(nil)
	}
	return order
}

func TestBeforeAPICallChain_StageOrder(t *testing.T) {
	order := runChain(t, func(c *BeforeAPICallChain, rec func(string) func([]llm.Message) []llm.Message) {
		// Register out of run order; stage must drive execution order.
		c.Add("post", HookStagePost, rec("post"))
		c.Add("pre", HookStagePre, rec("pre"))
		c.Add("normal", HookStageNormal, rec("normal"))
	})
	if want := []string{"pre", "normal", "post"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("stage order = %v, want %v", order, want)
	}
}

func TestBeforeAPICallChain_StableWithinStage(t *testing.T) {
	order := runChain(t, func(c *BeforeAPICallChain, rec func(string) func([]llm.Message) []llm.Message) {
		c.Add("a", HookStageNormal, rec("a"))
		c.Add("b", HookStageNormal, rec("b"))
		c.Add("c", HookStageNormal, rec("c"))
	})
	if want := []string{"a", "b", "c"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("within-stage order = %v, want registration order %v", order, want)
	}
}

func TestBeforeAPICallChain_AfterReorders(t *testing.T) {
	order := runChain(t, func(c *BeforeAPICallChain, rec func(string) func([]llm.Message) []llm.Message) {
		// b registered first but declares it must run after a.
		c.Add("b", HookStageNormal, rec("b"), "a")
		c.Add("a", HookStageNormal, rec("a"))
	})
	if want := []string{"a", "b"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("after order = %v, want %v", order, want)
	}
}

func TestBeforeAPICallChain_SingletonFirstWins(t *testing.T) {
	order := runChain(t, func(c *BeforeAPICallChain, rec func(string) func([]llm.Message) []llm.Message) {
		c.Add("dup", HookStageNormal, rec("first"))
		c.Add("dup", HookStageNormal, rec("second")) // same name → rejected
	})
	if want := []string{"first"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("singleton conflict order = %v, want only the first %v", order, want)
	}
}

func TestBeforeAPICallChain_NilHookSkipped(t *testing.T) {
	order := runChain(t, func(c *BeforeAPICallChain, rec func(string) func([]llm.Message) []llm.Message) {
		c.Add("steer", HookStageNormal, rec("steer"))
		c.Add("trailing-cache", HookStagePost, nil) // disabled feature
	})
	if want := []string{"steer"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("nil-skip order = %v, want %v", order, want)
	}
}

func TestBeforeAPICallChain_PreservesCacheAssemblyOrder(t *testing.T) {
	// Mirrors the run_exec.go assembly: steer (NORMAL) then the trailing
	// prompt-cache hook (POST), which must always run last.
	order := runChain(t, func(c *BeforeAPICallChain, rec func(string) func([]llm.Message) []llm.Message) {
		c.Add("trailing-cache", HookStagePost, rec("trailing"))
		c.Add("steer", HookStageNormal, rec("steer"))
	})
	if want := []string{"steer", "trailing"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("cache assembly order = %v, want %v", order, want)
	}
}

func TestBeforeAPICallChain_EmptyBuildsNil(t *testing.T) {
	var c BeforeAPICallChain
	if fn := c.Build(nil); fn != nil {
		t.Fatalf("empty chain Build = non-nil, want nil")
	}
}
