// Go Prolog MCP — an MCP server for workflow verification using Prolog.
//
// Uses ichiban/prolog (embeddable ISO Prolog in Go) to validate orchestrator
// workflow configurations. Detects conflicts, deadlocks, unreachable scenarios,
// and cycles in the workflow graph.
//
// MCP Tools:
//   - validate_workflow       — validate a workflow from JSON config
//   - validate_workflow_file  — validate from a file path
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/kirill-scherba/go-prolog-mcp/workflow"
)

func main() {
	// First argument (optional) is config path for CLI usage.
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
	s := server.NewMCPServer(
		"go-prolog-mcp",
		"0.1.0",
	)

	tools := []server.ServerTool{
		{
			Tool:    mcp.NewTool("validate_workflow",
				mcp.WithDescription("Validate an orchestrator workflow configuration from JSON string. Returns conflicts, deadlocks, unreachable scenarios, and cycles."),
				mcp.WithString("config_json",
					mcp.Required(),
					mcp.Description("Full orchestrator config.json content as a JSON string"),
				),
			),
			Handler: handleValidateWorkflow,
		},
		{
			Tool: mcp.NewTool("validate_workflow_file",
				mcp.WithDescription("Validate an orchestrator workflow configuration from a file path."),
				mcp.WithString("path",
					mcp.Required(),
					mcp.Description("Absolute path to the orchestrator config.json file"),
				),
			),
			Handler: handleValidateWorkflowFile,
		},
	}

	s.AddTools(tools...)

	if err := server.ServeStdio(s); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func handleValidateWorkflow(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := request.GetArguments()
	configJSON, ok := args["config_json"].(string)
	if !ok || configJSON == "" {
		return nil, fmt.Errorf("config_json is required")
	}

	result, err := workflow.ValidateConfig(configJSON)
	if err != nil {
		return nil, fmt.Errorf("validation error: %w", err)
	}

	return resultToToolResult(result), nil
}

func handleValidateWorkflowFile(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := request.GetArguments()
	path, ok := args["path"].(string)
	if !ok || path == "" {
		return nil, fmt.Errorf("path is required")
	}

	result, err := workflow.ValidateFile(path)
	if err != nil {
		return nil, fmt.Errorf("validation error: %w", err)
	}

	return resultToToolResult(result), nil
}

func resultToToolResult(result *workflow.ValidationResult) *mcp.CallToolResult {
	data := map[string]interface{}{
		"valid":              result.Valid,
		"summary":            result.String(),
		"conflicts":          result.Conflicts,
		"deadlocks":          result.Deadlocks,
		"unreachable":        result.Unreachable,
		"cycles":             result.Cycles,
	}

	raw, _ := json.MarshalIndent(data, "", "  ")

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: string(raw),
			},
		},
	}
}
