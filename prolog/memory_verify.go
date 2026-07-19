package prolog

import (
	_ "embed"
	"fmt"
	"strings"
	"time"
)

//go:embed memory_rules.pl
var memoryRulesPL string

// MemoryFact represents one memory entry as a Prolog mem/4 fact.
type MemoryFact struct {
	Key      string `json:"key"`
	Category string `json:"category"`
	Attr     string `json:"attr"`
	Value    string `json:"value"`
}

// MemoryIssue is a detected contradiction or anomaly.
type MemoryIssue struct {
	Key  string `json:"key"`
	Type string `json:"type"`
}

// MemoryStats holds summary counters for a verification run.
type MemoryStats struct {
	FactsChecked        int `json:"facts_checked"`
	ContradictionsFound int `json:"contradictions_found"`
	AnomaliesFound      int `json:"anomalies_found"`
	RulesApplied        int `json:"rules_applied"`
}

// MemoryVerifyResult is the complete output of the memory_verify tool.
type MemoryVerifyResult struct {
	Contradictions []MemoryIssue `json:"contradictions"`
	Anomalies      []MemoryIssue `json:"anomalies"`
	Stats          MemoryStats   `json:"stats"`
}

// BuildMemoryFacts converts a slice of MemoryFact into Prolog mem/4 facts.
func BuildMemoryFacts(facts []MemoryFact) string {
	var b strings.Builder
	for _, f := range facts {
		b.WriteString(fmt.Sprintf("mem('%s', '%s', '%s', '%s').\n",
			escapeAtom(f.Key),
			escapeAtom(f.Category),
			escapeAtom(f.Attr),
			escapeAtom(f.Value),
		))
	}
	return b.String()
}

// BuildMemoryProgram combines the embedded default rules with optional custom rules.
func BuildMemoryProgram(customRules string) string {
	var b strings.Builder
	b.WriteString(memoryRulesPL)
	b.WriteString("\n")
	if strings.TrimSpace(customRules) != "" {
		b.WriteString(customRules)
		b.WriteString("\n")
	}
	return b.String()
}

// BuildMemoryProgramWithFacts returns a full Prolog program for memory verification.
// It injects `today('YYYY-MM-DD').` so date-aware rules can compare deadlines.
func BuildMemoryProgramWithFacts(facts []MemoryFact, customRules string) string {
	var b strings.Builder
	b.WriteString(BuildMemoryProgram(customRules))
	b.WriteString(fmt.Sprintf("today('%s').\n", isoToday()))
	b.WriteString("\n")
	b.WriteString(BuildMemoryFacts(facts))
	return b.String()
}

// isoToday returns the current local date in ISO 8601 ('YYYY-MM-DD') format.
func isoToday() string {
	return time.Now().Format("2006-01-02")
}

// VerifyMemory runs contradiction and anomaly queries against the provided facts.
func VerifyMemory(facts []MemoryFact, customRules string) (*MemoryVerifyResult, error) {
	result := &MemoryVerifyResult{
		Stats: MemoryStats{
			FactsChecked: len(facts),
			RulesApplied: countRuleTypes(BuildMemoryProgram(customRules)),
		},
	}

	if len(facts) == 0 {
		return result, nil
	}

	program := BuildMemoryProgramWithFacts(facts, customRules)
	eng := New()
	if err := eng.LoadProgram(program); err != nil {
		return nil, fmt.Errorf("load memory verification program: %w", err)
	}

	var errs []string

	contradictions, err := eng.QueryMemoryIssues("contradiction")
	if err != nil {
		errs = append(errs, fmt.Sprintf("query contradictions: %v", err))
	} else {
		result.Contradictions = contradictions
	}

	anomalies, err := eng.QueryMemoryIssues("anomaly")
	if err != nil {
		errs = append(errs, fmt.Sprintf("query anomalies: %v", err))
	} else {
		result.Anomalies = anomalies
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("memory verification failed: %s", strings.Join(errs, "; "))
	}

	result.Stats.ContradictionsFound = len(result.Contradictions)
	result.Stats.AnomaliesFound = len(result.Anomalies)
	return result, nil
}

// QueryMemoryIssues executes a memory issue predicate and returns all matches.
func (e *Engine) QueryMemoryIssues(predicate string) ([]MemoryIssue, error) {
	query := fmt.Sprintf("%s(Key, Type).", predicate)
	sols, err := e.p.Query(query)
	if err != nil {
		return nil, fmt.Errorf("prolog query %q: %w", query, err)
	}
	defer sols.Close()

	seen := make(map[string]bool)
	var results []MemoryIssue
	for sols.Next() {
		var r MemoryIssue
		if err := sols.Scan(&r); err != nil {
			return nil, fmt.Errorf("scan %q: %w", query, err)
		}
		// Deduplicate by composite key in case multiple fact combinations match.
		k := r.Key + "|" + r.Type
		if !seen[k] {
			seen[k] = true
			results = append(results, r)
		}
	}
	return results, sols.Err()
}

// countRuleTypes counts contradiction/ and anomaly/ rule definitions in a program.
// Rule heads are expected at column 0; indented lines are treated as rule bodies.
func countRuleTypes(program string) int {
	count := 0
	for _, line := range strings.Split(program, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "%") {
			continue
		}
		if line == trimmed && (strings.HasPrefix(trimmed, "contradiction(") || strings.HasPrefix(trimmed, "anomaly(")) {
			count++
		}
	}
	return count
}
