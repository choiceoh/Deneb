package handlerevents

import "testing"

func TestEventsMethods_nilDeps(t *testing.T) {
	m := EventsMethods(EventsDeps{})
	if m != nil {
		t.Fatal("expected nil for nil Broadcaster")
	}
}
