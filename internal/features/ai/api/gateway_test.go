package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestExtractJSONObject(t *testing.T) {
	input := "```json\n{\"summary\":\"ok\"}\n```"
	if got := extractJSONObject(input); got != "{\"summary\":\"ok\"}" {
		t.Fatalf("unexpected object: %s", got)
	}
}

func TestCallProviderUsesResponsesAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/responses" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatal("authorization header missing")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp-1","model":"test-model","output_text":"{\"summary\":\"ok\"}","usage":{"input_tokens":4,"output_tokens":2,"total_tokens":6}}`))
	}))
	defer server.Close()
	provider := providerConfig{Type: providerTypeOpenAICompatible, BaseURL: server.URL + "/v1", APIKey: "test-key", Model: "test-model", Enabled: true, ExternalEgress: true, AllowPrivateNetwork: true, Timeout: time.Second, MaxInputChars: 1000, MaxOutputTokens: 100}
	result, err := callProvider(context.Background(), provider, "instructions", "input")
	if err != nil {
		t.Fatalf("callProvider() error = %v", err)
	}
	if result.ResponseID != "resp-1" || result.Usage.TotalTokens != 6 {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestCallProviderFallsBackToChatCompletions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/v1/responses" {
			http.NotFound(w, request)
			return
		}
		if request.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
		_, _ = w.Write([]byte(`{"id":"chat-1","model":"local-model","choices":[{"message":{"content":"{\"summary\":\"local\"}"}}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`))
	}))
	defer server.Close()
	provider := providerConfig{Type: providerTypeOpenAICompatible, BaseURL: server.URL + "/v1", Model: "local-model", Enabled: true, ExternalEgress: true, AllowPrivateNetwork: true, Timeout: time.Second, MaxInputChars: 1000, MaxOutputTokens: 100}
	result, err := callProvider(context.Background(), provider, "instructions", "input")
	if err != nil {
		t.Fatalf("callProvider() error = %v", err)
	}
	if result.ResponseID != "chat-1" || result.Usage.TotalTokens != 5 {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestNormalizeResultRemovesUnknownEvidence(t *testing.T) {
	result := analysisResult{Findings: []analysisFinding{{EvidenceIDs: []string{"service-config", "invented"}}}, Recommendations: []recommendation{{EvidenceIDs: []string{"invented"}}}}
	normalizeResult(&result, map[string]bool{"service-config": true})
	if len(result.Findings[0].EvidenceIDs) != 1 || result.Findings[0].EvidenceIDs[0] != "service-config" {
		t.Fatalf("unexpected finding evidence: %#v", result.Findings[0].EvidenceIDs)
	}
	if len(result.Recommendations[0].EvidenceIDs) != 0 {
		t.Fatalf("unknown evidence was retained: %#v", result.Recommendations[0].EvidenceIDs)
	}
}

func TestValidateResultRequiresCitations(t *testing.T) {
	valid := analysisResult{Summary: "Review required", Severity: "warning", Findings: []analysisFinding{{Title: "Failure", Explanation: "Readiness failed", EvidenceIDs: []string{"deploy-1"}}}}
	if err := validateResult(valid); err != nil {
		t.Fatalf("valid result rejected: %v", err)
	}
	valid.Findings[0].EvidenceIDs = nil
	if err := validateResult(valid); err == nil {
		t.Fatal("result without citations was accepted")
	}
}
