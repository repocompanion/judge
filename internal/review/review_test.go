package review

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/repocompanion/companion-judge/internal/change"
	"github.com/repocompanion/companion-judge/internal/docs"
	"github.com/repocompanion/companion-judge/internal/provider"
)

const testDiff = `diff --git a/internal/consumer/consumer.go b/internal/consumer/consumer.go
index 1111111..2222222 100644
--- a/internal/consumer/consumer.go
+++ b/internal/consumer/consumer.go
@@ -10,3 +10,6 @@ func Consume(body string) error {
 	msgs := receive()
+	for _, m := range msgs {
+		handle(m)
+	}
 	return nil
 }
`

func testDoc() docs.Doc {
	return docs.Doc{
		ID:     "sqs-consumer",
		Title:  "SQS consumer",
		Path:   "org/patterns/messaging/sqs.md",
		Kind:   "pattern",
		Status: "approved",
		Scope:  "org",
		Body:   "# SQS consumer\n\n- MUST make the handler idempotent.\n- MUST configure a dead-letter queue.\n",
	}
}

func testPack(t *testing.T) *Pack {
	t.Helper()
	d, err := change.Parse([]byte(testDiff))
	if err != nil {
		t.Fatal(err)
	}
	return BuildPack(Config{
		Task:         "implement an SQS consumer",
		Project:      "acme/api",
		Diff:         d,
		Documents:    []docs.Doc{testDoc()},
		MaxDocs:      5,
		MaxDocBytes:  10000,
		MaxDiffBytes: 10000,
	})
}

func validRaw() RawFinding {
	return RawFinding{
		DocID:       "sqs-consumer",
		DocPath:     "org/patterns/messaging/sqs.md",
		DocQuote:    "MUST make the handler idempotent",
		CodePath:    "internal/consumer/consumer.go",
		Line:        999,
		CodeQuote:   "handle(m)",
		Severity:    "warn",
		Category:    "pattern",
		Message:     "the handler is not idempotent",
		Remediation: "derive an idempotency key before handling the message",
		Confidence:  0.8,
	}
}

func TestVerifyAcceptsFinding(t *testing.T) {
	v := Verify(testPack(t), []RawFinding{validRaw()}, 10)
	if len(v.Findings) != 1 {
		t.Fatalf("findings = %d, dropped = %+v", len(v.Findings), v.Dropped)
	}
	f := v.Findings[0]
	if f.Line != 12 {
		t.Fatalf("line = %d, want 12 (recomputed from the diff)", f.Line)
	}
	if f.DocPath != "org/patterns/messaging/sqs.md" || f.Severity != "warn" {
		t.Fatalf("finding = %+v", f)
	}
}

func TestVerifyDropsUnverifiedEvidence(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*RawFinding)
		reason string
	}{
		{"unknown doc", func(r *RawFinding) { r.DocID = "nope" }, "doc_not_in_pack"},
		{"bad doc quote", func(r *RawFinding) { r.DocQuote = "must do something else entirely" }, "doc_quote_not_found"},
		{"path not changed", func(r *RawFinding) { r.CodePath = "internal/other.go" }, "code_path_not_changed"},
		{"bad code quote", func(r *RawFinding) { r.CodeQuote = "nonexistent call()" }, "code_quote_not_found"},
		{"short code quote", func(r *RawFinding) { r.CodeQuote = "if" }, "code_quote_too_short"},
		{"missing message", func(r *RawFinding) { r.Message = "" }, "message_missing"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := validRaw()
			tc.mutate(&raw)
			v := Verify(testPack(t), []RawFinding{raw}, 10)
			if len(v.Findings) != 0 {
				t.Fatalf("finding survived: %+v", v.Findings)
			}
			if len(v.Dropped) != 1 || v.Dropped[0].Reason != tc.reason {
				t.Fatalf("dropped = %+v, want %s", v.Dropped, tc.reason)
			}
		})
	}
}

type fakeProvider struct {
	content string
	err     error
	req     provider.Request
}

func (f *fakeProvider) Name() string { return "fake" }

func (f *fakeProvider) Complete(_ context.Context, req provider.Request) (provider.Response, error) {
	f.req = req
	if f.err != nil {
		return provider.Response{}, f.err
	}
	return provider.Response{Content: f.content, PromptTokens: 120, CompletionTokens: 30}, nil
}

func TestRunProducesVerifiedReport(t *testing.T) {
	fake := &fakeProvider{content: `{"findings":[{"doc_id":"sqs-consumer","doc_path":"org/patterns/messaging/sqs.md","doc_quote":"MUST make the handler idempotent","code_path":"internal/consumer/consumer.go","line":999,"code_quote":"handle(m)","severity":"warn","category":"pattern","message":"the handler is not idempotent","remediation":"derive an idempotency key","confidence":0.8}]}`}
	d, err := change.Parse([]byte(testDiff))
	if err != nil {
		t.Fatal(err)
	}
	rep, err := Run(context.Background(), Config{
		Task:         "implement an SQS consumer",
		Project:      "acme/api",
		Diff:         d,
		Documents:    []docs.Doc{testDoc()},
		MaxDocs:      5,
		MaxDocBytes:  10000,
		MaxDiffBytes: 10000,
		MaxFindings:  5,
		Provider:     fake,
		Model:        "test-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Schema != Schema || rep.Verdict != "advisory" || !rep.ReviewComplete {
		t.Fatalf("report = %+v", rep)
	}
	if len(rep.Findings) != 1 || rep.Findings[0].Line != 12 {
		t.Fatalf("findings = %+v", rep.Findings)
	}
	if rep.Docs.Cited != 1 || rep.Usage.InputTokens != 120 || rep.Usage.OutputTokens != 30 {
		t.Fatalf("report = %+v", rep)
	}
	if fake.req.Model != "test-model" {
		t.Fatalf("model = %q", fake.req.Model)
	}
	if !strings.Contains(fake.req.System, "Companion Judge") {
		t.Fatal("system prompt was not passed")
	}
	for _, want := range []string{"sqs-consumer", "org/patterns/messaging/sqs.md", "<<<DIFF", "untrusted"} {
		if !strings.Contains(fake.req.User, want) {
			t.Fatalf("user prompt missing %q:\n%s", want, fake.req.User)
		}
	}
}

func TestRunRequiresDocuments(t *testing.T) {
	d, err := change.Parse([]byte(testDiff))
	if err != nil {
		t.Fatal(err)
	}
	_, err = Run(context.Background(), Config{Diff: d, Provider: &fakeProvider{}})
	if !errors.Is(err, ErrNoDocuments) {
		t.Fatalf("error = %v, want ErrNoDocuments", err)
	}
}

func TestParseFindingsFenced(t *testing.T) {
	got, err := parseFindings("```json\n{\"findings\":[]}\n```")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("findings = %+v", got)
	}
	if _, err := parseFindings("not json"); err == nil {
		t.Fatal("invalid output was accepted")
	}
}

func TestMarkdownIncludesEvidence(t *testing.T) {
	v := Verify(testPack(t), []RawFinding{validRaw()}, 10)
	rep := &Report{
		Verdict: "advisory", Model: ModelInfo{ID: "test-model"},
		Docs: DocsInfo{InPack: 1}, Change: ChangeInfo{Files: 1},
		Findings: v.Findings,
	}
	md := rep.Markdown()
	for _, want := range []string{"Companion Judge", "org/patterns/messaging/sqs.md", "internal/consumer/consumer.go:12", "MUST make the handler idempotent"} {
		if !strings.Contains(md, want) {
			t.Fatalf("markdown missing %q:\n%s", want, md)
		}
	}
}
