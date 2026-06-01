package prolog

import (
	"strings"
	"testing"
)

func TestEscapeAtom(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"simple", "simple"},
		{"it's", "it\\'s"},
		{"no'escape", "no\\'escape"},
		{"", ""},
		{"multiple'quotes'here", "multiple\\'quotes\\'here"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := escapeAtom(tt.input)
			if got != tt.want {
				t.Errorf("escapeAtom(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestLabelList(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  string
	}{
		{"empty", nil, "[]"},
		{"empty_slice", []string{}, "[]"},
		{"single", []string{"bug"}, "['bug']"},
		{"multiple", []string{"bug", "feature"}, "['bug', 'feature']"},
		{"with_quote", []string{"it's"}, "['it\\'s']"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := labelList(tt.input)
			if got != tt.want {
				t.Errorf("labelList(%v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestBuildProgram(t *testing.T) {
	scenarios := []ScenarioFact{
		{Name: "start", From: "Backlog", To: "In progress"},
	}
	statuses := []string{"Backlog", "In progress", "Done"}

	program := BuildProgram(scenarios, statuses)

	if !strings.Contains(program, "conflict") {
		t.Error("BuildProgram output missing conflict rule")
	}
	if !strings.Contains(program, "deadlock") {
		t.Error("BuildProgram output missing deadlock rule")
	}
	if !strings.Contains(program, "unreachable") {
		t.Error("BuildProgram output missing unreachable rule")
	}
	if !strings.Contains(program, "cycle") {
		t.Error("BuildProgram output missing cycle rule")
	}
	if !strings.Contains(program, "board_status('Backlog').") {
		t.Error("BuildProgram missing board_status('Backlog')")
	}
	if !strings.Contains(program, "board_status('Done').") {
		t.Error("BuildProgram missing board_status('Done')")
	}
	if !strings.Contains(program, "scenario('start', 'Backlog', 'In progress'") {
		t.Error("BuildProgram missing scenario fact")
	}
}

func TestBuildProgramWithTasks(t *testing.T) {
	scenarios := []ScenarioFact{
		{Name: "start", From: "Backlog", To: "In progress", RequiredLabels: []string{"approved"}},
	}
	statuses := []string{"Backlog", "In progress"}
	tasks := []TaskFact{
		{ID: 1, Status: "Backlog", Labels: []string{"approved"}},
	}

	program := BuildProgramWithTasks(scenarios, statuses, tasks)

	if !strings.Contains(program, "task(1, 'Backlog', ['approved']).") {
		t.Error("BuildProgramWithTasks missing task fact")
	}
}

func TestQueryRaw(t *testing.T) {
	eng := New()
	err := eng.LoadProgram(`
		fact(a).
		fact(b).
		fact(c).
	`)
	if err != nil {
		t.Fatalf("LoadProgram: %v", err)
	}

	results, err := eng.QueryRaw("fact(X).")
	if err != nil {
		t.Fatalf("QueryRaw: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	seen := make(map[string]bool)
	for _, r := range results {
		val, ok := r["X"]
		if !ok {
			t.Error("result missing variable X")
		}
		seen[val] = true
	}

	for _, want := range []string{"a", "b", "c"} {
		if !seen[want] {
			t.Errorf("missing solution X=%q", want)
		}
	}
}

func TestNewAndLoadProgram(t *testing.T) {
	eng := New()
	if eng == nil {
		t.Fatal("New() returned nil")
	}

	if err := eng.LoadProgram("true."); err != nil {
		t.Fatalf("LoadProgram('true.'): %v", err)
	}

	if err := eng.LoadProgram("!!!invalid!!!"); err == nil {
		t.Error("expected error for invalid program, got nil")
	}
}
