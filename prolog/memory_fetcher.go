package prolog

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/kirill-scherba/go-prolog-mcp/mcpclient"
)

// DefaultMemoryStoreURL is the well-known local endpoint for memory-store-mcp
// through the MCP gateway.
const DefaultMemoryStoreURL = "http://localhost:7711/memory-store-mcp"

// maxAutoFetchKeys limits the number of leaf memory entries processed in one
// run. This keeps auto-fetch fast and avoids overloading memory-store-mcp.
const maxAutoFetchKeys = 500

// FetchStats tracks how many memory entries were listed, fetched, skipped, and
// limited during an auto-fetch run.
type FetchStats struct {
	Listed  int `json:"listed"`
	Fetched int `json:"fetched"`
	Skipped int `json:"skipped"`
	Limited bool `json:"limited"`
}

// FetchMemoryFacts connects to memory-store-mcp, recursively lists keys under
// the "memory/" prefix, fetches each leaf entry, and normalizes the entries
// into MemoryFact values suitable for VerifyMemory.
//
// Returns an error if the store could not be contacted or if no facts could be
// extracted from a non-empty store.
func FetchMemoryFacts(ctx context.Context, url string) ([]MemoryFact, error) {
	return FetchMemoryFactsWithStats(ctx, url, FetchStats{})
}

// FetchMemoryFactsWithStats performs the same work as FetchMemoryFacts and
// also returns fetch statistics. The stats argument is used for recursive
// calls; callers should pass an empty FetchStats.
func FetchMemoryFactsWithStats(ctx context.Context, url string, stats FetchStats) ([]MemoryFact, error) {
	if url == "" {
		url = DefaultMemoryStoreURL
	}

	client := mcpclient.NewClient(url, 0)
	defer client.Close()

	keys, listed, limited, err := listMemoryKeysRecursive(ctx, client, "memory/", maxAutoFetchKeys, &stats)
	if err != nil {
		return nil, fmt.Errorf("memory_list: %w", err)
	}
	stats.Listed = listed
	stats.Limited = limited

	var facts []MemoryFact
	for _, key := range keys {
		rawValue, err := client.CallTool(ctx, "memory_get", map[string]interface{}{
			"key": key,
		})
		if err != nil {
			// A single failed fetch should not abort the whole verification.
			stats.Skipped++
			continue
		}

		entryFacts := NormalizeMemoryEntry(key, rawValue)
		if len(entryFacts) == 0 {
			// The key was fetched but produced no usable facts (e.g. empty
			// content that is not JSON). Count it as fetched, not skipped.
		}
		facts = append(facts, entryFacts...)
		stats.Fetched++
	}

	if stats.Listed > 0 && stats.Fetched == 0 {
		return nil, fmt.Errorf("listed %d keys but failed to fetch usable facts from any of them (skipped %d)", stats.Listed, stats.Skipped)
	}

	return facts, nil
}

// FetchMemoryFactsByQuery searches memory-store-mcp by keyword using
// memory_find, fetches all matching entries, and normalizes them into facts
// with a single key matching the query. This lets Prolog find contradictions
// across entries that refer to the same subject.
func FetchMemoryFactsByQuery(ctx context.Context, url, query string) ([]MemoryFact, error) {
	if url == "" {
		url = DefaultMemoryStoreURL
	}
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}

	client := mcpclient.NewClient(url, 0)
	defer client.Close()

	// Call memory_find to get matching keys.
	raw, err := client.CallToolRaw(ctx, "memory_find", map[string]interface{}{
		"keyword": query,
	})
	if err != nil {
		return nil, fmt.Errorf("memory_find: %w", err)
	}

	// Parse the text response to extract keys.
	// Format: "Found N result(s) for \"query\":\n1. memory/key\n..."
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	var keys []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Skip header and empty lines.
		if line == "" || strings.HasPrefix(line, "Found") || strings.HasPrefix(line, "No memories") {
			continue
		}
		// Lines start with "N. memory/key" — strip the prefix.
		if idx := strings.Index(line, "memory/"); idx >= 0 {
			key := strings.TrimSpace(line[idx:])
			// Remove trailing date if present (from memory_list display).
			if spaceIdx := strings.Index(key, " "); spaceIdx > 0 {
				key = key[:spaceIdx]
			}
			keys = append(keys, key)
		}
	}

	if len(keys) == 0 {
		return nil, fmt.Errorf("no memories found for query %q", query)
	}

	// Fetch each key and normalize, forcing the short key to the query value.
	var facts []MemoryFact
	for _, key := range keys {
		rawValue, err := client.CallTool(ctx, "memory_get", map[string]interface{}{
			"key": key,
		})
		if err != nil {
			continue
		}

		entryFacts := normalizeWithKey(key, rawValue, query)
		facts = append(facts, entryFacts...)
	}

	if len(facts) == 0 {
		return nil, fmt.Errorf("found %d keys for %q but could not extract facts", len(keys), query)
	}

	return facts, nil
}

