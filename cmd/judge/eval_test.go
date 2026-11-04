package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/repocompanion/judge/internal/eval"
)

const fixtureTwoFindings = `{"findings":[{"doc_id":"sqs-consumer","doc_path":"org/patterns/messaging/sqs.md","doc_quote":"MUST NOT log raw message bodies.","code_path":"internal/consumer/consumer.go","line":999,"code_quote":"log.Printf(\"message body: %s\", m.Body)","severity":"error","category":"pattern","message":"the change logs the raw message body","remediation":"log the message id instead","confidence":0.9},{"doc_id":"sqs-consumer","doc_path":"org/patterns/messaging/sqs.md","doc_quote":"MUST NOT log raw message bodies.","code_path":"internal/consumer/consumer.go","line":11,"code_quote":"for _, m := range msgs {","severity":"error","category":"pattern","message":"the loop handles raw messages","remediation":"handle the messages differently","confidence":0.8}]}`

func runEvalCLI(t *testing.T, companion, baseURL string, extra ...string) (int, string, string) {
	t.Helper()
	args := []string{
		"eval",
		"--cases", "../../testdata/reviews",
		"--companion", companion,
		"--base-url", baseURL,
		"--model", "test-model",
		"--runs", "1",
	}
	args = append(args, extra...)
	var stdout, stderr bytes.Buffer
	code := run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestEvalEndToEnd(t *testing.T) {
	srv := modelStub(t, fixtureFinding)
	code, out, errOut := runEvalCLI(t, companionStub(t, fixtureGuidance), srv.URL, "--no-baseline", "--json")
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, errOut)
	}

	var rep eval.Report
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("decode report: %v\n%s", err, out)
	}
	if rep.Schema != eval.Schema || len(rep.Cases) < 2 {
		t.Fatalf("report = %+v", rep)
	}
	if rep.Aggregate.Verified < 1 {
		t.Fatalf("aggregate = %+v, want at least one verified finding", rep.Aggregate)
	}
	if len(rep.Errors) != 0 {
		t.Fatalf("errors = %v", rep.Errors)
	}

	var benign eval.CaseResult
	for _, c := range rep.Cases {
		if c.Name == "sqs-benign-log" {
			benign = c
		}
	}
	if benign.Name == "" {
		t.Fatal("benign case missing")
	}
	if benign.Verified != 0 || benign.EvidenceDropped != 1 {
		t.Fatalf("benign = %+v, want no verified finding and one dropped evidence", benign)
	}
}

func TestEvalEmitBaseline(t *testing.T) {
	srv := modelStub(t, fixtureFinding)
	code, out, errOut := runEvalCLI(t, companionStub(t, fixtureGuidance), srv.URL, "--emit-baseline")
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, errOut)
	}
	var base eval.Baseline
	if err := json.Unmarshal([]byte(out), &base); err != nil {
		t.Fatalf("decode baseline: %v\n%s", err, out)
	}
	if base.Schema != eval.BaselineSchema || base.Model != "test-model" || len(base.Cases) < 2 {
		t.Fatalf("baseline = %+v", base)
	}
	if base.Cases["sqs-body-logging"].Precision != 1 {
		t.Fatalf("body-logging baseline = %+v", base.Cases["sqs-body-logging"])
	}
}

func TestEvalBaselineRegression(t *testing.T) {
	srv := modelStub(t, fixtureTwoFindings)
	baseline := filepath.Join(t.TempDir(), "baseline.json")
	body := `{
  "schema": "companion.judge.eval-baseline/v1",
  "model": "test-model",
  "cases": {
    "sqs-body-logging": {
      "tp": 1, "precision": 1.0, "recall": 1.0,
      "citation_validity": 1.0, "stable": true
    }
  },
  "aggregate": {
    "precision": 1.0, "recall": 1.0,
    "citation_validity": 1.0, "stable": true
  }
}`
	if err := os.WriteFile(baseline, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out, errOut := runEvalCLI(t, companionStub(t, fixtureGuidance), srv.URL, "--baseline", baseline, "--json")
	if code != 4 {
		t.Fatalf("exit = %d, want 4 (regression); stderr = %s", code, errOut)
	}
	var rep eval.Report
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("decode report: %v\n%s", err, out)
	}
	if rep.Baseline == nil || len(rep.Baseline.Regressions) == 0 {
		t.Fatalf("baseline comparison = %+v, want a regression", rep.Baseline)
	}
	if rep.Aggregate.Precision >= 1 {
		t.Fatalf("aggregate precision = %.3f, want a false positive", rep.Aggregate.Precision)
	}
}
