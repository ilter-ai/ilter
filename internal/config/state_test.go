package config

import "testing"

func TestMergeRoutingOverrides_Jev(t *testing.T) {
	snap := &RuntimeConfigSnapshot{}
	mergeRuntimeConfigValues(snap, map[string]string{
		"routing:scorer":       "jev",
		"routing:jev_provider": "zen",
		"routing:jev_model":    "jev-1.13",
	})
	if snap.Routing.Scorer.Type != "jev" || snap.Routing.Scorer.Jev == nil ||
		snap.Routing.Scorer.Jev.Provider != "zen" || snap.Routing.Scorer.Jev.Model != "jev-1.13" {
		t.Fatalf("unexpected scorer config: %+v", snap.Routing.Scorer)
	}

	snap = &RuntimeConfigSnapshot{}
	mergeRuntimeConfigValues(snap, map[string]string{"routing:scorer": "heuristic"})
	if snap.Routing.Scorer.Type != "heuristic" || snap.Routing.Scorer.Jev != nil {
		t.Fatalf("unexpected scorer config: %+v", snap.Routing.Scorer)
	}
}
