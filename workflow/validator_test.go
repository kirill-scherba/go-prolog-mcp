package workflow

import (
	"sort"
	"testing"

	"github.com/kirill-scherba/go-prolog-mcp/prolog"
)

type validateTestCase struct {
	name    string
	config  string
	want    *ValidationResult
	wantErr bool
}

func TestValidate(t *testing.T) {
	tests := []validateTestCase{
		{
			name: "valid/minimal",
			config: `{
				"statuses": { "backlog": "Backlog", "done": "Done" },
				"scenarios": {
					"finish": { "trigger_status": "Backlog", "next_status": "Done" }
				}
			}`,
			want: &ValidationResult{Valid: true},
		},
		{
			name: "valid/multi_scenario",
			config: `{
				"statuses": { "b": "Backlog", "ip": "In progress", "ir": "In review", "d": "Done" },
				"scenarios": {
					"start":   { "trigger_status": "Backlog", "next_status": "In progress" },
					"review":  { "trigger_status": "In progress", "next_status": "In review" },
					"approve": { "trigger_status": "In review", "next_status": "Done" }
				}
			}`,
			want: &ValidationResult{Valid: true},
		},
		{
			// Two scenarios from same status with disjoint required labels: no conflict.
			name: "valid/with_labels",
			config: `{
				"statuses": { "b": "Backlog", "ip": "In progress", "r": "Review", "d": "Done" },
				"scenarios": {
					"start":     { "trigger_status": "Backlog", "next_status": "In progress" },
					"to_review": { "trigger_status": "In progress", "next_status": "Review", "required_labels": ["bug"] },
					"to_done":   { "trigger_status": "In progress", "next_status": "Done", "required_labels": ["feature"] },
					"finish_r":  { "trigger_status": "Review", "next_status": "Done" }
				}
			}`,
			want: &ValidationResult{Valid: true},
		},
		{
			// Two scenarios from same status sharing required labels: conflict detected.
			name: "conflict/two_from_same",
			config: `{
				"statuses": { "b": "Backlog", "ip": "In progress", "r": "Review", "d": "Done" },
				"scenarios": {
					"start":     { "trigger_status": "Backlog", "next_status": "In progress" },
					"to_review": { "trigger_status": "In progress", "next_status": "Review", "required_labels": ["approved"] },
					"to_done":   { "trigger_status": "In progress", "next_status": "Done", "required_labels": ["approved"] },
					"finish_r":  { "trigger_status": "Review", "next_status": "Done" }
				}
			}`,
			want: &ValidationResult{
				Valid: false,
				Conflicts: []prolog.Conflict{
					{From: "In progress", A: "to_done", B: "to_review"},
				},
			},
		},
		{
			// Conflict avoided because without_labels on one scenario blocks the other.
			name: "conflict/with_without_protection",
			config: `{
				"statuses": { "b": "Backlog", "ip": "In progress", "r": "Review", "d": "Done" },
				"scenarios": {
					"start":     { "trigger_status": "Backlog", "next_status": "In progress" },
					"to_review": { "trigger_status": "In progress", "next_status": "Review", "required_labels": ["approved"], "without_labels": ["urgent"] },
					"to_done":   { "trigger_status": "In progress", "next_status": "Done", "required_labels": ["approved", "urgent"] },
					"finish_r":  { "trigger_status": "Review", "next_status": "Done" }
				}
			}`,
			want: &ValidationResult{Valid: true},
		},
		{
			// Status "Stuck" has no outgoing scenario => deadlock.
			name: "deadlock/simple",
			config: `{
				"statuses": { "b": "Backlog", "ip": "In progress", "stuck": "Stuck", "d": "Done" },
				"scenarios": {
					"start":     { "trigger_status": "Backlog", "next_status": "In progress" },
					"get_stuck": { "trigger_status": "In progress", "next_status": "Stuck" }
				}
			}`,
			want: &ValidationResult{
				Valid:     false,
				Deadlocks: []string{"Stuck"},
			},
		},
		{
			// All non-Done statuses have outgoing scenarios => no deadlock.
			name: "deadlock/no_deadlock",
			config: `{
				"statuses": { "b": "Backlog", "ip": "In progress", "d": "Done" },
				"scenarios": {
					"start":  { "trigger_status": "Backlog", "next_status": "In progress" },
					"finish": { "trigger_status": "In progress", "next_status": "Done" }
				}
			}`,
			want: &ValidationResult{Valid: true},
		},
		{
			// Scenario "secret" has trigger "Hidden" which is not Backlog and not reachable.
			name: "unreachable/simple",
			config: `{
				"statuses": { "b": "Backlog", "ip": "In progress", "hidden": "Hidden", "d": "Done" },
				"scenarios": {
					"start":  { "trigger_status": "Backlog", "next_status": "In progress" },
					"finish": { "trigger_status": "In progress", "next_status": "Done" },
					"secret": { "trigger_status": "Hidden", "next_status": "Done" }
				}
			}`,
			want: &ValidationResult{
				Valid:       false,
				Unreachable: []string{"secret"},
			},
		},
		{
			// Scenario from Backlog is never unreachable.
			name: "unreachable/backlog_entry",
			config: `{
				"statuses": { "b": "Backlog", "d": "Done" },
				"scenarios": {
					"direct": { "trigger_status": "Backlog", "next_status": "Done" }
				}
			}`,
			want: &ValidationResult{Valid: true},
		},
		{
			// A -> B -> A creates a cycle for both A and B.
			name: "cycle/simple",
			config: `{
				"statuses": { "a": "A", "b": "B" },
				"scenarios": {
					"a_to_b": { "trigger_status": "A", "next_status": "B" },
					"b_to_a": { "trigger_status": "B", "next_status": "A" }
				}
			}`,
			want: &ValidationResult{
				Valid:  false,
				Cycles: []string{"A", "B"},
			},
		},
		{
			// Linear A -> B -> C -> Done has no cycles.
			name: "cycle/no_cycle",
			config: `{
				"statuses": { "b": "Backlog", "a": "A", "b2": "B", "c": "C", "d": "Done" },
				"scenarios": {
					"start":  { "trigger_status": "Backlog", "next_status": "A" },
					"a_to_b":  { "trigger_status": "A", "next_status": "B" },
					"b_to_c":  { "trigger_status": "B", "next_status": "C" },
					"c_done":  { "trigger_status": "C", "next_status": "Done" }
				}
			}`,
			want: &ValidationResult{Valid: true},
		},
		{
			name:    "error/invalid_json",
			config:  `{invalid json`,
			wantErr: true,
		},
		{
			// Scenario with empty trigger_status should be skipped.
			name: "scenario/skip_empty_trigger",
			config: `{
				"statuses": { "b": "Backlog", "d": "Done" },
				"scenarios": {
					"valid":      { "trigger_status": "Backlog", "next_status": "Done" },
					"no_trigger": { "next_status": "Done" }
				}
			}`,
			want: &ValidationResult{Valid: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ValidateConfig(tt.config)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got.Valid != tt.want.Valid {
				t.Errorf("Valid = %v, want %v", got.Valid, tt.want.Valid)
			}

			sort.Strings(got.Deadlocks)
			sort.Strings(tt.want.Deadlocks)
			if !stringSliceEqual(got.Deadlocks, tt.want.Deadlocks) {
				t.Errorf("Deadlocks = %v, want %v", got.Deadlocks, tt.want.Deadlocks)
			}

			sort.Strings(got.Unreachable)
			sort.Strings(tt.want.Unreachable)
			if !stringSliceEqual(got.Unreachable, tt.want.Unreachable) {
				t.Errorf("Unreachable = %v, want %v", got.Unreachable, tt.want.Unreachable)
			}

			sort.Strings(got.Cycles)
			sort.Strings(tt.want.Cycles)
			if !stringSliceEqual(got.Cycles, tt.want.Cycles) {
				t.Errorf("Cycles = %v, want %v", got.Cycles, tt.want.Cycles)
			}

			if !conflictsEqual(got.Conflicts, tt.want.Conflicts) {
				t.Errorf("Conflicts = %+v, want %+v", got.Conflicts, tt.want.Conflicts)
			}
		})
	}
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func conflictsEqual(a, b []prolog.Conflict) bool {
	if len(a) != len(b) {
		return false
	}

	setA := make(map[string]int)
	for _, c := range a {
		key := c.From + "|" + c.A + "|" + c.B
		setA[key]++
	}
	setB := make(map[string]int)
	for _, c := range b {
		key := c.From + "|" + c.A + "|" + c.B
		setB[key]++
	}

	for k, v := range setA {
		if setB[k] != v {
			return false
		}
	}
	return true
}
