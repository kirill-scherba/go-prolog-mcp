package prolog

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"
)

// mcpTextResponse builds a JSON-RPC 2.0 envelope wrapping plain text content.
func mcpTextResponse(text string) []byte {
	resp := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"result": map[string]interface{}{
			"content": []map[string]interface{}{{"type": "text", "text": text}},
			"isError": false,
		},
	}
	body, _ := json.Marshal(resp)
	return body
}

// mcpJSONResponse builds a JSON-RPC 2.0 envelope wrapping a JSON value.
func mcpJSONResponse(value interface{}) []byte {
	payload, _ := json.Marshal(value)
	return mcpTextResponse(string(payload))
}

// mcpErrorResponse builds a JSON-RPC 2.0 envelope with isError=true.
func mcpErrorResponse(text string) []byte {
	resp := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"result": map[string]interface{}{
			"content": []map[string]interface{}{{"type": "text", "text": text}},
			"isError": true,
		},
	}
	body, _ := json.Marshal(resp)
	return body
}

// memoryValueWrapper returns the standard memory_get wrapper object.
func memoryValueWrapper(content interface{}) map[string]interface{} {
	contentJSON, _ := json.Marshal(content)
	return map[string]interface{}{
		"content":   string(contentJSON),
		"timestamp": "2026-07-19T13:45:18Z",
	}
}

func TestNormalizeMemoryEntry_ProjectKey(t *testing.T) {
	key := "memory/project/cooksy/architecture/overview"
	value := memoryValueWrapper(map[string]interface{}{
		"summary":  "system design",
		"status":   "completed",
		"priority": 5,
		"tags":     []interface{}{"mcp", "prolog"},
	})

	facts := NormalizeMemoryEntry(key, value)

	if len(facts) == 0 {
		t.Fatal("expected facts, got none")
	}

	if !hasFact(facts, "architecture/overview", "architecture", "status", "completed") {
		t.Errorf("missing architecture category fact, got %+v", facts)
	}
	if !hasFact(facts, "architecture/overview", "project", "status", "completed") {
		t.Errorf("missing project scope fact, got %+v", facts)
	}
	if !hasFact(facts, "architecture/overview", "architecture", "tags", "mcp,prolog") {
		t.Errorf("missing joined tags fact, got %+v", facts)
	}
}

func TestNormalizeMemoryEntry_KnownCategory(t *testing.T) {
	key := "memory/project/go-prolog-mcp/docs/overview"
	value := memoryValueWrapper(map[string]interface{}{
		"status": "stale",
	})

	facts := NormalizeMemoryEntry(key, value)

	if !hasFact(facts, "docs/overview", "docs", "status", "stale") {
		t.Errorf("expected docs category fact, got %+v", facts)
	}
}

func TestNormalizeMemoryEntry_GoalKey(t *testing.T) {
	key := "memory/project/orchestrator/tracker/million-dollars"
	value := memoryValueWrapper(map[string]interface{}{
		"status":   "active",
		"deadline": "2026-06-30",
		"progress": 0,
		"assignee": "baron",
	})

	facts := NormalizeMemoryEntry(key, value)

	if !hasFact(facts, "tracker/million-dollars", "project", "status", "active") {
		t.Errorf("missing project status fact, got %+v", facts)
	}
	if !hasFact(facts, "tracker/million-dollars", "project", "deadline", "2026-06-30") {
		t.Errorf("missing project deadline fact, got %+v", facts)
	}
	if !hasFact(facts, "tracker/million-dollars", "project", "progress", "0") {
		t.Errorf("missing stringified progress fact, got %+v", facts)
	}
}

func TestNormalizeMemoryEntry_NestedObjectSkipped(t *testing.T) {
	key := "memory/project/x/feature"
	value := memoryValueWrapper(map[string]interface{}{
		"nested": map[string]interface{}{"foo": "bar"},
	})

	facts := NormalizeMemoryEntry(key, value)

	for _, f := range facts {
		if f.Attr == "nested" && f.Value != "" {
			t.Errorf("nested object should produce empty value, got %+v", f)
		}
	}
}

func TestNormalizeMemoryEntry_NonObjectValue(t *testing.T) {
	facts := NormalizeMemoryEntry("memory/project/x/y", "plain string")
	if len(facts) != 1 {
		t.Errorf("expected 1 raw-content fact, got %+v", facts)
	}
	if !hasFact(facts, "x/y", "project", "content", "plain string") {
		t.Errorf("missing raw content fact, got %+v", facts)
	}
}

