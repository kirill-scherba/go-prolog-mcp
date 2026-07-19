// Go Prolog MCP — an MCP server for workflow verification using Prolog.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/kirill-scherba/go-prolog-mcp/prolog"
	"github.com/kirill-scherba/go-prolog-mcp/workflow"
)

// defaultMemoryStoreURL is set from the --memory-store-url CLI flag and used
// by handleMemoryVerify when auto-fetching facts.
var defaultMemoryStoreURL string

func main() {
	// CLI flag for config path.
	configPath := flag.String("config", "", "Path to orchestrator config.json for CLI validation")
	// CLI flag for memory-store-mcp URL, used by memory_verify when auto-fetching facts.
	memoryStoreURL := flag.String("memory-store-url", prolog.DefaultMemoryStoreURL, "URL of memory-store-mcp for auto-fetching facts in memory_verify")
	flag.Parse()

	defaultMemoryStoreURL = *memoryStoreURL

	// Also accept positional argument for backward compatibility.
	path := *configPath
	if path == "" && flag.NArg() > 0 {
		path = flag.Arg(0)
	}

	if path != "" {
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
		{
			Tool: mcp.NewTool("memory_verify",
				mcp.WithDescription("Verify memory facts for contradictions and anomalies using Prolog rules. "+
					"When called without 'facts', auto-fetches all facts from memory-store-mcp. "+
					"When called with 'query', searches memory by keyword and checks for contradictions. "+
					"Returns contradictions, anomalies, and summary stats."),
				mcp.WithAny("facts",
					mcp.Description("List of memory facts: [{key, category, attr, value}]. "+
						"When omitted, facts are auto-fetched from memory-store-mcp."),
				),
				mcp.WithString("query",
					mcp.Description("Keyword to search memory for (uses memory_find). "+
						"All matching entries are collected under one key and checked for contradictions."),
				),
				mcp.WithString("rules",
					mcp.Description("Optional custom Prolog rules to add or override defaults"),
				),
				mcp.WithString("memory_store_url",
					mcp.Description("URL of memory-store-mcp (default: --memory-store-url flag)"),
				),
			),
			Handler: handleMemoryVerify,
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

func handleMemoryVerify(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := request.GetArguments()

	var facts []prolog.MemoryFact
	var fetchErr error

	if query, ok := args["query"].(string); ok && query != "" {
		// Query mode: search memory by keyword, collect under one key.
		url := stringArg(args, "memory_store_url")
		if url == "" {
			url = defaultMemoryStoreURL
		}
		facts, fetchErr = prolog.FetchMemoryFactsByQuery(ctx, url, query)
		if fetchErr != nil {
			return nil, fmt.Errorf("query memory facts: %w", fetchErr)
		}
	} else if factsRaw, ok := args["facts"].([]interface{}); ok {
		// Existing manual path.
		for _, fr := range factsRaw {
			m, ok := fr.(map[string]interface{})
			if !ok {
				continue
			}
			facts = append(facts, prolog.MemoryFact{
				Key:      stringField(m, "key"),
				Category: stringField(m, "category"),
				Attr:     stringField(m, "attr"),
				Value:    stringField(m, "value"),
			})
		}
	} else {
		// Auto-fetch path (all memory).
		url := stringArg(args, "memory_store_url")
		if url == "" {
			url = defaultMemoryStoreURL
		}

		facts, fetchErr = prolog.FetchMemoryFacts(ctx, url)
		if fetchErr != nil {
			return nil, fmt.Errorf("auto-fetch memory facts: %w", fetchErr)
		}
	}

	customRules, _ := args["rules"].(string)

	result, err := prolog.VerifyMemory(facts, customRules)
	if err != nil {
		return nil, fmt.Errorf("memory verify error: %w", err)
	}

	raw, _ := json.MarshalIndent(result, "", "  ")
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: string(raw),
			},
		},
	}, nil
}

func stringField(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// stringArg extracts a string argument from the tool request argument map.
func stringArg(args map[string]interface{}, key string) string {
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
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
