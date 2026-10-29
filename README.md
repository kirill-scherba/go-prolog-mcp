# go-prolog-mcp

[![Go Version](https://img.shields.io/github/go-mod/go-version/kirill-scherba/go-prolog-mcp)](go.mod)
[![License](https://img.shields.io/github/license/kirill-scherba/go-prolog-mcp)](#license)

An MCP server for orchestrator workflow validation using an embedded Prolog engine. Evaluates workflow configurations for conflicts, deadlocks, unreachable scenarios, and cycles -- then selects tasks to trigger based on rule matching.

Built with [ichiban/prolog](https://github.com/ichiban/prolog) and the [MCP Go SDK](https://github.com/mark3labs/mcp-go).

## Install in AnyText

One binary for Linux and Windows; Go only, nothing to install next to it.

1. AnyText → Настройки → MCP → «Доступные серверы» → **Prolog engine** → «Установить».
2. No settings are needed: the engine keeps its state in memory and asks nothing.
3. The tools appear in the chat: `debug_query`, `validate_workflow`, `validate_workflow_file`,
   `select_tasks`, `memory_verify`.

## Architecture

The server runs over **stdio transport** (standard MCP protocol) and can also be used as a standalone CLI tool for one-shot validation.

```
main.go (MCP server + CLI)
  |
  +-- mcpclient/        Minimal MCP JSON-RPC HTTP client
  |     client.go         MCP client with Streamable HTTP + SSE fallback
  |
  +-- workflow/           Config loading and validation orchestration
  |     facts.go          Config parsing, Prolog fact generation
  |     engine.go         Engine constructors
  |     validator.go      Validate() entry point, result formatting
  |
  +-- prolog/             Prolog engine wrapper
        engine.go         Engine, program builder, query methods
        rules.pl          Embedded Prolog validation rules
        memory_rules.pl   Embedded memory consistency rules
        memory_verify.go  Memory verification types and queries
        memory_fetcher.go Auto-fetch facts from memory-store-mcp
```

## MCP Tools

| Tool | Description | Required Params |
|---|---|---|
| `validate_workflow` | Validate a workflow config from JSON string | `config_json` |
| `validate_workflow_file` | Validate a workflow config from file path | `path` |
| `debug_query` | Run arbitrary Prolog code (rules + query) | `code` |
| `select_tasks` | Select tasks to trigger based on workflow rules | `tasks` |
| `memory_verify` | Verify memory facts for contradictions and anomalies | `facts` (optional), `rules` (optional), `memory_store_url` (optional) |

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

### memory_verify

Check memory facts for contradictions and anomalies. Optional `rules` can extend or override defaults.

When `facts` is omitted, the tool automatically connects to **memory-store-mcp** (via the `--memory-store-url` flag or the `memory_store_url` parameter), fetches all keys under the `memory/` prefix, normalizes them into facts, and runs the rules.

#### Manual mode

```json
{
  "facts": [
    { "key": "feature-x", "category": "feature", "attr": "status", "value": "completed" },
    { "key": "feature-x", "category": "docs", "attr": "status", "value": "stale" }
  ]
}
```

#### Auto-fetch mode

```json
{
  "memory_store_url": "http://localhost:7711/memory-store-mcp"
}
```

If the URL is omitted, the default is `http://localhost:7711/memory-store-mcp`.

Returns:

```json
{
  "contradictions": [
    { "key": "feature-x", "type": "completed-vs-stale-docs" }
  ],
  "anomalies": [],
  "stats": {
    "facts_checked": 2,
    "contradictions_found": 1,
    "anomalies_found": 0,
    "rules_applied": 4
  }
}
```

The runtime injects `today('YYYY-MM-DD').` so date-aware rules can compare ISO 8601 deadlines.

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
✅ Workflow is valid — no conflicts, deadlocks, cycles, or unreachable scenarios.
```

Or with issues found:

```
⚡ CONFLICTS (1):
  • from "In progress": "assign_reviewer" and "auto_merge" can both match the same item

💀 DEADLOCKS (2):
  • "In review" — no outgoing scenario
  • "Testing" — no outgoing scenario

🚫 UNREACHABLE SCENARIOS (1):
  • "start_review"
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

### Memory consistency rules

The embedded `memory_rules.pl` defines these predicates for the `memory_verify` tool:

| Predicate | Purpose |
|---|---|
| `contradiction(Key, Type)` | Strict memory conflict for the given key |
| `anomaly(Key, Type)` | Suspicious memory state for the given key |

Built-in anomaly types include `completed-vs-stale-docs`, `expired-goal`, `conflicting-results`, and `baron-idle`.

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

# Run as MCP server pointing to a custom memory-store-mcp
go-prolog-mcp --memory-store-url http://localhost:7711/memory-store-mcp

# Run as CLI
go-prolog-mcp --config testdata/config.json
```

## License

MIT
