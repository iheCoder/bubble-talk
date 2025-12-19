package realtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// EphemeralKeyResponse 是 OpenAI Realtime 创建 session 后返回的 client_secret。
// 注意：这个 key 只能在短时间内用于浏览器建立 WebRTC/WebSocket Realtime 连接，
// 不能替代长期 API Key，更不能暴露服务端的 OPENAI_API_KEY。
type EphemeralKeyResponse struct {
	ClientSecret struct {
		Value     string `json:"value"`
		ExpiresAt int64  `json:"expires_at"`
	} `json:"client_secret"`
}

// CreateSessionRequest 是创建 Realtime session 的最小请求体。
// 字段会随着 OpenAI 的 Realtime API 演进而扩展；第一阶段只保留必要项。
type CreateSessionRequest struct {
	Model        string `json:"model"`
	Voice        string `json:"voice,omitempty"`
	Instructions string `json:"instructions,omitempty"`
}

// Client 封装 OpenAI Realtime 的“签发 ephemeral key”能力。
// 设计目的：把 OpenAI API Key 的使用限制在服务端，前端只拿到短期凭证。
type Client struct {
	HTTPClient *http.Client
	APIKey     string
	BaseURL    string // 默认 https://api.openai.com
}

func (c *Client) CreateEphemeralKey(ctx context.Context, req CreateSessionRequest) (EphemeralKeyResponse, error) {
	if c.APIKey == "" {
		return EphemeralKeyResponse{}, errors.New("OPENAI_API_KEY is empty")
	}
	baseURL := c.BaseURL
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return EphemeralKeyResponse{}, fmt.Errorf("marshal request: %w", err)
	}

	// OpenAI: POST /v1/realtime/sessions
	// 返回：{ client_secret: { value, expires_at } }
	url := baseURL + "/v1/realtime/sessions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return EphemeralKeyResponse{}, fmt.Errorf("new request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return EphemeralKeyResponse{}, fmt.Errorf("openai request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// 读取少量错误信息，便于本地调试；不要把整段 body（可能很长）透传给上层。
		limited, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return EphemeralKeyResponse{}, fmt.Errorf("openai realtime sessions: status=%d body=%s", resp.StatusCode, string(limited))
	}

	var out EphemeralKeyResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return EphemeralKeyResponse{}, fmt.Errorf("decode response: %w", err)
	}
	if out.ClientSecret.Value == "" {
		return EphemeralKeyResponse{}, errors.New("openai returned empty client_secret.value")
	}
	return out, nil
}
