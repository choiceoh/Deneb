package runstate

import (
	"fmt"
	"sync"
	"testing"
)

func TestPendingQueueDrainReturnsLatestIntent(t *testing.T) {
	queue := NewPendingQueue()
	queue.Enqueue("client:main", Params{SessionKey: "client:main", Message: "first"})
	queue.Enqueue("client:main", Params{SessionKey: "client:main", Message: "latest"})

	if got := queue.Len("client:main"); got != 1 {
		t.Fatalf("Len = %d, want 1 latest intent", got)
	}
	got := queue.Drain("client:main")
	if got == nil || got.Message != "latest" {
		t.Fatalf("Drain = %#v, want latest message", got)
	}
	if again := queue.Drain("client:main"); again != nil {
		t.Fatalf("second Drain = %#v, want nil", again)
	}
}

func TestPendingQueueWithMultipleSessionsReturnsIndependentMessages(t *testing.T) {
	queue := NewPendingQueue()
	queue.Enqueue("client:a", Params{SessionKey: "client:a", Message: "A"})
	queue.Enqueue("client:b", Params{SessionKey: "client:b", Message: "B"})

	if got := queue.Drain("client:b"); got == nil || got.Message != "B" {
		t.Fatalf("Drain(client:b) = %#v", got)
	}
	if got := queue.Drain("client:a"); got == nil || got.Message != "A" {
		t.Fatalf("Drain(client:a) = %#v", got)
	}
}

func TestPendingQueueClearAndReset(t *testing.T) {
	queue := NewPendingQueue()
	queue.Enqueue("client:a", Params{Message: "A"})
	queue.Enqueue("client:b", Params{Message: "B"})

	queue.Clear("client:a")
	if queue.Drain("client:a") != nil {
		t.Fatal("Clear left client:a queued")
	}
	if got := queue.Len("client:b"); got != 1 {
		t.Fatalf("Clear affected client:b: Len = %d", got)
	}

	queue.Reset()
	if queue.Drain("client:b") != nil {
		t.Fatal("Reset left client:b queued")
	}
}

func TestPendingQueueConcurrentEnqueueRemainsBounded(t *testing.T) {
	queue := NewPendingQueue()
	const writers = 128
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			queue.Enqueue("client:main", Params{SessionKey: "client:main", Message: fmt.Sprintf("message-%d", i)})
		}(i)
	}
	wg.Wait()

	if got := queue.Len("client:main"); got != 1 {
		t.Fatalf("concurrent Len = %d, want bounded latest-only queue", got)
	}
	got := queue.Drain("client:main")
	if got == nil || got.SessionKey != "client:main" || got.Message == "" {
		t.Fatalf("concurrent Drain returned invalid params: %#v", got)
	}
}

func TestPendingQueueDrainReturnsValueCopy(t *testing.T) {
	queue := NewPendingQueue()
	original := Params{SessionKey: "client:main", Message: "queued"}
	queue.Enqueue("client:main", original)
	original.Message = "caller-mutated"

	got := queue.Drain("client:main")
	if got == nil || got.Message != "queued" {
		t.Fatalf("Drain = %#v, queue retained caller mutation", got)
	}
	got.Message = "consumer-mutated"
	if queue.Len("client:main") != 0 {
		t.Fatal("mutating drained value changed queue state")
	}
}
