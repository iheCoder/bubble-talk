package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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
	case "talopenai":
		return NewTalOpenAIClient(cfg.LLM.TalOpenAI), nil
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
		"model":                 c.config.Model,
		"messages":              messages,
		"temperature":           c.config.Temperature,
		"max_completion_tokens": c.config.MaxTokens,
	}

	// gpt-5 系列在 ChatCompletions 下可能会把 token 预算主要消耗在 reasoning，
	// 导致 message.content 为空且 finish_reason=length（只产出 reasoning tokens）。
	// 这里默认将 reasoning effort 降到 low，确保能稳定产出可解析的输出内容。
	if isOpenAIReasoningModel(c.config.Model) {
		reqBody["reasoning_effort"] = "low"
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

	content := result.Choices[0].Message.Content
	if content == "" {
		return "", fmt.Errorf("empty content in response: %s", string(respBody))
	}

	return content, nil
}

func isOpenAIReasoningModel(model string) bool {
	// 经验规则：gpt-5 / o1 等会产出 reasoning tokens。
	// 这里用最保守的匹配，避免影响 gpt-4o 等常规模型。
	return len(model) >= 5 && (model[:5] == "gpt-5" || (len(model) >= 2 && model[:2] == "o1"))
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

// TalOpenAIClient 兼容 OpenAI 的 Tal 内部 openai-compatible 服务
type TalOpenAIClient struct {
	config     config.LLMProviderConfig
	httpClient *http.Client
}

// NewTalOpenAIClient 创建 TalOpenAI 客户端
func NewTalOpenAIClient(cfg config.LLMProviderConfig) *TalOpenAIClient {
	return &TalOpenAIClient{
		config: cfg,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// Complete 完成文本生成（Tal OpenAI-compatible）
func (c *TalOpenAIClient) Complete(ctx context.Context, messages []Message, schema *JSONSchema) (string, error) {
	// 构造与 OpenAI chat/completions 类似的请求体
	reqBody := map[string]any{
		"model":    c.config.Model,
		"messages": messages,
		"stream":   false,
	}

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

	// Endpoint provided in the user's example uses path /openai-compatible/v1/chat/completions
	req, err := http.NewRequestWithContext(ctx, "POST", c.config.APIURL+"/openai-compatible/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	// Tal uses Bearer in Authorization header in the user's curl example
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

	// First try to parse as OpenAI-style response
	var oa struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(respBody, &oa); err == nil && len(oa.Choices) > 0 {
		content := oa.Choices[0].Message.Content
		if content != "" {
			// Normalize content: it might be wrapped in markdown fences, be an escaped JSON string，
			// 或包含前后文本。尝试提取有效的 JSON 值并以紧凑的 JSON 格式返回，以便调用方如 director.decideLLM 可以 json.Unmarshal 。
			trimmed := strings.TrimSpace(content)

			// 如果内容是 markdown 围栏（例如 ```json\n{...}\n```），则去除围栏
			if strings.HasPrefix(trimmed, "```") {
				// 去除前导 ``` 和尾部 ```
				end := strings.LastIndex(trimmed, "```")
				if end > 3 {
					inner := trimmed[3:end]
					inner = strings.TrimSpace(inner)
					// 如果第一行是类似 "json" 的语言提示，则删除它
					if idx := strings.IndexAny(inner, "\n\r"); idx > 0 {
						firstLine := inner[:idx]
						if isAlphaString(firstLine) {
							inner = strings.TrimSpace(inner[idx:])
						}
					}
					trimmed = strings.TrimSpace(inner)
				}
			}

			// 如果 trimmed 现在看起来像 JSON，尝试解组为通用类型并重新序列化为紧凑格式
			if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[' || trimmed[0] == '"') {
				var raw any
				if err := json.Unmarshal([]byte(trimmed), &raw); err == nil {
					b, _ := json.Marshal(raw)
					return string(b), nil
				}

				// 可能是包含 JSON 对象（转义）的 JSON 字符串，尝试解引号+解组
				var possible string
				if err := json.Unmarshal([]byte(trimmed), &possible); err == nil {
					possible = strings.TrimSpace(possible)
					if len(possible) > 0 && (possible[0] == '{' || possible[0] == '[') {
						var raw2 any
						if err2 := json.Unmarshal([]byte(possible), &raw2); err2 == nil {
							b, _ := json.Marshal(raw2)
							return string(b), nil
						}
					}
				}
			}

			// 作为最后的尝试，如果内容中某处包含 JSON 对象，尝试提取第一个 {
			if idx := strings.Index(trimmed, "{"); idx >= 0 {
				suffix := strings.TrimSpace(trimmed[idx:])
				var raw any
				if err := json.Unmarshal([]byte(suffix), &raw); err == nil {
					b, _ := json.Marshal(raw)
					return string(b), nil
				}
			}

			// 回退：返回原始内容
			return content, nil
		}
	}

	// If not OpenAI style, try to accept raw JSON object (e.g., Claude returning a JSON object)
	// Trim leading spaces and check first byte
	trimmed := bytes.TrimSpace(respBody)
	if len(trimmed) > 0 {
		first := trimmed[0]
		if first == '{' || first == '[' || first == '"' {
			// Attempt to unmarshal into generic
			var raw any
			if err := json.Unmarshal(trimmed, &raw); err == nil {
				switch v := raw.(type) {
				case string:
					// The body was a JSON string containing the content
					return v, nil
				case map[string]any, []any:
					// Return compact JSON string so callers can parse if needed
					b, _ := json.Marshal(v)
					return string(b), nil
				default:
					b, _ := json.Marshal(v)
					return string(b), nil
				}
			}
		}
	}

	// Fallback: return raw body as string
	if len(respBody) > 0 {
		return string(respBody), nil
	}

	return "", fmt.Errorf("empty response body")
}

// helper: check if a string is alphabetic (used to detect language hints like "json")
func isAlphaString(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') {
			continue
		}
		return false
	}
	return true
}
