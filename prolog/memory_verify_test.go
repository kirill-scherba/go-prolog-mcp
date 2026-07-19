package prolog

import (
	"strings"
	"testing"
)

// experimentalFacts reproduces the data from the manual experiment in issue #4.
func experimentalFacts() []MemoryFact {
	return []MemoryFact{
		{Key: "agent-tosha", Category: "agent", Attr: "status", Value: "idea"},
		{Key: "agent-tosha", Category: "agent", Attr: "nature", Value: "myth"},
		{Key: "memory-extract-async", Category: "feature", Attr: "status", Value: "completed"},
		{Key: "memory-extract-async", Category: "docs", Attr: "status", Value: "stale"},
		{Key: "memory-extract-async", Category: "docs", Attr: "last_updated", Value: "2026-07-13"},
		{Key: "pr21-ai-approval", Category: "operation", Attr: "result", Value: "added"},
		{Key: "pr21-ai-approval", Category: "review", Attr: "result", Value: "not-performed-as-intended"},
		{Key: "million-dollars", Category: "goal", Attr: "status", Value: "active"},
		{Key: "million-dollars", Category: "goal", Attr: "deadline", Value: "2026-06-30"},
		{Key: "million-dollars", Category: "goal", Attr: "progress", Value: "0"},
		{Key: "million-dollars", Category: "goal", Attr: "assignee", Value: "baron"},
		{Key: "rag-mcp", Category: "feature", Attr: "status", Value: "active"},
		{Key: "rag-mcp", Category: "docs", Attr: "status", Value: "current"},
	}
}

func TestBuildMemoryFacts(t *testing.T) {
	facts := []MemoryFact{
		{Key: "k1", Category: "c1", Attr: "a1", Value: "v1"},
		{Key: "it's", Category: "c'2", Attr: "a'2", Value: "v'2"},
	}
	got := BuildMemoryFacts(facts)
	want1 := "mem('k1', 'c1', 'a1', 'v1')."
	want2 := "mem('it\\'s', 'c\\'2', 'a\\'2', 'v\\'2')."
	if !strings.Contains(got, want1) {
		t.Errorf("BuildMemoryFacts missing fact: want substring %q, got:\n%s", want1, got)
	}
	if !strings.Contains(got, want2) {
		t.Errorf("BuildMemoryFacts missing escaped fact: want substring %q, got:\n%s", want2, got)
	}
}

func TestBuildMemoryProgram(t *testing.T) {
	custom := "anomaly(Key, 'custom-rule') :- mem(Key, 'x', 'y', 'z')."
	program := BuildMemoryProgram(custom)
	if !strings.Contains(program, "contradiction(Key, 'completed-vs-stale-docs')") {
		t.Error("BuildMemoryProgram missing default contradiction rule")
	}
	if !strings.Contains(program, "anomaly(Key, 'expired-goal')") {
		t.Error("BuildMemoryProgram missing default anomaly rule")
	}
	if !strings.Contains(program, custom) {
		t.Error("BuildMemoryProgram missing custom rules")
	}
}

func TestBuildMemoryProgramWithoutCustomRules(t *testing.T) {
	program := BuildMemoryProgram("")
	if !strings.Contains(program, memoryRulesPL) {
		t.Error("BuildMemoryProgram(\"\") should include embedded rules")
	}
}

func TestVerifyMemoryExperimentalFacts(t *testing.T) {
	result, err := VerifyMemory(experimentalFacts(), "")
	if err != nil {
		t.Fatalf("VerifyMemory: %v", err)
	}

	if result.Stats.FactsChecked != 13 {
		t.Errorf("FactsChecked = %d, want 13", result.Stats.FactsChecked)
	}
	if result.Stats.RulesApplied != 4 {
		t.Errorf("RulesApplied = %d, want 4", result.Stats.RulesApplied)
	}
	if result.Stats.ContradictionsFound != 1 {
		t.Errorf("ContradictionsFound = %d, want 1", result.Stats.ContradictionsFound)
	}
	if result.Stats.AnomaliesFound != 3 {
		t.Errorf("AnomaliesFound = %d, want 3", result.Stats.AnomaliesFound)
	}

	if !hasIssue(result.Contradictions, "memory-extract-async", "completed-vs-stale-docs") {
		t.Errorf("missing contradiction memory-extract-async/completed-vs-stale-docs, got %+v", result.Contradictions)
	}
	if !hasIssue(result.Anomalies, "million-dollars", "expired-goal") {
		t.Errorf("missing anomaly million-dollars/expired-goal, got %+v", result.Anomalies)
	}
	if !hasIssue(result.Anomalies, "million-dollars", "baron-idle") {
		t.Errorf("missing anomaly million-dollars/baron-idle, got %+v", result.Anomalies)
	}
	if !hasIssue(result.Anomalies, "pr21-ai-approval", "conflicting-results") {
		t.Errorf("missing anomaly pr21-ai-approval/conflicting-results, got %+v", result.Anomalies)
	}
}

