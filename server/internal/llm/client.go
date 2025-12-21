package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"bubble-talk/server/internal/config"
)

// Client LLM 客户端接口
type Client interface {
	// Complete 完成文本生成任务
	Complete(ctx context.Context, messages []Message, schema *JSONSchema) (string, error)
}

// Message 消息结构
type Message struct {
	Role    string `json:"role"` // "system", "user", "assistant"
	Content string `json:"content"`
}

// JSONSchema JSON Schema 定义（用于结构化输出）
type JSONSchema struct {
	Name   string         `json:"name"`
	Schema map[string]any `json:"schema"`
	Strict bool           `json:"strict,omitempty"`
}

// NewClient 创建 LLM 客户端
func NewClient(cfg *config.Config) (Client, error) {
	switch cfg.LLM.Provider {
	case "openai":
		return NewOpenAIClient(cfg.LLM.OpenAI), nil
	case "anthropic":
		return NewAnthropicClient(cfg.LLM.Anthropic), nil
	default:
		return nil, fmt.Errorf("unsupported LLM provider: %s", cfg.LLM.Provider)
	}
}

// OpenAIClient OpenAI 客户端
type OpenAIClient struct {
	config     config.LLMProviderConfig
	httpClient *http.Client
}

// NewOpenAIClient 创建 OpenAI 客户端
func NewOpenAIClient(cfg config.LLMProviderConfig) *OpenAIClient {
	return &OpenAIClient{
		config: cfg,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Complete 完成文本生成（OpenAI）
func (c *OpenAIClient) Complete(ctx context.Context, messages []Message, schema *JSONSchema) (string, error) {
	reqBody := map[string]any{
		"model":       c.config.Model,
		"messages":    messages,
		"temperature": c.config.Temperature,
		"max_tokens":  c.config.MaxTokens,
	}

	// 如果提供了 schema，使用 JSON mode
	if schema != nil {
		reqBody["response_format"] = map[string]any{
			"type":        "json_schema",
			"json_schema": schema,
		}
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.config.APIURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.config.APIKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("unmarshal response: %w", err)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}

	return result.Choices[0].Message.Content, nil
}

// AnthropicClient Anthropic 客户端
type AnthropicClient struct {
	config     config.LLMProviderConfig
	httpClient *http.Client
}

// NewAnthropicClient 创建 Anthropic 客户端
func NewAnthropicClient(cfg config.LLMProviderConfig) *AnthropicClient {
	return &AnthropicClient{
		config: cfg,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Complete 完成文本生成（Anthropic）
func (c *AnthropicClient) Complete(ctx context.Context, messages []Message, schema *JSONSchema) (string, error) {
	// Anthropic 需要分离 system message
	var systemMsg string
	var userMessages []map[string]string

	for _, msg := range messages {
		if msg.Role == "system" {
			systemMsg = msg.Content
		} else {
			userMessages = append(userMessages, map[string]string{
				"role":    msg.Role,
				"content": msg.Content,
			})
		}
	}

	reqBody := map[string]any{
		"model":       c.config.Model,
		"messages":    userMessages,
		"max_tokens":  c.config.MaxTokens,
		"temperature": c.config.Temperature,
	}

	if systemMsg != "" {
		reqBody["system"] = systemMsg
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.config.APIURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.config.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Content []struct {
			Text string `json:"text"`
			Type string `json:"type"`
		} `json:"content"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("unmarshal response: %w", err)
	}

	if len(result.Content) == 0 {
		return "", fmt.Errorf("no content in response")
	}

	return result.Content[0].Text, nil
}
