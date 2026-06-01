# go-prolog-mcp

[![Go Version](https://img.shields.io/github/go-mod/go-version/kirill-scherba/go-prolog-mcp)](go.mod)
[![License](https://img.shields.io/github/license/kirill-scherba/go-prolog-mcp)](#license)

An MCP server for orchestrator workflow validation using an embedded Prolog engine. Evaluates workflow configurations for conflicts, deadlocks, unreachable scenarios, and cycles -- then selects tasks to trigger based on rule matching.

Built with [ichiban/prolog](https://github.com/ichiban/prolog) and the [MCP Go SDK](https://github.com/mark3labs/mcp-go).

## Architecture

The server runs over **stdio transport** (standard MCP protocol) and can also be used as a standalone CLI tool for one-shot validation.

```
main.go (MCP server + CLI)
  |
  +-- workflow/           Config loading and validation orchestration
  |     facts.go          Config parsing, Prolog fact generation
  |     engine.go         Engine constructors
  |     validator.go      Validate() entry point, result formatting
  |
  +-- prolog/             Prolog engine wrapper
        engine.go         Engine, program builder, query methods
        rules.pl          Embedded Prolog validation rules
```

## MCP Tools

| Tool | Description | Required Params |
|---|---|---|
| `validate_workflow` | Validate a workflow config from JSON string | `config_json` |
| `validate_workflow_file` | Validate a workflow config from file path | `path` |
| `debug_query` | Run arbitrary Prolog code (rules + query) | `code` |
| `select_tasks` | Select tasks to trigger based on workflow rules | `tasks` |

### validate_workflow

```json
{
  "config_json": "{ \"statuses\": { ... }, \"scenarios\": { ... } }"
}
```

Returns validation result with `valid`, `conflicts`, `deadlocks`, `unreachable_scenarios`, `cycles`, and a human-readable `summary`.

### debug_query

Execute free-form Prolog for testing rules:

```json
{
  "code": "member(X, [a,b,c])."
}
```

### select_tasks

Feed current board tasks and get back which scenarios should trigger:

```json
{
  "tasks": [
    { "id": 42, "status": "In progress", "labels": ["bug"] }
  ]
}
```

## CLI Usage

Run a one-shot validation without MCP:

```bash
# Using --config flag (recommended)
go-prolog-mcp --config /path/to/config.json

# Using positional argument (backward compatible)
go-prolog-mcp /path/to/config.json
```

Example output:

```
OK: Workflow is valid -- no conflicts, deadlocks, cycles, or unreachable scenarios.
```

Or with issues found:

```
CONFLICTS (1):
  - from "In progress": "assign_reviewer" and "auto_merge" can both match the same item

DEADLOCKS (2):
  - "In review" -- no outgoing scenario
  - "Testing" -- no outgoing scenario

UNREACHABLE SCENARIOS (1):
  - "start_review"
```

## Config Structure

The server validates an orchestrator-style `config.json`:

```json
{
  "statuses": {
    "backlog": "Backlog",
    "in_progress": "In progress",
    "in_review": "In review",
    "done": "Done",
    "cancelled": "Cancelled"
  },
  "scenarios": {
    "start_work": {
      "type": "ai",
      "trigger_status": "Backlog",
      "next_status": "In progress",
      "required_label": "approved"
    },
    "request_review": {
      "type": "ai",
      "trigger_status": "In progress",
      "next_status": "In review",
      "required_labels": ["ready-for-review"],
      "without_labels": ["draft", "wip"]
    },
    "approve": {
      "type": "bridge",
      "trigger_status": "In review",
      "next_status": "Done"
    }
  }
}
```

### Schema

| Field | Type | Description |
|---|---|---|
| `statuses` | `map[string]string` | Board column names (key = internal, value = display) |
| `scenarios` | `map[string]object` | Transition rules between statuses |
| `type` | `string` | Handler type (ai, bridge, merge, shell) -- informational |
| `trigger_status` | `string` | Source status that triggers this scenario |
| `next_status` | `string` | Target status after scenario completes |
| `required_label` | `string` | Single label required (shorthand) |
| `required_labels` | `[]string` | All required labels (AND logic) |
| `without_labels` | `[]string` | Labels that block this scenario |

## Prolog Rules

The embedded `rules.pl` defines these validation predicates:

| Predicate | Purpose |
|---|---|
| `conflict(From, A, B)` | Two scenarios from same status can match the same item |
| `deadlock(Status)` | Status with no outgoing scenario (except "Done") |
| `unreachable(Name)` | Scenario whose trigger status is never entered by other scenarios |
| `cycle(Status)` | Status that can reach itself via a path |
| `can_trigger(IssueID, ScenarioName)` | Task matches a scenario's label requirements |

Label matching is **case-insensitive** (e.g. `"Bug"` matches `"bug"`).

## Development

### Prerequisites

- Go 1.25+

### Commands

```bash
# Build
go build -o go-prolog-mcp .

# Test
go test -v -count=1 ./...

# Lint
go vet ./...

# Run as MCP server
go-prolog-mcp

# Run as CLI
go-prolog-mcp --config testdata/config.json
```

## License

MIT