func TestContradictionCompletedVsStaleDocs(t *testing.T) {
	facts := []MemoryFact{
		{Key: "f1", Category: "feature", Attr: "status", Value: "completed"},
		{Key: "f1", Category: "docs", Attr: "status", Value: "stale"},
	}
	result, err := VerifyMemory(facts, "")
	if err != nil {
		t.Fatalf("VerifyMemory: %v", err)
	}
	if !hasIssue(result.Contradictions, "f1", "completed-vs-stale-docs") {
		t.Errorf("expected contradiction, got %+v", result.Contradictions)
	}
	if len(result.Anomalies) != 0 {
		t.Errorf("expected 0 anomalies, got %+v", result.Anomalies)
	}
}

func TestContradictionNoStaleDoc(t *testing.T) {
	facts := []MemoryFact{
		{Key: "f1", Category: "feature", Attr: "status", Value: "completed"},
		{Key: "f1", Category: "docs", Attr: "status", Value: "current"},
	}
	result, err := VerifyMemory(facts, "")
	if err != nil {
		t.Fatalf("VerifyMemory: %v", err)
	}
	if len(result.Contradictions) != 0 {
		t.Errorf("expected 0 contradictions, got %+v", result.Contradictions)
	}
}

func TestAnomalyExpiredGoal(t *testing.T) {
	facts := []MemoryFact{
		{Key: "g1", Category: "goal", Attr: "status", Value: "active"},
		{Key: "g1", Category: "goal", Attr: "deadline", Value: "2020-01-01"},
		{Key: "g1", Category: "goal", Attr: "progress", Value: "0"},
	}
	result, err := VerifyMemory(facts, "")
	if err != nil {
		t.Fatalf("VerifyMemory: %v", err)
	}
	if !hasIssue(result.Anomalies, "g1", "expired-goal") {
		t.Errorf("expected expired-goal anomaly, got %+v", result.Anomalies)
	}
}

func TestNoExpiredGoalForFutureDeadline(t *testing.T) {
	facts := []MemoryFact{
		{Key: "g2", Category: "goal", Attr: "status", Value: "active"},
		{Key: "g2", Category: "goal", Attr: "deadline", Value: "2030-01-01"},
		{Key: "g2", Category: "goal", Attr: "progress", Value: "0"},
	}
	result, err := VerifyMemory(facts, "")
	if err != nil {
		t.Fatalf("VerifyMemory: %v", err)
	}
	if hasIssue(result.Anomalies, "g2", "expired-goal") {
		t.Errorf("future deadline should not trigger expired-goal, got %+v", result.Anomalies)
	}
}

func TestAnomalyConflictingResults(t *testing.T) {
	facts := []MemoryFact{
		{Key: "x1", Category: "operation", Attr: "result", Value: "added"},
		{Key: "x1", Category: "review", Attr: "result", Value: "not-performed-as-intended"},
	}
	result, err := VerifyMemory(facts, "")
	if err != nil {
		t.Fatalf("VerifyMemory: %v", err)
	}
	if !hasIssue(result.Anomalies, "x1", "conflicting-results") {
		t.Errorf("expected conflicting-results anomaly, got %+v", result.Anomalies)
	}
}

