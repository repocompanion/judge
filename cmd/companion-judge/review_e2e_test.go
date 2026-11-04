package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const (
	fixtureDir      = "../../testdata/reviews/sqs-idempotency"
	fixtureGuidance = `[{"id":"sqs-consumer","title":"SQS consumer","path":"org/patterns/messaging/sqs.md","kind":"pattern","status":"approved","scope":"org","score":3.8}]`
	fixtureFinding  = `{"findings":[{"doc_id":"sqs-consumer","doc_path":"org/patterns/messaging/sqs.md","doc_quote":"MUST make the handler idempotent","code_path":"internal/consumer/consumer.go","line":999,"code_quote":"process(m)","severity":"error","category":"pattern","message":"the handler processes messages without an idempotency key","remediation":"derive a key from the message id before processing","confidence":0.9}]}`
)

func fixturePaths(t *testing.T) (store, diff string) {
	t.Helper()
	store = filepath.Join(fixtureDir, "docs")
	diff = filepath.Join(fixtureDir, "change.patch")
	for _, p := range []string{store, diff} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("fixture %s: %v", p, err)
		}
	}
	return store, diff
}

// companionStub emits fixed guidance JSON regardless of its arguments. The
// judge still reads document bodies from --store.
func companionStub(t *testing.T, results string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the companion stub is a shell script")
	}
	path := filepath.Join(t.TempDir(), "companion")
	script := "#!/bin/sh\nprintf '%s' '" + results + "'\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func modelStub(t *testing.T, content string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("model path = %q, want /chat/completions", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]string{"content": content}}},
			"usage":   map[string]int{"prompt_tokens": 120, "completion_tokens": 20},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func runReviewCLI(t *testing.T, companion, store, diff, baseURL string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run([]string{
		"review",
		"--store", store,
		"--project", "acme/api",
		"--lang", "go",
		"--companion", companion,
		"--diff", diff,
		"--title", "add SQS consumer",
		"--base-url", baseURL,
		"--model", "test-model",
		"--json",
	}, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestReviewEndToEnd(t *testing.T) {
	store, diff := fixturePaths(t)
	srv := modelStub(t, fixtureFinding)
	code, out, errOut := runReviewCLI(t, companionStub(t, fixtureGuidance), store, diff, srv.URL)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, errOut)
	}

	var rep struct {
		Schema   string `json:"schema"`
		Findings []struct {
			Line  int    `json:"line"`
			DocID string `json:"doc_id"`
		} `json:"findings"`
		Dropped []json.RawMessage `json:"dropped"`
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("decode report: %v\n%s", err, out)
	}
	if rep.Schema != "companion.judge/v1" {
		t.Fatalf("schema = %q", rep.Schema)
	}
	if len(rep.Findings) != 1 || rep.Findings[0].Line != 12 || rep.Findings[0].DocID != "sqs-consumer" {
		t.Fatalf("findings = %+v, want one verified finding at line 12", rep.Findings)
	}
	if rep.Dropped == nil || len(rep.Dropped) != 0 {
		t.Fatalf("dropped = %v, want []", rep.Dropped)
	}
}

func TestReviewEndToEndEmptyFindings(t *testing.T) {
	store, diff := fixturePaths(t)
	srv := modelStub(t, `{"findings":[]}`)
	code, out, errOut := runReviewCLI(t, companionStub(t, fixtureGuidance), store, diff, srv.URL)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, errOut)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatalf("decode report: %v\n%s", err, out)
	}
	if got := string(raw["findings"]); got != "[]" {
		t.Fatalf("findings = %s, want []", got)
	}
	if got := string(raw["dropped"]); got != "[]" {
		t.Fatalf("dropped = %s, want []", got)
	}
}

func TestReviewRejectsOnlyNonApproved(t *testing.T) {
	store, diff := fixturePaths(t)
	guidance := `[{"id":"legacy-queue","title":"Legacy queue consumer","path":"org/patterns/messaging/legacy-queue.md","kind":"pattern","status":"deprecated","scope":"org","score":3.8}]`
	code, _, errOut := runReviewCLI(t, companionStub(t, guidance), store, diff, "http://127.0.0.1:1/v1")
	if code != 3 {
		t.Fatalf("exit = %d, want 3; stderr = %s", code, errOut)
	}
	if !strings.Contains(errOut, "not approved") {
		t.Fatalf("stderr = %q, want a not-approved error", errOut)
	}
}

// TestReviewWithRealCompanion exercises the actual `companion guidance --store`
// contract. It is skipped unless COMPANION_BIN names a build with --store.
func TestReviewWithRealCompanion(t *testing.T) {
	bin := strings.TrimSpace(os.Getenv("COMPANION_BIN"))
	if bin == "" {
		t.Skip("set COMPANION_BIN to a companion build with --store support")
	}
	store, diff := fixturePaths(t)
	srv := modelStub(t, fixtureFinding)
	code, out, errOut := runReviewCLI(t, bin, store, diff, srv.URL)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, errOut)
	}

	var rep struct {
		Findings []struct {
			Line int `json:"line"`
		} `json:"findings"`
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("decode report: %v\n%s", err, out)
	}
	if len(rep.Findings) != 1 || rep.Findings[0].Line != 12 {
		t.Fatalf("findings = %+v, want one verified finding at line 12", rep.Findings)
	}
}
