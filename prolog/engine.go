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
