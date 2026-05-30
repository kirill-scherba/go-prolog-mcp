package workflow

import (
	"fmt"

	"github.com/kirill-scherba/go-prolog-mcp/prolog"
)

// NewEngine creates a Prolog engine loaded with the given scenario facts.
func NewEngine(facts []prolog.ScenarioFact, statuses []string) (*prolog.Engine, error) {
	program := prolog.BuildProgram(facts, statuses)

	eng := prolog.New()
	if err := eng.LoadProgram(program); err != nil {
		return nil, fmt.Errorf("load program: %w", err)
	}

	return eng, nil
}

// NewEngineFromJSON creates a Prolog engine loaded from a JSON config string.
func NewEngineFromJSON(cfgJSON string) (*prolog.Engine, error) {
	facts, statuses, err := LoadFromJSON(cfgJSON)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	return NewEngine(facts, statuses)
}

// NewEngineFromFile creates a Prolog engine loaded from a config file path.
func NewEngineFromFile(path string) (*prolog.Engine, error) {
	facts, statuses, err := LoadFromFile(path)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	return NewEngine(facts, statuses)
}
