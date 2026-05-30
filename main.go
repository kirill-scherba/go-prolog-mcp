// Go Prolog MCP — an MCP server for workflow verification using Prolog.
//
// Uses ichiban/prolog (embeddable ISO Prolog in Go) to validate orchestrator
// workflow configurations. Detects conflicts, deadlocks, unreachable scenarios,
// and cycles in the workflow graph.
//
// MCP Tools:
//   - validate_workflow       — validate a workflow from JSON config
//   - validate_workflow_file  — validate from a file path
//   - workflow_query          — run arbitrary Prolog queries against facts
package main

import (
	"log"
	"os"

	"github.com/kirill-scherba/go-prolog-mcp/mcp"
	"github.com/kirill-scherba/go-prolog-mcp/workflow"
)

func main() {
	// First argument (optional) is config path for file-based usage.
	if len(os.Args) > 1 {
		path := os.Args[1]

		result, err := workflow.ValidateFile(path)
		if err != nil {
			log.Fatalf("ERROR: %v", err)
		}

		log.Println(result.String())
		return
	}

	// Run as MCP server (stdio transport).
	server := mcp.New()
	if err := server.Run(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