func TestNormalizeMemoryEntry_KeyPathEdgeCases(t *testing.T) {
	cases := []struct {
		key          string
		wantShortKey string
		wantCategory string
	}{
		{"memory/user/kirill/preferences", "kirill/preferences", "user"},
		{"memory/technical/deployment/k8s", "deployment/k8s", "technical"},
		{"memory/auto/2026-07-19/abc123", "2026-07-19/abc123", "auto"},
	}

	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			facts := NormalizeMemoryEntry(tc.key, memoryValueWrapper(map[string]interface{}{"status": "ok"}))
			if !hasFact(facts, tc.wantShortKey, tc.wantCategory, "status", "ok") {
				t.Errorf("expected category=%s shortKey=%s, got %+v", tc.wantCategory, tc.wantShortKey, facts)
			}
		})
	}
}

func TestParseMemoryListText(t *testing.T) {
	memories := map[string]interface{}{
		"memory/project/x/feature-a": memoryValueWrapper(map[string]interface{}{"status": "completed"}),
		"memory/project/x/docs/a":   memoryValueWrapper(map[string]interface{}{"status": "stale"}),
		"memory/project/y/z":        memoryValueWrapper(map[string]interface{}{"status": "ok"}),
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		params, _ := req["params"].(map[string]interface{})
		name, _ := params["name"].(string)

		w.Header().Set("Content-Type", "application/json")
		switch name {
		case "memory_list":
			w.Write(mcpTextResponse("Found 3 memories:\nmemory/project/x/feature-a\nmemory/project/x/docs/a\nmemory/project/y/z"))
		case "memory_get":
			args, _ := params["arguments"].(map[string]interface{})
			key, _ := args["key"].(string)
			value, ok := memories[key]
			if !ok {
				w.Write(mcpErrorResponse("Error: Key not found"))
				return
			}
			w.Write(mcpJSONResponse(value))
		default:
			http.Error(w, "unknown tool", http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	facts, err := FetchMemoryFacts(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("FetchMemoryFacts: %v", err)
	}

	if len(facts) == 0 {
		t.Fatal("expected facts from parsed list")
	}
}

func TestFetchMemoryFacts_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(mcpTextResponse("No memories found."))
	}))
	defer srv.Close()

	facts, err := FetchMemoryFacts(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("FetchMemoryFacts: %v", err)
	}
	if len(facts) != 0 {
		t.Errorf("expected 0 facts, got %d", len(facts))
	}
}

