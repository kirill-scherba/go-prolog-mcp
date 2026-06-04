// Package workflow converts orchestrator scenario configurations into Prolog facts
// and provides validation queries.
package workflow

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/kirill-scherba/go-prolog-mcp/prolog"
)

// OrcConfig mirrors the relevant subset of orchestrator-watchdog's config.json.
type OrcConfig struct {
	Statuses map[string]string                `json:"statuses"`
	Scenarios map[string]ScenarioDef          `json:"scenarios"`
}

// ScenarioDef mirrors config.ScenarioConfig for loading from JSON.
type ScenarioDef struct {
	Type           string   `json:"type,omitempty"`
	TriggerStatus  string   `json:"trigger_status"`
	NextStatus     string   `json:"next_status"`
	RequiredLabels []string `json:"required_labels,omitempty"`
	WithoutLabels  []string `json:"without_labels,omitempty"`
	RequiredLabel  string   `json:"required_label,omitempty"`
	Disable        bool     `json:"disable,omitempty"`
}

// LoadFromFile reads an orchestrator config.json and returns Prolog scenario facts.
func LoadFromFile(path string) ([]prolog.ScenarioFact, []string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read config: %w", err)
	}

	var cfg OrcConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, nil, fmt.Errorf("parse config: %w", err)
	}

	return ConvertToFacts(cfg)
}

// LoadFromJSON reads orchestrator config from a JSON string.
func LoadFromJSON(jsonData string) ([]prolog.ScenarioFact, []string, error) {
	var cfg OrcConfig
	if err := json.Unmarshal([]byte(jsonData), &cfg); err != nil {
		return nil, nil, fmt.Errorf("parse config: %w", err)
	}

	return ConvertToFacts(cfg)
}

// ConvertToFacts converts an OrcConfig into Prolog scenario facts and board statuses.
func ConvertToFacts(cfg OrcConfig) ([]prolog.ScenarioFact, []string, error) {
	// Collect all unique board statuses from statuses map + trigger/next from scenarios.
	statusSet := make(map[string]bool)

	// Add statuses from the statuses map.
	for _, v := range cfg.Statuses {
		statusSet[v] = true
	}

	var facts []prolog.ScenarioFact

	for name, sc := range cfg.Scenarios {
		// Skip non-AI, non-bridge, non-merge, non-shell — all types are valid
		// as long as they have trigger and next status.
		if sc.TriggerStatus == "" || sc.NextStatus == "" {
			continue
		}

		// Skip disabled scenarios (matching orchestrator's behavior).
		if sc.Disable {
			continue
		}

		// Add trigger/next to status set.
		statusSet[sc.TriggerStatus] = true
		statusSet[sc.NextStatus] = true

		// Combine required_label (singular) and required_labels (plural).
		var reqLabels []string
		if sc.RequiredLabel != "" {
			reqLabels = append(reqLabels, sc.RequiredLabel)
		}
		reqLabels = append(reqLabels, sc.RequiredLabels...)

		facts = append(facts, prolog.ScenarioFact{
			Name:           name,
			From:           sc.TriggerStatus,
			To:             sc.NextStatus,
			RequiredLabels: reqLabels,
			WithoutLabels:  sc.WithoutLabels,
		})
	}

	// Convert status set to sorted slice.
	statuses := make([]string, 0, len(statusSet))
	for s := range statusSet {
		statuses = append(statuses, s)
	}

	return facts, statuses, nil
}
