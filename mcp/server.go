// Package mcp implements the MCP server running the Go Prolog workflow verifier.
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/kirill-scherba/go-prolog-mcp/workflow"
)

// JSON-RPC message structures.
type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *rpcError   `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type toolDef struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema inputSchema `json:"inputSchema"`
}

type inputSchema struct {
	Type       string                    `json:"type"`
	Properties map[string]propertySchema `json:"properties"`
}

type propertySchema struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

// Server is the MCP stdio server.
type Server struct {
	reader *bufio.Scanner
}

// New creates a new MCP server reading from stdin.
func New() *Server {
	return &Server{
		reader: bufio.NewScanner(os.Stdin),
	}
}

// Run starts the MCP server event loop.
func (s *Server) Run() error {
	log.Println("go-prolog-mcp starting")

	for s.reader.Scan() {
		line := s.reader.Text()
		if line == "" {
			continue
		}

		var req request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			log.Printf("invalid JSON-RPC: %v", err)
			continue
		}

		s.handleRequest(req)
	}

	if err := s.reader.Err(); err != nil {
		return fmt.Errorf("stdin: %w", err)
	}

	return nil
}

func (s *Server) handleRequest(req request) {
	// Notifications (no ID) — must NOT send a response per JSON-RPC spec.
	// Check both nil (field absent) and explicit JSON null.
	if req.ID == nil || string(req.ID) == "null" {
		return
	}

	switch req.Method {
	case "initialize":
		s.handleInitialize(req)
	case "tools/list":
		s.handleToolList(req)
	case "tools/call":
		s.handleToolCall(req)
	default:
		s.writeError(req.ID, -32601, fmt.Sprintf("Method not found: %s", req.Method))
	}
}

func (s *Server) handleInitialize(req request) {
	s.writeResponse(response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"protocolVersion": "1.0",
			"capabilities": map[string]interface{}{
				"tools": map[string]bool{"listChanged": true},
			},
			"serverInfo": map[string]string{
				"name":    "go-prolog-mcp",
				"version": "0.1.0",
			},
		},
	})
}

func (s *Server) handleToolList(req request) {
	tools := []toolDef{
		{
			Name:        "validate_workflow",
			Description: "Validate an orchestrator workflow configuration from JSON string. Returns conflicts, deadlocks, unreachable scenarios, and cycles.",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]propertySchema{
					"config_json": {
						Type:        "string",
						Description: "Full orchestrator config.json content as a JSON string",
					},
				},
			},
		},
		{
			Name:        "validate_workflow_file",
			Description: "Validate an orchestrator workflow configuration from a file path.",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]propertySchema{
					"path": {
						Type:        "string",
						Description: "Absolute path to the orchestrator config.json file",
					},
				},
			},
		},
	}

	s.writeResponse(response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"tools": tools,
		},
	})
}

func (s *Server) handleToolCall(req request) {
	var params struct {
		Name   string          `json:"name"`
		Params json.RawMessage `json:"arguments"`
	}

	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.writeError(req.ID, -32602, fmt.Sprintf("Invalid params: %v", err))
		return
	}

	switch params.Name {
	case "validate_workflow":
		s.handleValidateWorkflow(req.ID, params.Params)
	case "validate_workflow_file":
		s.handleValidateWorkflowFile(req.ID, params.Params)
	default:
		s.writeError(req.ID, -32601, fmt.Sprintf("Tool not found: %s", params.Name))
	}
}

func (s *Server) handleValidateWorkflow(id json.RawMessage, rawArgs json.RawMessage) {
	var args struct {
		ConfigJSON string `json:"config_json"`
	}

	if err := json.Unmarshal(rawArgs, &args); err != nil {
		s.writeError(id, -32602, fmt.Sprintf("Invalid arguments: %v", err))
		return
	}

	result, err := workflow.ValidateConfig(args.ConfigJSON)
	if err != nil {
		s.writeError(id, -32603, fmt.Sprintf("Validation error: %v", err))
		return
	}

	s.writeResponse(response{
		JSONRPC: "2.0",
		ID:      id,
		Result: map[string]interface{}{
			"valid":              result.Valid,
			"summary":            result.String(),
			"conflicts":          result.Conflicts,
			"deadlocks":          result.Deadlocks,
			"unreachable":        result.Unreachable,
			"cycles":             result.Cycles,
		},
	})
}

func (s *Server) handleValidateWorkflowFile(id json.RawMessage, rawArgs json.RawMessage) {
	var args struct {
		Path string `json:"path"`
	}

	if err := json.Unmarshal(rawArgs, &args); err != nil {
		s.writeError(id, -32602, fmt.Sprintf("Invalid arguments: %v", err))
		return
	}

	result, err := workflow.ValidateFile(args.Path)
	if err != nil {
		s.writeError(id, -32603, fmt.Sprintf("Validation error: %v", err))
		return
	}

	s.writeResponse(response{
		JSONRPC: "2.0",
		ID:      id,
		Result: map[string]interface{}{
			"valid":              result.Valid,
			"summary":            result.String(),
			"conflicts":          result.Conflicts,
			"deadlocks":          result.Deadlocks,
			"unreachable":        result.Unreachable,
			"cycles":             result.Cycles,
		},
	})
}

func (s *Server) writeResponse(resp response) {
	data, _ := json.Marshal(resp)
	fmt.Println(string(data))
}

func (s *Server) writeError(id json.RawMessage, code int, message string) {
	s.writeResponse(response{
		JSONRPC: "2.0",
		ID:      id,
		Error: &rpcError{
			Code:    code,
			Message: message,
		},
	})
}

func init() {
	log.SetFlags(0)
	log.SetPrefix("[go-prolog-mcp] ")
}
