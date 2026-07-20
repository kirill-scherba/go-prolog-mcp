// Package prolog provides an embeddable Prolog engine wrapping ichiban/prolog.
package prolog

import (
	_ "embed"
	"fmt"
	"regexp"
	"strings"

	"github.com/ichiban/prolog"
)

var (
	// rePredHead matches a Prolog predicate definition at the start of a line:
	//   foo(...  or  foo(...) :-
	rePredHead = regexp.MustCompile(`(?m)^\s*([a-zA-Z_]\w*)\s*\(`)
	// reDiscontiguous extracts the predicate indicator from an error like:
	//   "says/2 is discontiguous"
	reDiscontiguous = regexp.MustCompile(`(\w+/\d+)`)
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

// Sanitize replaces problematic Unicode characters that the ichiban/prolog
// parser cannot handle (em dash, en dash, typographic quotes, etc.).
func Sanitize(program string) string {
	program = strings.ReplaceAll(program, "\u2014", "-")   // em dash —
	program = strings.ReplaceAll(program, "\u2013", "-")   // en dash –
	program = strings.ReplaceAll(program, "\u2018", "'")   // left single quote '
	program = strings.ReplaceAll(program, "\u2019", "'")   // right single quote '
	program = strings.ReplaceAll(program, "\u201c", "\"")  // left double quote "
	program = strings.ReplaceAll(program, "\u201d", "\"")  // right double quote "
	program = strings.ReplaceAll(program, "\u2026", "...") // ellipsis …
	program = strings.ReplaceAll(program, "\u00a0", " ")   // non-breaking space
	return program
}

// LoadProgram loads a complete Prolog program (rules + facts) at once.
// Automatically sanitizes Unicode and retries on discontiguous predicate errors
// by adding the necessary discontiguous/1 directive.
func (e *Engine) LoadProgram(program string) error {
	program = Sanitize(program)
	return e.loadProgramRecursive(program)
}

// loadProgramRecursive tries to execute the program. If the parser complains
// about a discontiguous predicate, it extracts the predicate indicator,
// prepends a discontiguous/1 directive, and retries (up to 10 iterations).
func (e *Engine) loadProgramRecursive(program string) error {
	const maxRetries = 10

	for range maxRetries {
		err := e.p.Exec(program)
		if err == nil {
			return nil
		}

		errStr := err.Error()
		if !strings.Contains(errStr, "discontiguous") {
			return fmt.Errorf("load prolog program: %w", err)
		}

		// Extract predicate indicator like "says/2" from "says/2 is discontiguous".
		m := reDiscontiguous.FindString(errStr)
		if m == "" {
			return fmt.Errorf("load prolog program (unparseable discontiguous error): %w", err)
		}

		// Prepend discontiguous directive and retry.
		program = fmt.Sprintf(":- discontiguous(%s).\n%s", m, program)
	}

	reRun := e.p.Exec(program)
	return fmt.Errorf("load prolog program (after %d retries): %w", maxRetries, reRun)
}

// BuildProgram combines rules with scenario and terminal-status facts into one
// program string. Terminal statuses are exempt from deadlock detection.
func BuildProgram(scenarios []ScenarioFact, boardStatuses []string, terminalStatuses []string) string {
	var b strings.Builder
	b.WriteString(rulesPL)
	b.WriteString("\n")

	for _, s := range boardStatuses {
		b.WriteString(fmt.Sprintf("board_status('%s').\n", escapeAtom(s)))
	}
	for _, s := range terminalStatuses {
		b.WriteString(fmt.Sprintf("terminal_status('%s').\n", escapeAtom(s)))
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
// Handles side-effect-only queries (write, nl, etc.) gracefully by recovering
// from panics and returning empty results when no variable bindings exist.
func (e *Engine) QueryRaw(queryStr string) (results []map[string]string, err error) {
	// Recover from nil pointer panics (e.g. Scan on write-only queries).
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("query panicked: %v", r)
		}
	}()

	sols, qErr := e.p.Query(queryStr)
	if qErr != nil {
		return nil, qErr
	}
	defer sols.Close()

	for sols.Next() {
		m := make(map[string]interface{})
		if sErr := sols.Scan(m); sErr != nil {
			// If scanning fails (e.g. no variables to bind), skip this solution
			// but continue iterating.
			continue
		}

		if len(m) == 0 {
			// Side-effect-only solution (write, nl, etc.) — no variables to return.
			continue
		}

		res := make(map[string]string)
		for k, v := range m {
			res[k] = fmt.Sprint(v)
		}
		results = append(results, res)
	}

	if sErr := sols.Err(); sErr != nil {
		return results, sErr
	}
	return results, nil
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

// BuildProgramWithTasks combines rules, scenarios, terminal-status facts,
// and task facts.
func BuildProgramWithTasks(scenarios []ScenarioFact, boardStatuses []string, terminalStatuses []string, tasks []TaskFact) string {
	var b strings.Builder
	b.WriteString(BuildProgram(scenarios, boardStatuses, terminalStatuses))
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
	s = strings.ReplaceAll(s, "'", "\\'")
	// Заменяем любые символы, небезопасные для Prolog-атома в кавычках
	var safe strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			safe.WriteRune(r)
		} else if r == ' ' || r == '-' || r == '_' || r == '.' || r == '/' || r == '#' || r == ':' {
			safe.WriteRune(r)
		} else if r == '\n' || r == '\r' || r == '\t' {
			safe.WriteRune(' ')
		} else {
			safe.WriteRune(' ')
		}
	}
	return strings.TrimSpace(safe.String())
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
