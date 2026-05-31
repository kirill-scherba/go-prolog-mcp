// Package prolog provides an embeddable Prolog engine wrapping ichiban/prolog.
package prolog

import (
	_ "embed"
	"fmt"
	"strings"

	"github.com/ichiban/prolog"
)

//go:embed rules.pl
var rulesPL string

// Engine wraps a Prolog interpreter with workflow facts and rules.
type Engine struct {
	p *prolog.Interpreter
}

// ScenarioFact represents a scenario as Prolog facts.
type ScenarioFact struct {
	Name           string
	From           string
	To             string
	RequiredLabels []string
	WithoutLabels  []string
}

// Conflict holds a conflict tuple.
type Conflict struct {
	From string `prolog:"From"`
	A    string `prolog:"A"`
	B    string `prolog:"B"`
}

// New creates an empty Prolog engine.
func New() *Engine {
	return &Engine{p: prolog.New(nil, nil)}
}

// LoadProgram loads a complete Prolog program (rules + facts) at once.
func (e *Engine) LoadProgram(program string) error {
	if err := e.p.Exec(program); err != nil {
		return fmt.Errorf("load prolog program: %w", err)
	}
	return nil
}

// BuildProgram combines rules with scenario facts into one program string.
func BuildProgram(scenarios []ScenarioFact, boardStatuses []string) string {
	var b strings.Builder
	b.WriteString(rulesPL)
	b.WriteString("\n")

	for _, s := range boardStatuses {
		b.WriteString(fmt.Sprintf("board_status('%s').\n", escapeAtom(s)))
	}
	for _, s := range scenarios {
		b.WriteString(fmt.Sprintf("scenario('%s', '%s', '%s', %s, %s).\n",
			escapeAtom(s.Name), escapeAtom(s.From), escapeAtom(s.To),
			labelList(s.RequiredLabels), labelList(s.WithoutLabels),
		))
	}
	return b.String()
}

// QueryConflicts returns all conflict tuples.
func (e *Engine) QueryConflicts() ([]Conflict, error) {
	sols, err := e.p.Query("conflict(From, A, B).")
	if err != nil {
		return nil, fmt.Errorf("prolog query: %w", err)
	}
	defer sols.Close()

	var results []Conflict
	for sols.Next() {
		var c Conflict
		if err := sols.Scan(&c); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		results = append(results, c)
	}
	return results, sols.Err()
}

// QueryDeadlocks returns all deadlocked status names.
func (e *Engine) QueryDeadlocks() ([]string, error) {
	sols, err := e.p.Query("deadlock(Status).")
	if err != nil {
		return nil, fmt.Errorf("prolog query: %w", err)
	}
	defer sols.Close()

	var results []string
	for sols.Next() {
		var r struct{ Status string }
		if err := sols.Scan(&r); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		results = append(results, r.Status)
	}
	return results, sols.Err()
}

// QueryUnreachable returns all unreachable scenario names.
func (e *Engine) QueryUnreachable() ([]string, error) {
	sols, err := e.p.Query("unreachable(Name).")
	if err != nil {
		return nil, fmt.Errorf("prolog query: %w", err)
	}
	defer sols.Close()

	var results []string
	for sols.Next() {
		var r struct{ Name string }
		if err := sols.Scan(&r); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		results = append(results, r.Name)
	}
	return results, sols.Err()
}

// QueryCycles returns all statuses involved in cycles.
func (e *Engine) QueryCycles() ([]string, error) {
	sols, err := e.p.Query("cycle(Status).")
	if err != nil {
		return nil, fmt.Errorf("prolog query: %w", err)
	}
	defer sols.Close()

	seen := make(map[string]bool)
	var results []string
	for sols.Next() {
		var r struct{ Status string }
		if err := sols.Scan(&r); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		if !seen[r.Status] {
			seen[r.Status] = true
			results = append(results, r.Status)
		}
	}
	return results, sols.Err()
}

// QueryScenarios returns all scenario definitions as readable strings.
func (e *Engine) QueryScenarios() ([]string, error) {
	sols, err := e.p.Query("scenario(N, F, T, Req, Without).")
	if err != nil {
		return nil, fmt.Errorf("prolog query: %w", err)
	}
	defer sols.Close()

	var results []string
	for sols.Next() {
		var r struct {
			N, F, T, Req, Without string
		}
		if err := sols.Scan(&r); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		results = append(results, fmt.Sprintf("%s: %s -> %s [%s] without [%s]",
			r.N, r.F, r.T, r.Req, r.Without))
	}
	return results, sols.Err()
}

// QueryRaw executes an arbitrary query and returns all solutions as a list of maps.
func (e *Engine) QueryRaw(queryStr string) ([]map[string]string, error) {
	sols, err := e.p.Query(queryStr)
	if err != nil {
		return nil, err
	}
	defer sols.Close()

	var results []map[string]string

	for sols.Next() {
		m := make(map[string]interface{})
		if err := sols.Scan(m); err != nil {
			return nil, err
		}
		
		res := make(map[string]string)
		for k, v := range m {
			res[k] = fmt.Sprint(v)
		}
		results = append(results, res)
	}
	return results, sols.Err()
}

// TaskFact represents a board item as Prolog facts.
type TaskFact struct {
	ID     int
	Status string
	Labels []string
}

// TriggerResult holds a task selection result.
type TriggerResult struct {
	IssueID      int    `prolog:"IssueID"`
	ScenarioName string `prolog:"ScenarioName"`
}

// BuildProgramWithTasks combines rules, scenarios, and task facts.
func BuildProgramWithTasks(scenarios []ScenarioFact, boardStatuses []string, tasks []TaskFact) string {
	var b strings.Builder
	b.WriteString(BuildProgram(scenarios, boardStatuses))
	b.WriteString("\n")

	for _, t := range tasks {
		b.WriteString(fmt.Sprintf("task(%d, '%s', %s).\n",
			t.ID, escapeAtom(t.Status), labelList(t.Labels)))
	}
	return b.String()
}

// QueryTriggerable returns all (IssueID, ScenarioName) pairs that can be triggered.
func (e *Engine) QueryTriggerable() ([]TriggerResult, error) {
	sols, err := e.p.Query("can_trigger(IssueID, ScenarioName).")
	if err != nil {
		return nil, fmt.Errorf("prolog query: %w", err)
	}
	defer sols.Close()

	var results []TriggerResult
	for sols.Next() {
		var r TriggerResult
		if err := sols.Scan(&r); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		results = append(results, r)
	}
	return results, sols.Err()
}

func escapeAtom(s string) string {
	return strings.ReplaceAll(s, "'", "\\'")
}

func labelList(labels []string) string {
	if len(labels) == 0 {
		return "[]"
	}
	elems := make([]string, len(labels))
	for i, l := range labels {
		elems[i] = fmt.Sprintf("'%s'", escapeAtom(l))
	}
	return "[" + strings.Join(elems, ", ") + "]"
}
