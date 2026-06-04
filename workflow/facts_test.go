package workflow

import (
	"encoding/json"
	"testing"
)

// TestConvertToFacts_DisabledScenarios verifies that scenarios with
// "disable: true" are excluded from the generated Prolog facts.
func TestConvertToFacts_DisabledScenarios(t *testing.T) {
	configJSON := `{
		"statuses": {
			"backlog": "Backlog",
			"ready": "Ready"
		},
		"scenarios": {
			"enabled_scenario": {
				"trigger_status": "Backlog",
				"next_status": "Ready",
				"required_labels": ["plan/improve"]
			},
			"disabled_scenario": {
				"trigger_status": "Backlog",
				"next_status": "Backlog",
				"disable": true
			}
		}
	}`

	var cfg OrcConfig
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		t.Fatalf("failed to parse test config: %v", err)
	}

	facts, statuses, err := ConvertToFacts(cfg)
	if err != nil {
		t.Fatalf("ConvertToFacts failed: %v", err)
	}

	// Should only have the enabled scenario.
	if len(facts) != 1 {
		t.Errorf("expected 1 fact (enabled), got %d", len(facts))
		for i, f := range facts {
			t.Logf("  fact[%d]: Name=%q From=%q To=%q", i, f.Name, f.From, f.To)
		}
	}

	// Verify the enabled scenario is present.
	found := false
	for _, f := range facts {
		if f.Name == "enabled_scenario" {
			found = true
			if f.From != "Backlog" || f.To != "Ready" {
				t.Errorf("enabled_scenario: expected From=Backlog To=Ready, got From=%q To=%q", f.From, f.To)
			}
			break
		}
	}
	if !found {
		t.Error("enabled_scenario not found in facts")
	}

	// Verify the disabled scenario is absent.
	for _, f := range facts {
		if f.Name == "disabled_scenario" {
			t.Error("disabled_scenario should not be in facts (disable: true)")
		}
	}

	// Verify Backlog and Ready are in statuses.
	hasBacklog := false
	hasReady := false
	for _, s := range statuses {
		if s == "Backlog" {
			hasBacklog = true
		}
		if s == "Ready" {
			hasReady = true
		}
	}
	if !hasBacklog {
		t.Error("Backlog should be in statuses")
	}
	if !hasReady {
		t.Error("Ready should be in statuses")
	}
}

// TestConvertToFacts_AllDisabled verifies that when all scenarios are disabled,
// no facts are generated but statuses are still populated from the statuses map.
func TestConvertToFacts_AllDisabled(t *testing.T) {
	configJSON := `{
		"statuses": {
			"backlog": "Backlog",
			"done": "Done"
		},
		"scenarios": {
			"task_a": {
				"trigger_status": "Backlog",
				"next_status": "Ready",
				"disable": true
			},
			"task_b": {
				"trigger_status": "Ready",
				"next_status": "Done",
				"disable": true
			}
		}
	}`

	var cfg OrcConfig
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		t.Fatalf("failed to parse test config: %v", err)
	}

	facts, statuses, err := ConvertToFacts(cfg)
	if err != nil {
		t.Fatalf("ConvertToFacts failed: %v", err)
	}

	if len(facts) != 0 {
		t.Errorf("expected 0 facts (all disabled), got %d", len(facts))
	}

	// Statuses from the statuses map should still be present.
	if len(statuses) == 0 {
		t.Error("statuses should not be empty even when all scenarios are disabled")
	}
}

// TestConvertToFacts_MixedScenarios verifies that a mix of enabled and disabled
// scenarios is handled correctly, including scenarios without trigger_status.
func TestConvertToFacts_MixedScenarios(t *testing.T) {
	configJSON := `{
		"statuses": {
			"backlog": "Backlog",
			"in_review": "In review"
		},
		"scenarios": {
			"active_scenario": {
				"trigger_status": "Backlog",
				"next_status": "In review",
				"required_labels": ["plan/approved"]
			},
			"disabled_manager": {
				"trigger_status": "In review",
				"next_status": "In review",
				"without_labels": ["plan/manager_checked"],
				"disable": true
			},
			"schedule_trigger": {
				"next_status": "Backlog",
				"trigger": {
					"type": "schedule",
					"schedule": {
						"every": "24h",
						"repos": ["owner/repo"]
					}
				},
				"disable": true
			}
		}
	}`

	var cfg OrcConfig
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		t.Fatalf("failed to parse test config: %v", err)
	}

	facts, _, err := ConvertToFacts(cfg)
	if err != nil {
		t.Fatalf("ConvertToFacts failed: %v", err)
	}

	// Only active_scenario should be included.
	// disabled_manager has trigger_status so would be included if not disabled.
	// schedule_trigger has no trigger_status, so skipped regardless of disable.
	if len(facts) != 1 {
		t.Errorf("expected 1 fact (only active_scenario), got %d", len(facts))
		for i, f := range facts {
			t.Logf("  fact[%d]: Name=%q From=%q To=%q", i, f.Name, f.From, f.To)
		}
	}

	// Check disabled_manager is not present.
	for _, f := range facts {
		if f.Name == "disabled_manager" {
			t.Error("disabled_manager should not be in facts (disable: true)")
		}
	}
}
