package lifecycle

import "testing"

func TestIdentityForReturnsCompleteOperationalLayerIdentity(t *testing.T) {
	layers := [...]Layer{LayerL1, LayerL2, LayerL3, LayerL4}
	for _, layer := range layers {
		identity, ok := IdentityFor(layer)
		if !ok {
			t.Fatalf("IdentityFor(%q) not found", layer)
		}
		if identity.Layer != layer || identity.Title == "" || identity.Detail == "" {
			t.Fatalf("incomplete identity for %q: %+v", layer, identity)
		}
	}
	if identity, ok := IdentityFor(Layer("unknown")); ok || identity != (Identity{}) {
		t.Fatalf("unknown identity = (%+v, %v), want zero, false", identity, ok)
	}
}
