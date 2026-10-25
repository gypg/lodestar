package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// GET /api/v1/model/probe-states feeds the settings panel. Probe rows carry
// operational telemetry (group names, timestamps, failure counters) and no
// customer-facing value, so the route must stay inside the settings:read
// gated model group -- folding it into the ungated /capabilities group would
// expose it to every paying customer.
//
// ★ Source assertion, same precedent as TestModelCapabilitiesIsNotInAPermissionGatedGroup:
// gin's route table does not expose middleware chains, so the gate is proven
// by where the route is registered.
func TestModelProbeStatesStaysInTheGatedModelGroup(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("handlers", "model.go"))
	if err != nil {
		t.Fatalf("read handlers/model.go: %v", err)
	}
	text := string(src)

	const marker = `router.NewGroupRouter("/api/v1/model")`
	blocks := strings.Split(text, marker)
	if len(blocks) < 3 {
		t.Fatalf("expected gated + ungated model groups, found %d", len(blocks)-1)
	}

	for i, b := range blocks[1:] {
		if end := strings.Index(b, "router.NewGroupRouter("); end != -1 {
			b = b[:end]
		}
		hasProbeStates := strings.Contains(b, `"/probe-states"`)
		hasGate := strings.Contains(b, "PermSettingsRead")
		if hasProbeStates && !hasGate {
			t.Fatalf("/probe-states is registered in the ungated model group (block %d): panel telemetry must require settings:read", i+1)
		}
	}

	engine := getProductionEngine(t)
	found := false
	for _, r := range engine.Routes() {
		if r.Method == "GET" && r.Path == "/api/v1/model/probe-states" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("GET /api/v1/model/probe-states is not registered")
	}
}
