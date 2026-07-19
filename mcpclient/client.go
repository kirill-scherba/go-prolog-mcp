// Package mcpclient provides a minimal MCP JSON-RPC HTTP client for calling
// tools on a remote MCP server. It is intentionally small and uses only the
// Go standard library so that go-prolog-mcp does not gain extra dependencies.
package mcpclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ErrConnectionRefused is returned when the HTTP server cannot be reached.
type ErrConnectionRefused struct {
	URL string
	Err error
}

func (e *ErrConnectionRefused) Error() string {
	return fmt.Sprintf("connection refused to %s: %v", e.URL, e.Err)
}

func (e *ErrConnectionRefused) Unwrap() error { return e.Err }

// ErrTimeout is returned when an HTTP request exceeds the configured timeout.
type ErrTimeout struct {
	URL     string
	Timeout time.Duration
}

func (e *ErrTimeout) Error() string {
	return fmt.Sprintf("request to %s timed out after %s", e.URL, e.Timeout)
}

// ErrMCPError is returned when the MCP server responds with a JSON-RPC error.
type ErrMCPError struct {
	Code    int
	Message string
	Data    interface{}
}

func (e *ErrMCPError) Error() string {
	return fmt.Sprintf("mcp error %d: %s", e.Code, e.Message)
}

// Client is a lightweight MCP JSON-RPC HTTP client.
type Client struct {
	url        string
	httpClient *http.Client
	mu         sync.Mutex
	id         int64

	// SSE transport state, discovered lazily.
	sseOnce    sync.Once
	ssePostURL string
	sseErr     error
}

// NewClient creates an MCP client for the given URL.
// If timeout is zero, a default of 30 seconds is used.
func NewClient(url string, timeout time.Duration) *Client {
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		url:        strings.TrimSuffix(url, "/"),
		httpClient: &http.Client{Timeout: timeout},
	}
}

// Close releases resources. For the basic HTTP client this is a no-op.
func (c *Client) Close() error {
	return nil
}

// nextID returns a monotonically increasing JSON-RPC request id.
func (c *Client) nextID() int64 {
	return atomic.AddInt64(&c.id, 1)
}

// CallTool invokes a tool on the remote MCP server and returns the parsed
// JSON result. It first tries a direct JSON-RPC POST (Streamable HTTP) and,
// if that is rejected, falls back to an SSE transport.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]interface{}) (map[string]interface{}, error) {
	text, err := c.CallToolRaw(ctx, name, args)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return nil, fmt.Errorf("decode tool result payload: %w (payload: %q)", err, text)
	}
	return result, nil
}

// CallToolRaw invokes a tool and returns the raw text content from the MCP
// CallToolResult envelope. Use this when the tool returns plain text (e.g.
// memory_store-mcp's memory_list) rather than a JSON object.
func (c *Client) CallToolRaw(ctx context.Context, name string, args map[string]interface{}) (string, error) {
	text, directErr := c.callToolRawDirect(ctx, name, args)
	if directErr == nil {
		return text, nil
	}

	// Some MCP servers expose tools only through SSE. Discover the SSE post
	// endpoint lazily and retry once.
	c.sseOnce.Do(func() { c.sseErr = c.discoverSSEEndpoint(ctx) })
	if c.sseErr != nil {
		return "", fmt.Errorf("direct call failed: %w; sse fallback failed: %v", directErr, c.sseErr)
	}
	return c.callToolRawSSE(ctx, name, args)
}

// callToolRawDirect sends a JSON-RPC 2.0 tools/call request via HTTP POST and
// returns the raw text content of the result.
func (c *Client) callToolRawDirect(ctx context.Context, name string, args map[string]interface{}) (string, error) {
	body, err := c.buildRequestBody(name, args)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", classifyNetworkError(c.url, c.httpClient.Timeout, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusNotAcceptable {
		// Signal to the caller that SSE may be required.
		return "", fmt.Errorf("http %d: direct post not supported", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("http %d: %s", resp.StatusCode, string(body))
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response body: %w", err)
	}

	return extractTextFromJSONRPCResponse(respBody)
}

