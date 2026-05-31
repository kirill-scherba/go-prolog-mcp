// Go Prolog MCP — an MCP server for workflow verification using Prolog.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/kirill-scherba/go-prolog-mcp/prolog"
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
		{
			Tool: mcp.NewTool("debug_query",
				mcp.WithDescription("Run arbitrary Prolog code (rules + query). Returns all solutions."),
				mcp.WithString("code",
					mcp.Required(),
					mcp.Description("Prolog code including facts, rules and a query at the end (e.g. '?- goal(X).')"),
				),
			),
			Handler: handleDebugQuery,
		},
		{
			Tool: mcp.NewTool("select_tasks",
				mcp.WithDescription("Select tasks to trigger based on workflow rules. Returns a list of {IssueID, ScenarioName}."),
				mcp.WithString("config_path",
					mcp.Description("Absolute path to orchestrator config.json (optional, defaults to ~/.config/orchestrator-watchdog/config.json)"),
				),
				mcp.WithAny("tasks",
					mcp.Required(),
					mcp.Description("List of tasks: [{id: number, status: string, labels: [string]}]"),
				),
			),
			Handler: handleSelectTasks,
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

func handleDebugQuery(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := request.GetArguments()
	code, ok := args["code"].(string)
	if !ok || code == "" {
		return nil, fmt.Errorf("code is required")
	}

	parts := strings.Split(code, "?-")
	if len(parts) < 2 {
		return nil, fmt.Errorf("query starting with '?-' is required in the code")
	}

	program := strings.Join(parts[:len(parts)-1], "?-")
	queryStr := parts[len(parts)-1]
	queryStr = strings.TrimSpace(queryStr)
	if !strings.HasSuffix(queryStr, ".") {
		queryStr += "."
	}

	engine := prolog.New()
	if err := engine.LoadProgram(program); err != nil {
		return nil, fmt.Errorf("load program: %w", err)
	}

	results, err := engine.QueryRaw(queryStr)
	if err != nil {
		return nil, fmt.Errorf("query error: %w", err)
	}

	raw, _ := json.MarshalIndent(results, "", "  ")

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: string(raw),
			},
		},
	}, nil
}

func handleSelectTasks(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := request.GetArguments()
	path, _ := args["config_path"].(string)
	if path == "" {
		home, _ := os.UserHomeDir()
		path = home + "/go/src/github.com/kirill-scherba/orchestrator-watchdog/config.json"
	}

	tasksRaw, ok := args["tasks"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("tasks array is required")
	}

	var tasks []prolog.TaskFact
	for _, tr := range tasksRaw {
		m, ok := tr.(map[string]interface{})
		if !ok { continue }
		
		id, _ := m["id"].(float64)
		status, _ := m["status"].(string)
		labelsRaw, _ := m["labels"].([]interface{})
		
		var labels []string
		for _, lr := range labelsRaw {
			if s, ok := lr.(string); ok {
				labels = append(labels, s)
			}
		}

		tasks = append(tasks, prolog.TaskFact{
			ID: int(id), Status: status, Labels: labels,
		})
	}

	results, err := workflow.SelectTasksFile(path, tasks)
	if err != nil {
		return nil, fmt.Errorf("selection error: %w", err)
	}

	raw, _ := json.MarshalIndent(results, "", "  ")

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: string(raw),
			},
		},
	}, nil
}

func resultToToolResult(result *workflow.ValidationResult) *mcp.CallToolResult {
	data := map[string]interface{}{
		"valid":       result.Valid,
		"summary":     result.String(),
		"conflicts":   result.Conflicts,
		"deadlocks":   result.Deadlocks,
		"unreachable": result.Unreachable,
		"cycles":      result.Cycles,
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
