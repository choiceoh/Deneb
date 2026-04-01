package metrics

import "testing"

func TestWireStatsSnapshot(t *testing.T) {
	// Record some wire activity.
	WireCallsTotal.Inc("reply", "ok")
	WireCallsTotal.Inc("reply", "ok")
	WireCallsTotal.Inc("reply", "error")
	WireBytesTotal.Add(1024, "reply", "out")
	WireBytesTotal.Add(512, "reply", "out")

	WireCallsTotal.Inc("typing", "ok")

	snap := WireStatsSnapshot()

	reply, ok := snap["reply"]
	if !ok {
		t.Fatal("expected reply wire in snapshot")
	}
	if reply.CallsOK < 2 {
		t.Errorf("reply.CallsOK = %d, want >= 2", reply.CallsOK)
	}
	if reply.CallsErr < 1 {
		t.Errorf("reply.CallsErr = %d, want >= 1", reply.CallsErr)
	}
	if reply.BytesOut < 1536 {
		t.Errorf("reply.BytesOut = %d, want >= 1536", reply.BytesOut)
	}

	typing, ok := snap["typing"]
	if !ok {
		t.Fatal("expected typing wire in snapshot")
	}
	if typing.CallsOK < 1 {
		t.Errorf("typing.CallsOK = %d, want >= 1", typing.CallsOK)
	}
}