func TestAnomalyConflictingResultsDedup(t *testing.T) {
	facts := []MemoryFact{
		{Key: "x1", Category: "a", Attr: "result", Value: "foo"},
		{Key: "x1", Category: "b", Attr: "result", Value: "bar"},
		{Key: "x1", Category: "c", Attr: "result", Value: "baz"},
	}
	result, err := VerifyMemory(facts, "")
	if err != nil {
		t.Fatalf("VerifyMemory: %v", err)
	}
	if result.Stats.AnomaliesFound != 1 {
		t.Errorf("AnomaliesFound = %d, want 1 (dedup by key|type)", result.Stats.AnomaliesFound)
	}
}

func TestAnomalyBaronIdle(t *testing.T) {
	facts := []MemoryFact{
		{Key: "g1", Category: "goal", Attr: "assignee", Value: "baron"},
		{Key: "g1", Category: "goal", Attr: "progress", Value: "0"},
	}
	result, err := VerifyMemory(facts, "")
	if err != nil {
		t.Fatalf("VerifyMemory: %v", err)
	}
	if !hasIssue(result.Anomalies, "g1", "baron-idle") {
		t.Errorf("expected baron-idle anomaly, got %+v", result.Anomalies)
	}
}

func TestNoFalsePositiveAgentTosha(t *testing.T) {
	facts := []MemoryFact{
		{Key: "agent-tosha", Category: "agent", Attr: "status", Value: "idea"},
		{Key: "agent-tosha", Category: "agent", Attr: "nature", Value: "myth"},
	}
	result, err := VerifyMemory(facts, "")
	if err != nil {
		t.Fatalf("VerifyMemory: %v", err)
	}
	if len(result.Contradictions) != 0 || len(result.Anomalies) != 0 {
		t.Errorf("expected no issues for agent-tosha, got contradictions=%+v anomalies=%+v",
			result.Contradictions, result.Anomalies)
	}
}

func TestNoFalsePositiveRagMcp(t *testing.T) {
	facts := []MemoryFact{
		{Key: "rag-mcp", Category: "feature", Attr: "status", Value: "active"},
		{Key: "rag-mcp", Category: "docs", Attr: "status", Value: "current"},
	}
	result, err := VerifyMemory(facts, "")
	if err != nil {
		t.Fatalf("VerifyMemory: %v", err)
	}
	if len(result.Contradictions) != 0 || len(result.Anomalies) != 0 {
		t.Errorf("expected no issues for rag-mcp, got contradictions=%+v anomalies=%+v",
			result.Contradictions, result.Anomalies)
	}
}

func TestVerifyMemoryEmptyFacts(t *testing.T) {
	result, err := VerifyMemory([]MemoryFact{}, "")
	if err != nil {
		t.Fatalf("VerifyMemory: %v", err)
	}
	if result.Stats.FactsChecked != 0 {
		t.Errorf("FactsChecked = %d, want 0", result.Stats.FactsChecked)
	}
	if len(result.Contradictions) != 0 || len(result.Anomalies) != 0 {
		t.Errorf("expected empty results, got %+v", result)
	}
}

func TestVerifyMemoryCustomRules(t *testing.T) {
	facts := []MemoryFact{
		{Key: "k1", Category: "x", Attr: "y", Value: "z"},
	}
	custom := "anomaly(Key, 'custom-rule') :- mem(Key, 'x', 'y', 'z')."
	result, err := VerifyMemory(facts, custom)
	if err != nil {
		t.Fatalf("VerifyMemory: %v", err)
	}
	if !hasIssue(result.Anomalies, "k1", "custom-rule") {
		t.Errorf("expected custom-rule anomaly, got %+v", result.Anomalies)
	}
	if result.Stats.RulesApplied != 5 {
		t.Errorf("RulesApplied = %d, want 5 (4 default + 1 custom)", result.Stats.RulesApplied)
	}
}

func TestVerifyMemoryInvalidCustomRules(t *testing.T) {
	facts := []MemoryFact{
		{Key: "k1", Category: "c", Attr: "a", Value: "v"},
	}
	_, err := VerifyMemory(facts, "!!!invalid!!!")
	if err == nil {
		t.Fatal("expected error for invalid custom rules, got nil")
	}
}

func hasIssue(issues []MemoryIssue, key, typ string) bool {
	for _, i := range issues {
		if i.Key == key && i.Type == typ {
			return true
		}
	}
	return false
}
