package lmtpd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
)

func queueTestMessage(key string) *Message {
	return &Message{
		Detail:   &gmail.MessageDetail{ID: key},
		DedupKey: key,
		Raw:      []byte("From: a@b.com\r\nSubject: queued\r\n\r\nbody\r\n"),
	}
}

func TestQueueEnqueueClaimComplete(t *testing.T) {
	q, err := NewQueue(t.TempDir())
	if err != nil {
		t.Fatalf("NewQueue: %v", err)
	}
	if ok, err := q.Enqueue(queueTestMessage("k1")); err != nil || !ok {
		t.Fatalf("Enqueue = %v, %v; want true, nil", ok, err)
	}
	if ok, err := q.Enqueue(queueTestMessage("k1")); err != nil || ok {
		t.Fatalf("duplicate Enqueue = %v, %v; want false, nil", ok, err)
	}
	if st := q.Stats(); st.Pending != 1 || st.Processing != 0 || st.Failed != 0 {
		t.Fatalf("stats after enqueue = %+v", st)
	}

	item, err := q.Claim()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if item == nil || item.Key != "k1" {
		t.Fatalf("claimed %+v, want k1", item)
	}
	if st := q.Stats(); st.Pending != 0 || st.Processing != 1 {
		t.Fatalf("stats after claim = %+v", st)
	}
	if err := q.Complete(item); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if st := q.Stats(); st.Pending != 0 || st.Processing != 0 || st.Failed != 0 {
		t.Fatalf("stats after complete = %+v", st)
	}
}

func TestQueueRecoverProcessing(t *testing.T) {
	dir := t.TempDir()
	q, err := NewQueue(dir)
	if err != nil {
		t.Fatalf("NewQueue: %v", err)
	}
	if ok, err := q.Enqueue(queueTestMessage("k1")); err != nil || !ok {
		t.Fatalf("Enqueue = %v, %v", ok, err)
	}
	if _, err := q.Claim(); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if st := q.Stats(); st.Processing != 1 {
		t.Fatalf("processing before recovery = %+v", st)
	}

	q2, err := NewQueue(dir)
	if err != nil {
		t.Fatalf("NewQueue recovery: %v", err)
	}
	if st := q2.Stats(); st.Pending != 1 || st.Processing != 0 {
		t.Fatalf("stats after recovery = %+v", st)
	}
}

func TestQueueFailRequeuesThenFails(t *testing.T) {
	q, err := NewQueue(t.TempDir())
	if err != nil {
		t.Fatalf("NewQueue: %v", err)
	}
	if ok, err := q.Enqueue(queueTestMessage("k1")); err != nil || !ok {
		t.Fatalf("Enqueue = %v, %v", ok, err)
	}
	item, err := q.Claim()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if err := q.Fail(item, errors.New("temporary"), 2); err != nil {
		t.Fatalf("Fail requeue: %v", err)
	}
	if st := q.Stats(); st.Pending != 1 || st.Failed != 0 {
		t.Fatalf("stats after first fail = %+v", st)
	}

	item, err = q.Claim()
	if err != nil {
		t.Fatalf("Claim retry: %v", err)
	}
	if item.Attempts != 1 || item.LastError != "temporary" {
		t.Fatalf("retry metadata = attempts %d error %q", item.Attempts, item.LastError)
	}
	if err := q.Fail(item, errors.New("permanent"), 2); err != nil {
		t.Fatalf("Fail final: %v", err)
	}
	if st := q.Stats(); st.Pending != 0 || st.Processing != 0 || st.Failed != 1 {
		t.Fatalf("stats after final fail = %+v", st)
	}
}

func TestQueueFailedItemBlocksDuplicateUntilRemoved(t *testing.T) {
	dir := t.TempDir()
	q, err := NewQueue(dir)
	if err != nil {
		t.Fatalf("NewQueue: %v", err)
	}
	if ok, err := q.Enqueue(queueTestMessage("k1")); err != nil || !ok {
		t.Fatalf("Enqueue = %v, %v", ok, err)
	}
	item, _ := q.Claim()
	if err := q.Fail(item, errors.New("bad"), 1); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	if ok, err := q.Enqueue(queueTestMessage("k1")); err != nil || ok {
		t.Fatalf("duplicate failed Enqueue = %v, %v; want false, nil", ok, err)
	}
	if err := os.Remove(filepath.Join(dir, "failed", queueFileName("k1"))); err != nil {
		t.Fatalf("remove failed item: %v", err)
	}
	if ok, err := q.Enqueue(queueTestMessage("k1")); err != nil || !ok {
		t.Fatalf("Enqueue after failed removal = %v, %v; want true, nil", ok, err)
	}
}

func TestQueueRecoverProcessingDoesNotReviveFailedItem(t *testing.T) {
	dir := t.TempDir()
	q, err := NewQueue(dir)
	if err != nil {
		t.Fatalf("NewQueue: %v", err)
	}
	if ok, err := q.Enqueue(queueTestMessage("k1")); err != nil || !ok {
		t.Fatalf("Enqueue = %v, %v", ok, err)
	}
	item, err := q.Claim()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	name := queueFileName("k1")
	if err := q.writeItemAtomic(filepath.Join(q.failedDir, name), item); err != nil {
		t.Fatalf("write failed copy: %v", err)
	}

	q2, err := NewQueue(dir)
	if err != nil {
		t.Fatalf("NewQueue recovery: %v", err)
	}
	if st := q2.Stats(); st.Pending != 0 || st.Processing != 0 || st.Failed != 1 {
		t.Fatalf("stats after failed recovery = %+v", st)
	}
}
