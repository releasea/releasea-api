package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const maxProviderResponseBytes = 2 << 20

func callProvider(ctx context.Context, provider providerConfig, instructions, input string) (inferenceResult, error) {
	if !provider.Enabled {
		return inferenceResult{}, fmt.Errorf("AI provider is disabled")
	}
	if !provider.ExternalEgress {
		return inferenceResult{}, fmt.Errorf("external inference is disabled for this provider")
	}
	if len(input) > provider.MaxInputChars {
		input = input[:provider.MaxInputChars]
	}
	result, status, err := callResponses(ctx, provider, instructions, input)
	if err == nil {
		return result, nil
	}
	if provider.Type != providerTypeOpenAICompatible || (status != http.StatusNotFound && status != http.StatusMethodNotAllowed) {
		return inferenceResult{}, err
	}
	return callChatCompletions(ctx, provider, instructions, input)
}

func callResponses(ctx context.Context, provider providerConfig, instructions, input string) (inferenceResult, int, error) {
	payload := map[string]interface{}{
		"model":             provider.Model,
		"instructions":      instructions,
		"input":             input,
		"max_output_tokens": provider.MaxOutputTokens,
		"text":              map[string]interface{}{"format": map[string]interface{}{"type": "json_object"}},
	}
	var response struct {
		ID         string `json:"id"`
		Model      string `json:"model"`
		OutputText string `json:"output_text"`
		Output     []struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		Usage struct {
			Input  int64 `json:"input_tokens"`
			Output int64 `json:"output_tokens"`
			Total  int64 `json:"total_tokens"`
		} `json:"usage"`
	}
	status, err := providerRequest(ctx, provider, "/responses", payload, &response)
	if err != nil {
		return inferenceResult{}, status, err
	}
	text := strings.TrimSpace(response.OutputText)
	if text == "" {
		for _, item := range response.Output {
			for _, content := range item.Content {
				if content.Type == "output_text" {
					text += content.Text
				}
			}
		}
	}
	if text == "" {
		return inferenceResult{}, status, fmt.Errorf("provider returned an empty response")
	}
	return inferenceResult{ResponseID: response.ID, Model: response.Model, Text: text, Usage: usage{response.Usage.Input, response.Usage.Output, response.Usage.Total}}, status, nil
}

func callChatCompletions(ctx context.Context, provider providerConfig, instructions, input string) (inferenceResult, error) {
	payload := map[string]interface{}{
		"model":           provider.Model,
		"messages":        []map[string]string{{"role": "system", "content": instructions}, {"role": "user", "content": input}},
		"max_tokens":      provider.MaxOutputTokens,
		"response_format": map[string]string{"type": "json_object"},
	}
	var response struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			Prompt     int64 `json:"prompt_tokens"`
			Completion int64 `json:"completion_tokens"`
			Total      int64 `json:"total_tokens"`
		} `json:"usage"`
	}
	_, err := providerRequest(ctx, provider, "/chat/completions", payload, &response)
	if err != nil {
		return inferenceResult{}, err
	}
	if len(response.Choices) == 0 || strings.TrimSpace(response.Choices[0].Message.Content) == "" {
		return inferenceResult{}, fmt.Errorf("provider returned an empty response")
	}
	return inferenceResult{ResponseID: response.ID, Model: response.Model, Text: response.Choices[0].Message.Content, Usage: usage{response.Usage.Prompt, response.Usage.Completion, response.Usage.Total}}, nil
}

func providerRequest(ctx context.Context, provider providerConfig, path string, payload interface{}, target interface{}) (int, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, provider.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	request.Header.Set("Content-Type", "application/json")
	if provider.APIKey != "" {
		request.Header.Set("Authorization", "Bearer "+provider.APIKey)
	}
	client := providerHTTPClient(provider)
	response, err := client.Do(request)
	if err != nil {
		return 0, fmt.Errorf("provider request failed: %w", err)
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, maxProviderResponseBytes)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(limited, 4096))
		return response.StatusCode, fmt.Errorf("provider returned HTTP %d: %s", response.StatusCode, redactSensitive(strings.TrimSpace(string(message))))
	}
	if err := json.NewDecoder(limited).Decode(target); err != nil {
		return response.StatusCode, fmt.Errorf("decode provider response: %w", err)
	}
	return response.StatusCode, nil
}
