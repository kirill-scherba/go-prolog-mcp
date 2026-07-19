package mcpclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// mcpResponse builds a JSON-RPC 2.0 response wrapping the given tool result.
func mcpResponse(toolResult map[string]interface{}) []byte {
	payload, _ := json.Marshal(toolResult)
	resp := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"result": map[string]interface{}{
			"content": []map[string]interface{}{
				{"type": "text", "text": string(payload)},
			},
			"isError": false,
		},
	}
	body, _ := json.Marshal(resp)
	return body
}

func TestClient_StreamableHTTP(t *testing.T) {
	var gotName string
	var gotArgs map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		contentType := r.Header.Get("Content-Type")
		if contentType != "application/json" {
			t.Errorf("expected Content-Type application/json, got %q", contentType)
		}

		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		params, _ := req["params"].(map[string]interface{})
		gotName, _ = params["name"].(string)
		gotArgs, _ = params["arguments"].(map[string]interface{})

		w.Header().Set("Content-Type", "application/json")
		w.Write(mcpResponse(map[string]interface{}{"keys": []string{"a", "b"}}))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 5*time.Second)
	defer c.Close()

	result, err := c.CallTool(context.Background(), "memory_list", map[string]interface{}{"prefix": "memory/"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	if gotName != "memory_list" {
		t.Errorf("got name %q, want memory_list", gotName)
	}
	if gotArgs["prefix"] != "memory/" {
		t.Errorf("got args %+v, want prefix=memory/", gotArgs)
	}

	keys, ok := result["keys"].([]interface{})
	if !ok || len(keys) != 2 {
		t.Errorf("expected 2 keys, got %+v", result)
	}
}

func TestClient_ConnectionRefused(t *testing.T) {
	// Use a port that is extremely unlikely to be open.
	c := NewClient("http://127.0.0.1:1", 2*time.Second)
	defer c.Close()

	_, err := c.CallTool(context.Background(), "memory_list", nil)
	if err == nil {
		t.Fatal("expected error for unreachable server")
	}

	var refused *ErrConnectionRefused
	if !errors.As(err, &refused) {
		t.Errorf("expected ErrConnectionRefused, got %T: %v", err, err)
	}
}

func TestClient_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Write(mcpResponse(map[string]interface{}{}))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 50*time.Millisecond)
	defer c.Close()

	_, err := c.CallTool(context.Background(), "memory_list", nil)
	if err == nil {
		t.Fatal("expected timeout error")
	}

	var timeoutErr *ErrTimeout
	if !errors.As(err, &timeoutErr) {
		t.Errorf("expected ErrTimeout, got %T: %v", err, err)
	}
}

func TestClient_MCPErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"error": map[string]interface{}{
				"code":    -32602,
				"message": "unknown tool",
			},
		}
		body, _ := json.Marshal(resp)
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 5*time.Second)
	defer c.Close()

	_, err := c.CallTool(context.Background(), "unknown_tool", nil)
	if err == nil {
		t.Fatal("expected mcp error")
	}

	var mcpErr *ErrMCPError
	if !errors.As(err, &mcpErr) {
		t.Errorf("expected ErrMCPError, got %T: %v", err, err)
	} else if mcpErr.Code != -32602 {
		t.Errorf("expected code -32602, got %d", mcpErr.Code)
	}
}

func TestClient_SSEFallback(t *testing.T) {
	var postEndpoint string

	mux := http.NewServeMux()
	// Root endpoint serves SSE stream on GET and returns 405 on POST.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", postEndpoint)
			w.(http.Flusher).Flush()
			// Keep connection open briefly.
			time.Sleep(50 * time.Millisecond)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	})
	// Dedicated POST endpoint for messages.
	mux.HandleFunc("/message", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(mcpResponse(map[string]interface{}{"value": "sse-works"}))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()
	postEndpoint = srv.URL + "/message"

	c := NewClient(srv.URL, 5*time.Second)
	defer c.Close()

	result, err := c.CallTool(context.Background(), "memory_get", map[string]interface{}{"key": "x"})
	if err != nil {
		t.Fatalf("CallTool via SSE: %v", err)
	}
	if result["value"] != "sse-works" {
		t.Errorf("unexpected result: %+v", result)
	}
}

func TestClient_DirectPostReturnsToolError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]interface{}{
				"content": []map[string]interface{}{
					{"type": "text", "text": "it failed"},
				},
				"isError": true,
			},
		}
		body, _ := json.Marshal(resp)
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 5*time.Second)
	defer c.Close()

	_, err := c.CallTool(context.Background(), "memory_get", nil)
	if err == nil {
		t.Fatal("expected error for isError=true")
	}
	if !strings.Contains(err.Error(), "mcp tool error") {
		t.Errorf("unexpected error message: %v", err)
	}
}