// discoverSSEEndpoint establishes an SSE stream and extracts the POST endpoint.
func (c *Client) discoverSSEEndpoint(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return fmt.Errorf("create sse request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return classifyNetworkError(c.url, c.httpClient.Timeout, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("sse endpoint returned http %d: %s", resp.StatusCode, string(body))
	}

	// Read events until we find the endpoint event.
	scanner := bufio.NewScanner(resp.Body)
	var postURL string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: endpoint") {
			// Next line should be "data: <url>".
			if scanner.Scan() {
				dataLine := scanner.Text()
				const prefix = "data: "
				if strings.HasPrefix(dataLine, prefix) {
					postURL = strings.TrimPrefix(dataLine, prefix)
					break
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read sse stream: %w", err)
	}
	if postURL == "" {
		return errors.New("no endpoint event in sse stream")
	}

	c.mu.Lock()
	c.ssePostURL = postURL
	c.mu.Unlock()
	return nil
}

// callToolRawSSE sends a tools/call request through a previously discovered
// SSE post endpoint and returns the raw text content.
func (c *Client) callToolRawSSE(ctx context.Context, name string, args map[string]interface{}) (string, error) {
	c.mu.Lock()
	postURL := c.ssePostURL
	c.mu.Unlock()
	if postURL == "" {
		return "", errors.New("sse post url not discovered")
	}

	body, err := c.buildRequestBody(name, args)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, postURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create sse request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", classifyNetworkError(postURL, c.httpClient.Timeout, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read sse response body: %w", err)
	}

	return extractTextFromJSONRPCResponse(respBody)
}

// buildRequestBody creates a JSON-RPC 2.0 tools/call request body.
func (c *Client) buildRequestBody(name string, args map[string]interface{}) ([]byte, error) {
	payload := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      name,
			"arguments": args,
		},
		"id": c.nextID(),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal jsonrpc request: %w", err)
	}
	return body, nil
}

// extractTextFromJSONRPCResponse decodes a JSON-RPC response and returns the
// aggregated text content from the standard CallToolResult envelope.
func extractTextFromJSONRPCResponse(body []byte) (string, error) {
	var envelope struct {
		JSONRPC string      `json:"jsonrpc"`
		ID      interface{} `json:"id"`
		Result  *struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
		Error *ErrMCPError `json:"error"`
	}

	if err := json.Unmarshal(body, &envelope); err != nil {
		return "", fmt.Errorf("decode jsonrpc response: %w", err)
	}

	if envelope.Error != nil {
		return "", envelope.Error
	}

	if envelope.Result == nil {
		return "", errors.New("empty jsonrpc result")
	}

	if envelope.Result.IsError {
		// The tool itself reported an error. Return the text content as the
		// error message so callers can decide how to handle it.
		var parts []string
		for _, c := range envelope.Result.Content {
			if c.Type == "text" {
				parts = append(parts, c.Text)
			}
		}
		return "", fmt.Errorf("mcp tool error: %s", strings.Join(parts, ""))
	}

	if len(envelope.Result.Content) == 0 {
		return "", errors.New("mcp tool result has no content")
	}

	var textParts []string
	for _, c := range envelope.Result.Content {
		if c.Type == "text" {
			textParts = append(textParts, c.Text)
		}
	}
	if len(textParts) == 0 {
		return "", errors.New("mcp tool result has no text content")
	}

	return strings.Join(textParts, ""), nil
}

// classifyNetworkError converts net/http errors into typed, user-friendly errors.
func classifyNetworkError(url string, timeout time.Duration, err error) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return &ErrTimeout{URL: url, Timeout: timeout}
	}
	if strings.Contains(err.Error(), "connection refused") {
		return &ErrConnectionRefused{URL: url, Err: err}
	}
	if strings.Contains(err.Error(), "Client.Timeout") || strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "Timeout") {
		return &ErrTimeout{URL: url, Timeout: timeout}
	}
	return err
}