func TestFetchMemoryFacts_TwoEntries(t *testing.T) {
	memories := map[string]interface{}{
		"memory/project/x/feature-a": memoryValueWrapper(map[string]interface{}{"status": "completed"}),
		"memory/project/x/docs/a":     memoryValueWrapper(map[string]interface{}{"status": "stale"}),
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		params, _ := req["params"].(map[string]interface{})
		name, _ := params["name"].(string)

		w.Header().Set("Content-Type", "application/json")
		switch name {
		case "memory_list":
			w.Write(mcpTextResponse("Found 2 memories:\nmemory/project/x/feature-a\nmemory/project/x/docs/a"))
		case "memory_get":
			args, _ := params["arguments"].(map[string]interface{})
			key, _ := args["key"].(string)
			value, ok := memories[key]
			if !ok {
				w.Write(mcpErrorResponse("Error: Key not found"))
				return
			}
			w.Write(mcpJSONResponse(value))
		default:
			http.Error(w, "unknown tool", http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	facts, err := FetchMemoryFacts(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("FetchMemoryFacts: %v", err)
	}

	if !hasFact(facts, "x/feature-a", "project", "status", "completed") {
		t.Errorf("missing project scope feature-a fact, got %+v", facts)
	}
	if !hasFact(facts, "x/feature-a", "feature-a", "status", "completed") {
		t.Errorf("missing feature-a category fact, got %+v", facts)
	}
	if !hasFact(facts, "docs/a", "project", "status", "stale") {
		t.Errorf("missing project scope docs/a fact, got %+v", facts)
	}
	if !hasFact(facts, "docs/a", "docs", "status", "stale") {
		t.Errorf("missing docs/a stale fact, got %+v", facts)
	}
}

func TestFetchMemoryFacts_RecursiveFolders(t *testing.T) {
	memories := map[string]interface{}{
		"memory/project/orchestrator/tracker/kirill-scherba/go-prolog-mcp/7": memoryValueWrapper(map[string]interface{}{
			"stage": "in_review",
		}),
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		params, _ := req["params"].(map[string]interface{})
		name, _ := params["name"].(string)
		args, _ := params["arguments"].(map[string]interface{})
		prefix, _ := args["prefix"].(string)

		w.Header().Set("Content-Type", "application/json")
		switch name {
		case "memory_list":
			switch prefix {
			case "memory/":
				w.Write(mcpTextResponse("Found 1 memories:\nmemory/project/"))
			case "memory/project/":
				w.Write(mcpTextResponse("Found 1 memories:\nmemory/project/orchestrator/"))
			case "memory/project/orchestrator/":
				w.Write(mcpTextResponse("Found 1 memories:\nmemory/project/orchestrator/tracker/"))
			case "memory/project/orchestrator/tracker/":
				w.Write(mcpTextResponse("Found 1 memories:\nmemory/project/orchestrator/tracker/kirill-scherba/"))
			case "memory/project/orchestrator/tracker/kirill-scherba/":
				w.Write(mcpTextResponse("Found 1 memories:\nmemory/project/orchestrator/tracker/kirill-scherba/go-prolog-mcp/"))
			case "memory/project/orchestrator/tracker/kirill-scherba/go-prolog-mcp/":
				w.Write(mcpTextResponse("Found 1 memories:\nmemory/project/orchestrator/tracker/kirill-scherba/go-prolog-mcp/7"))
			default:
				w.Write(mcpTextResponse("No memories found."))
			}
		case "memory_get":
			key, _ := args["key"].(string)
			value, ok := memories[key]
			if !ok {
				w.Write(mcpErrorResponse("Error: Key not found"))
				return
			}
			w.Write(mcpJSONResponse(value))
		default:
			http.Error(w, "unknown tool", http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	facts, err := FetchMemoryFacts(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("FetchMemoryFacts: %v", err)
	}

	if !hasFact(facts, "go-prolog-mcp/7", "project", "stage", "in_review") {
		t.Errorf("missing recursive fact, got %+v", facts)
	}
}

func TestFetchMemoryFacts_SkipMissingKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		params, _ := req["params"].(map[string]interface{})
		name, _ := params["name"].(string)

		w.Header().Set("Content-Type", "application/json")
		if name == "memory_list" {
			w.Write(mcpTextResponse("Found 1 memories:\nmemory/project/x/bad-key"))
			return
		}
		w.Write(mcpErrorResponse("Error: Key not found"))
	}))
	defer srv.Close()

	_, err := FetchMemoryFacts(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected error when all fetches fail")
	}
	if !strings.Contains(err.Error(), "failed to fetch usable facts") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestFetchMemoryFacts_RespectsMaxKeys(t *testing.T) {
	var listCalls, getCalls int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)
		params, _ := req["params"].(map[string]interface{})
		name, _ := params["name"].(string)
		prefix, _ := params["arguments"].(map[string]interface{})["prefix"].(string)

		w.Header().Set("Content-Type", "application/json")
		if name == "memory_list" {
			listCalls++
			// Return one folder containing many leaf keys.
			if prefix == "memory/" {
				w.Write(mcpTextResponse("Found 1 memories:\nmemory/project/x/"))
				return
			}
			keys := make([]string, maxAutoFetchKeys+10)
			for i := range keys {
				keys[i] = fmt.Sprintf("memory/project/x/key-%d", i)
			}
			w.Write(mcpTextResponse("Found " + fmt.Sprintf("%d", len(keys)) + " memories:\n" + strings.Join(keys, "\n")))
			return
		}
		getCalls++
		w.Write(mcpJSONResponse(memoryValueWrapper(map[string]interface{}{"status": "ok"})))
	}))
	defer srv.Close()

	facts, err := FetchMemoryFacts(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("FetchMemoryFacts: %v", err)
	}

	if getCalls != maxAutoFetchKeys {
		t.Errorf("expected %d memory_get calls, got %d", maxAutoFetchKeys, getCalls)
	}
	// Each key produces facts for 'project' scope and the last path segment category.
	wantFacts := maxAutoFetchKeys * 2
	if len(facts) != wantFacts {
		t.Errorf("expected %d facts, got %d", wantFacts, len(facts))
	}
}

func TestFetchMemoryFacts_ContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		w.Write(mcpTextResponse("No memories found."))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := FetchMemoryFacts(ctx, srv.URL)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// hasFact checks whether the given fact exists in the slice.
func hasFact(facts []MemoryFact, key, category, attr, value string) bool {
	for _, f := range facts {
		if f.Key == key && f.Category == category && f.Attr == attr && f.Value == value {
			return true
		}
	}
	return false
}

// factKey returns a stable string for sorting/deduplicating facts in tests.
func factKey(f MemoryFact) string {
	return fmt.Sprintf("%s|%s|%s|%s", f.Key, f.Category, f.Attr, f.Value)
}

// sortedFacts returns the facts sorted by factKey for deterministic test output.
func sortedFacts(facts []MemoryFact) []MemoryFact {
	cp := append([]MemoryFact(nil), facts...)
	sort.Slice(cp, func(i, j int) bool {
		return factKey(cp[i]) < factKey(cp[j])
	})
	return cp
}