// normalizeWithKey normalizes a memory entry like NormalizeMemoryEntry but
// forces the Prolog key to the given overrideKey instead of extracting it
// from the memory key path.
func normalizeWithKey(memKey string, value interface{}, overrideKey string) []MemoryFact {
	parts := keyPathParts(memKey)
	if len(parts) == 0 {
		return nil
	}

	categories := extractCategories(parts)

	innerValue, ok := unwrapMemoryValue(value)
	if !ok {
		return nil
	}

	// Normalize like in NormalizeMemoryEntry but with overrideKey.
	valueMap, ok := innerValue.(map[string]interface{})
	if !ok {
		if len(categories) == 0 {
			return nil
		}
		// Plain text or wrapper: extract wrapper fields.
		var facts []MemoryFact
		for _, category := range categories {
			if wrapper, ok := value.(map[string]interface{}); ok {
				for _, attr := range []string{"summary", "status", "priority", "progress", "deadline", "timestamp"} {
					if val, exists := wrapper[attr]; exists && val != nil {
						vs := valueToString(val)
						if vs != "" {
							facts = append(facts, MemoryFact{
								Key:      overrideKey,
								Category: category,
								Attr:     attr,
								Value:    vs,
							})
						}
					}
				}
				if tags, exists := wrapper["tags"]; exists {
					if tagArr, ok := tags.([]interface{}); ok {
						for _, t := range tagArr {
							facts = append(facts, MemoryFact{
								Key:      overrideKey,
								Category: category,
								Attr:     "tag",
								Value:    valueToString(t),
							})
						}
					}
				}
			}
			// Try extractFieldsFromText on the raw inner value.
			if contentStr, ok := innerValue.(string); ok && contentStr != "" {
				extras := extractFieldsFromText(contentStr)
				for _, ef := range extras {
					facts = append(facts, MemoryFact{
						Key:      overrideKey,
						Category: category,
						Attr:     ef.attr,
						Value:    ef.value,
					})
				}
			}
		}
		return facts
	}

	// JSON content path.
	var facts []MemoryFact
	for _, category := range categories {
		for attr, val := range valueMap {
			facts = append(facts, MemoryFact{
				Key:      overrideKey,
				Category: category,
				Attr:     attr,
				Value:    valueToString(val),
			})
		}
		if wrapper, ok := value.(map[string]interface{}); ok {
			for _, attr := range []string{"summary", "status", "priority", "progress", "deadline", "timestamp"} {
				if val, exists := wrapper[attr]; exists && val != nil {
					vs := valueToString(val)
					if vs != "" && vs != valueToString(valueMap[attr]) {
						facts = append(facts, MemoryFact{
							Key:      overrideKey,
							Category: category,
							Attr:     attr,
							Value:    vs,
						})
					}
				}
			}
		}
	}
	return facts
}

// memoryListHeaderPattern matches the "Found N memories:" header line returned
// by memory-store-mcp's memory_list tool.
var memoryListHeaderPattern = regexp.MustCompile(`^Found\s+\d+\s+memories?:`)

// listMemoryKeysRecursive calls memory_list for the given prefix and, for any
// folder entries (keys ending in "/"), recurses into them. It returns leaf keys
// only. The limit controls the maximum number of leaf keys to return; the
// prefixLimit controls how many list results are processed at each level.
func listMemoryKeysRecursive(ctx context.Context, client *mcpclient.Client, prefix string, limit int, stats *FetchStats) ([]string, int, bool, error) {
	text, err := client.CallToolRaw(ctx, "memory_list", map[string]interface{}{
		"prefix": prefix,
	})
	if err != nil {
		return nil, 0, false, err
	}

	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) == 0 {
		return nil, 0, false, nil
	}

	// First non-empty line must be the header; the rest are keys.
	var entries []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if memoryListHeaderPattern.MatchString(line) {
			continue
		}
		if line == "No memories found." {
			return nil, 0, false, nil
		}
		entries = append(entries, line)
	}

	if len(entries) == 0 {
		return nil, 0, false, nil
	}

	var leafKeys []string
	var listed int
	limited := false

	for _, entry := range entries {
		// Respect the global leaf-key limit.
		if len(leafKeys) >= limit {
			limited = true
			break
		}

		if strings.HasSuffix(entry, "/") {
			// Folder: recurse. Do not count folders as listed keys.
			childKeys, childListed, childLimited, err := listMemoryKeysRecursive(ctx, client, entry, limit-len(leafKeys), stats)
			if err != nil {
				// Log and continue: one broken folder should not abort the whole tree.
				continue
			}
			leafKeys = append(leafKeys, childKeys...)
			listed += childListed
			if childLimited {
				limited = true
			}
			continue
		}

		leafKeys = append(leafKeys, entry)
		listed++
	}

	return leafKeys, listed, limited, nil
}

