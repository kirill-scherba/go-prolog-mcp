package workflow

import (
	"fmt"
	"sort"
	"strings"

	"github.com/kirill-scherba/go-prolog-mcp/prolog"
)

// ValidationResult holds the complete validation output.
type ValidationResult struct {
	Conflicts   []prolog.Conflict `json:"conflicts"`
	Deadlocks   []string           `json:"deadlocks"`
	Unreachable []string           `json:"unreachable_scenarios"`
	Cycles      []string           `json:"cycles"`
	Valid       bool               `json:"valid"`
}

// ValidateConfig validates from JSON string.
func ValidateConfig(cfgJSON string) (*ValidationResult, error) {
	facts, statuses, err := LoadFromJSON(cfgJSON)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	return Validate(facts, statuses)
}

// ValidateFile validates from a file path.
func ValidateFile(path string) (*ValidationResult, error) {
	facts, statuses, err := LoadFromFile(path)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	return Validate(facts, statuses)
}

// Validate runs Prolog validation rules against scenario facts.
func Validate(facts []prolog.ScenarioFact, statuses []string) (*ValidationResult, error) {
	eng, err := NewEngine(facts, statuses)
	if err != nil {
		return nil, fmt.Errorf("create engine: %w", err)
	}

	result := &ValidationResult{}

	result.Conflicts, _ = eng.QueryConflicts()
	result.Deadlocks, _ = eng.QueryDeadlocks()
	result.Unreachable, _ = eng.QueryUnreachable()
	result.Cycles, _ = eng.QueryCycles()

	result.Valid = len(result.Conflicts) == 0 &&
		len(result.Deadlocks) == 0 &&
		len(result.Unreachable) == 0 &&
		len(result.Cycles) == 0

	return result, nil
}

// String returns a human-readable summary of the validation result.
func (r *ValidationResult) String() string {
	if r.Valid {
		return "✅ Workflow is valid — no conflicts, deadlocks, cycles, or unreachable scenarios."
	}

	var b strings.Builder

	if len(r.Conflicts) > 0 {
		b.WriteString(fmt.Sprintf("⚡ CONFLICTS (%d):\n", len(r.Conflicts)))
		for _, c := range r.Conflicts {
			b.WriteString(fmt.Sprintf("  • %q in %q: %q and %q can both match the same item\n",
				c.From, c.A, c.B))
		}
	}

	if len(r.Deadlocks) > 0 {
		b.WriteString(fmt.Sprintf("💀 DEADLOCKS (%d):\n", len(r.Deadlocks)))
		sort.Strings(r.Deadlocks)
		for _, d := range r.Deadlocks {
			b.WriteString(fmt.Sprintf("  • %q — no outgoing scenario\n", d))
		}
	}

	if len(r.Unreachable) > 0 {
		b.WriteString(fmt.Sprintf("🚫 UNREACHABLE SCENARIOS (%d):\n", len(r.Unreachable)))
		sort.Strings(r.Unreachable)
		for _, u := range r.Unreachable {
			b.WriteString(fmt.Sprintf("  • %q\n", u))
		}
	}

	if len(r.Cycles) > 0 {
		b.WriteString(fmt.Sprintf("🔄 CYCLES (%d):\n", len(r.Cycles)))
		sort.Strings(r.Cycles)
		for _, c := range r.Cycles {
			b.WriteString(fmt.Sprintf("  • %q can loop back to itself\n", c))
		}
	}

	return b.String()
}
