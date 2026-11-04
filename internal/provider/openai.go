// Package provider calls an OpenAI-compatible chat-completions endpoint.
package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Request is one chat completion request.
type Request struct {
	Model       string
	System      string
	User        string
	Temperature float64
	MaxTokens   int
	JSONMode    bool
}

// Response is the text and token usage returned by the provider.
type Response struct {
	Content          string
	PromptTokens     int
	CompletionTokens int
}

// Provider is a model endpoint.
type Provider interface {
	Name() string
	Complete(ctx context.Context, req Request) (Response, error)
}

// OpenAI talks to any OpenAI-compatible /chat/completions endpoint.
type OpenAI struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

// NewOpenAI validates the endpoint. HTTP is accepted only for loopback hosts.
func NewOpenAI(baseURL, apiKey string, timeout time.Duration) (*OpenAI, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("invalid base URL %q", baseURL)
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && isLoopback(u.Hostname())) {
		return nil, fmt.Errorf("base URL must use https (http is allowed only for localhost)")
	}
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	return &OpenAI{BaseURL: baseURL, APIKey: strings.TrimSpace(apiKey), HTTP: &http.Client{Timeout: timeout}}, nil
}

func (o *OpenAI) Name() string { return "openai-compatible" }

func (o *OpenAI) Complete(ctx context.Context, req Request) (Response, error) {
	payload := map[string]any{
		"model":       req.Model,
		"temperature": req.Temperature,
		"messages": []map[string]string{
			{"role": "system", "content": req.System},
			{"role": "user", "content": req.User},
		},
	}
	if req.MaxTokens > 0 {
		payload["max_tokens"] = req.MaxTokens
	}
	if req.JSONMode {
		payload["response_format"] = map[string]string{"type": "json_object"}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Response{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Response{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if o.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+o.APIKey)
	}
	resp, err := o.HTTP.Do(httpReq)
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Response{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Response{}, fmt.Errorf("model returned %s: %s", resp.Status, snippet(raw))
	}
	var parsed struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens            int `json:"prompt_tokens"`
			CompletionTokens        int `json:"completion_tokens"`
			CompletionTokensDetails struct {
				ReasoningTokens int `json:"reasoning_tokens"`
			} `json:"completion_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Response{}, fmt.Errorf("decode model response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return Response{}, fmt.Errorf("model returned no choices")
	}
	choice := parsed.Choices[0]
	if choice.FinishReason == "length" {
		return Response{}, fmt.Errorf("model output was truncated at %d completion tokens (%d reasoning tokens); increase --max-tokens",
			parsed.Usage.CompletionTokens, parsed.Usage.CompletionTokensDetails.ReasoningTokens)
	}
	if strings.TrimSpace(choice.Message.Content) == "" {
		if strings.TrimSpace(choice.Message.ReasoningContent) != "" {
			return Response{}, fmt.Errorf("model returned reasoning but no final content (finish_reason=%q)", choice.FinishReason)
		}
		return Response{}, fmt.Errorf("model returned no content (finish_reason=%q)", choice.FinishReason)
	}
	return Response{
		Content:          choice.Message.Content,
		PromptTokens:     parsed.Usage.PromptTokens,
		CompletionTokens: parsed.Usage.CompletionTokens,
	}, nil
}

// IsLoopbackURL reports whether raw points at a loopback host.
func IsLoopbackURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return isLoopback(u.Hostname())
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 512 {
		s = s[:512] + "..."
	}
	return s
}