// NormalizeMemoryEntry converts a memory-store-mcp key and its JSON value into
// a set of MemoryFact rows that the embedded Prolog rules can reason about.
//
// The key is expected to follow the convention memory/<scope>/.../<id>. The
// category is derived from the path, and each top-level JSON field becomes an
// attribute/value pair. Nested objects are skipped; arrays are joined as
// comma-separated strings.
//
// memory_get returns a wrapper like
//
//	{"content": "...", "timestamp": "...", "status": "..."}
//
// where "content" is the stored JSON value as a string. This function unwraps
// that wrapper and normalizes the inner content. If content is not valid JSON,
// a single fact with attr "content" and the raw text as value is produced.
func NormalizeMemoryEntry(key string, value interface{}) []MemoryFact {
	parts := keyPathParts(key)
	if len(parts) == 0 {
		return nil
	}

	categories := extractCategories(parts)
	shortKey := extractShortKey(parts)

	// memory_get returns a wrapper object. Extract the inner "content" value.
	innerValue, ok := unwrapMemoryValue(value)
	if !ok {
		return nil
	}

	valueMap, ok := innerValue.(map[string]interface{})
	if !ok {
		// Content is not a JSON object: emit facts from wrapper fields
		// (summary, status, tags, timestamp, priority, etc.) so structured
		// data is not lost even when content is plain text.
		if len(categories) == 0 {
			return nil
		}
		var facts []MemoryFact
		for _, category := range categories {
			// Extract wrapper-level fields for non-JSON content.
			if wrapper, ok := value.(map[string]interface{}); ok {
				for _, attr := range []string{"summary", "status", "priority", "progress", "deadline", "timestamp"} {
					if val, exists := wrapper[attr]; exists && val != nil {
						vs := valueToString(val)
						if vs != "" {
							facts = append(facts, MemoryFact{
								Key:      shortKey,
								Category: category,
								Attr:     attr,
								Value:    vs,
							})
						}
					}
				}
				// tags are an array
				if tags, exists := wrapper["tags"]; exists {
					if tagArr, ok := tags.([]interface{}); ok {
						for _, t := range tagArr {
							facts = append(facts, MemoryFact{
								Key:      shortKey,
								Category: category,
								Attr:     "tag",
								Value:    valueToString(t),
							})
						}
					}
				}
			}
		}
		// Also try to extract structured fields from plain text content
		// by matching key patterns like "deadline: ...", "progress: ...", etc.
		if contentStr, ok := innerValue.(string); ok && contentStr != "" {
			extras := extractFieldsFromText(contentStr)
			for _, ef := range extras {
				for _, category := range categories {
					facts = append(facts, MemoryFact{
						Key:      shortKey,
						Category: category,
						Attr:     ef.attr,
						Value:    ef.value,
					})
				}
			}
		}
		return facts
	}

	var facts []MemoryFact
	for _, category := range categories {
		for attr, val := range valueMap {
			facts = append(facts, MemoryFact{
				Key:      shortKey,
				Category: category,
				Attr:     attr,
				Value:    valueToString(val),
			})
		}
	}

	// Also add wrapper-level fields (summary, status, tags) if not already in valueMap
	if wrapper, ok := value.(map[string]interface{}); ok {
		for _, attr := range []string{"summary", "status", "priority", "progress", "deadline", "timestamp"} {
			if val, exists := wrapper[attr]; exists && val != nil {
				vs := valueToString(val)
				if vs != "" {
					// Only add if valueMap didn't already have this attr (avoid duplicates)
					if _, inMap := valueMap[attr]; !inMap {
						for _, category := range categories {
							facts = append(facts, MemoryFact{
								Key:      shortKey,
								Category: category,
								Attr:     attr,
								Value:    vs,
							})
						}
					}
				}
			}
		}
	}

	return facts
}

// unwrapMemoryValue extracts the stored content from a memory_get wrapper.
// If the input is already a bare value (not a wrapper), it is returned as-is.
func unwrapMemoryValue(value interface{}) (interface{}, bool) {
	wrapper, ok := value.(map[string]interface{})
	if !ok {
		return value, true
	}

	content, hasContent := wrapper["content"]
	if !hasContent {
		// Not a wrapper: use the value directly.
		return value, true
	}

	contentStr, ok := content.(string)
	if !ok {
		// content is not a string; return the wrapper content field as-is.
		return content, true
	}

	contentStr = strings.TrimSpace(contentStr)
	if contentStr == "" {
		// Empty content: still return the wrapper so callers can see status/timestamp.
		return wrapper, true
	}

	// Try to parse the content string as JSON.
	var parsed interface{}
	if err := json.Unmarshal([]byte(contentStr), &parsed); err != nil {
		// Not valid JSON: return the raw content string.
		return contentStr, true
	}
	return parsed, true
}

