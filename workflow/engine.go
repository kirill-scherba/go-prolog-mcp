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

// SelectTasks executes the task selection logic for the given board items.
func SelectTasks(cfgJSON string, tasks []prolog.TaskFact) ([]prolog.TriggerResult, error) {
	facts, statuses, err := LoadFromJSON(cfgJSON)
	if err != nil {
		return nil, err
	}

	program := prolog.BuildProgramWithTasks(facts, statuses, tasks)
	eng := prolog.New()
	if err := eng.LoadProgram(program); err != nil {
		return nil, err
	}

	return eng.QueryTriggerable()
}

// SelectTasksFile executes the task selection logic using a config file.
func SelectTasksFile(configPath string, tasks []prolog.TaskFact) ([]prolog.TriggerResult, error) {
	facts, statuses, err := LoadFromFile(configPath)
	if err != nil {
		return nil, err
	}

	program := prolog.BuildProgramWithTasks(facts, statuses, tasks)
	eng := prolog.New()
	if err := eng.LoadProgram(program); err != nil {
		return nil, err
	}

	return eng.QueryTriggerable()
}
