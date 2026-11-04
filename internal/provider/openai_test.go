package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOpenAIComplete(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]string{"content": `{"findings":[]}`}}},
			"usage":   map[string]int{"prompt_tokens": 11, "completion_tokens": 3},
		})
	}))
	defer srv.Close()

	p, err := NewOpenAI(srv.URL, "test-key", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := p.Complete(context.Background(), Request{
		Model: "test-model", System: "system", User: "user", Temperature: 0, MaxTokens: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/chat/completions" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotAuth != "Bearer test-key" {
		t.Fatalf("auth = %q", gotAuth)
	}
	if gotBody["model"] != "test-model" {
		t.Fatalf("model = %v", gotBody["model"])
	}
	if resp.Content != `{"findings":[]}` || resp.PromptTokens != 11 || resp.CompletionTokens != 3 {
		t.Fatalf("response = %+v", resp)
	}
}

func TestOpenAIErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"boom"}`, http.StatusUnauthorized)
	}))
	defer srv.Close()
	p, err := NewOpenAI(srv.URL, "key", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Complete(context.Background(), Request{Model: "m", System: "s", User: "u"})
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("error = %v, want a 401", err)
	}
}

func TestNewOpenAIRejectsRemoteHTTP(t *testing.T) {
	if _, err := NewOpenAI("http://example.com/v1", "key", time.Second); err == nil {
		t.Fatal("remote http was accepted")
	}
	if !IsLoopbackURL("http://127.0.0.1:8080/v1") {
		t.Fatal("loopback URL not detected")
	}
}

func TestOpenAIReportsTruncation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{
				"finish_reason": "length",
				"message":       map[string]string{"content": "", "reasoning_content": "still thinking"},
			}},
			"usage": map[string]any{
				"prompt_tokens":     10,
				"completion_tokens": 4000,
				"completion_tokens_details": map[string]int{
					"reasoning_tokens": 3900,
				},
			},
		})
	}))
	defer srv.Close()
	p, err := NewOpenAI(srv.URL, "key", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Complete(context.Background(), Request{Model: "m", System: "s", User: "u"})
	if err == nil || !strings.Contains(err.Error(), "truncated") {
		t.Fatalf("error = %v, want a truncation error", err)
	}
	for _, want := range []string{"4000", "3900"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want %s", err, want)
		}
	}
}

func TestOpenAIReportsReasoningWithoutContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{
				"finish_reason": "stop",
				"message":       map[string]string{"content": "", "reasoning_content": "thoughts"},
			}},
		})
	}))
	defer srv.Close()
	p, err := NewOpenAI(srv.URL, "key", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Complete(context.Background(), Request{Model: "m", System: "s", User: "u"})
	if err == nil || !strings.Contains(err.Error(), "reasoning but no final content") {
		t.Fatalf("error = %v, want a reasoning-only error", err)
	}
}