// keyPathParts splits a memory key like "memory/project/foo/bar" into path
// segments after removing the leading "memory/" prefix.
func keyPathParts(key string) []string {
	key = strings.TrimPrefix(key, "memory/")
	key = strings.Trim(key, "/")
	if key == "" {
		return nil
	}
	parts := strings.Split(key, "/")
	var result []string
	for _, p := range parts {
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// extractCategories returns candidate Prolog categories for a memory key.
// Multiple categories are emitted so that a single memory entry can match
// rules written for different scopes (project, docs, goal, etc.).
func extractCategories(parts []string) []string {
	seen := make(map[string]bool)
	var cats []string

	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		cats = append(cats, s)
	}

	if len(parts) == 0 {
		return nil
	}

	// First segment is the top-level scope (project, user, technical, auto).
	add(parts[0])

	// Map known plural/misspelled scopes to rule-friendly singulars.
	if parts[0] == "goals" {
		add("goal")
	}
	if parts[0] == "bugs" {
		add("bug")
	}

	// For project memories the third segment is the concrete category.
	if parts[0] == "project" && len(parts) >= 3 {
		add(parts[2])
	}

	// Also add any segment that looks like a known domain category. This helps
	// rules like completed-vs-stale-docs match documentation entries even when
	// the category is not the third segment.
	for _, p := range parts[1:] {
		if isKnownCategory(p) {
			add(p)
		}
	}

	return cats
}

// isKnownCategory recognises domain names that the default Prolog rules use.
func isKnownCategory(s string) bool {
	switch s {
	case "docs", "goal", "feature", "agent", "operation", "review":
		return true
	}
	return false
}

// extractShortKey derives a readable key from the path. It uses the last two
// segments whenever possible so that related entries share a meaningful key.
func extractShortKey(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		return parts[0]
	}
	if len(parts) >= 3 {
		return strings.Join(parts[len(parts)-2:], "/")
	}
	return parts[len(parts)-1]
}

// valueToString converts a memory value field to a string for Prolog facts.
func valueToString(v interface{}) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case []interface{}:
		var elems []string
		for _, e := range x {
			elems = append(elems, valueToString(e))
		}
		return strings.Join(elems, ",")
	case map[string]interface{}:
		// Nested objects are not flattened. Returning an empty string keeps
		// the fact count predictable; consumers can extend this later if
		// needed.
		return ""
	default:
		return fmt.Sprint(x)
	}
}

// extractFieldsFromText tries to find structured key:value pairs in plain text
// like "Deadline: 2026-06-30", "Progress: 0", "Priority: 10", "Assignee: baron".
type extractedField struct {
	attr  string
	value string
}

func extractFieldsFromText(text string) []extractedField {
	var fields []extractedField

	// Map of patterns: attr name → list of [label regexp, value regexp]
	type pattern struct {
		label *regexp.Regexp
		value string
	}
	patterns := []struct {
		attr   string
		labels []string
	}{
		{"deadline", []string{"deadline", "Deadline", "Дедлайн", "дедлайн", "срок"}},
		{"progress", []string{"progress", "Progress", "Прогресс", "прогресс", "готовность"}},
		{"priority", []string{"priority", "Priority", "Приоритет", "приоритет"}},
		{"assignee", []string{"assignee", "Assignee", "Исполнитель", "исполнитель", "Ответственный", "ответственный"}},
	}

	for _, p := range patterns {
		for _, lbl := range p.labels {
			// Pattern: "Label: value" — value up to next comma, dot, newline or end
			re := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(lbl) + `\s*[:=]\s*([^,.\n]+)`)
			matches := re.FindStringSubmatch(text)
			if len(matches) >= 2 {
				val := strings.TrimSpace(matches[1])
				if val != "" {
					fields = append(fields, extractedField{attr: p.attr, value: val})
				}
				break // first match wins for this attr
			}
		}
	}

	// Special: extract status=active from "Цель:" or статус
	statusRe := regexp.MustCompile(`(?i)(?:status|статус)\s*[:=]\s*([^,.\n]+)`)
	if m := statusRe.FindStringSubmatch(text); len(m) >= 2 {
		fields = append(fields, extractedField{attr: "status", value: strings.TrimSpace(m[1])})
	}
	// If text starts with "Цель:" or "Goal:" and no status found, assume active
	hasStatus := false
	for _, f := range fields {
		if f.attr == "status" {
			hasStatus = true
			break
		}
	}
	if !hasStatus {
		goalRe := regexp.MustCompile(`(?i)^\s*(?:Цель|Goal|Задача|Task)\s*[::]`)
		if goalRe.MatchString(text) {
			fields = append(fields, extractedField{attr: "status", value: "active"})
		}
	}

	return fields
}
