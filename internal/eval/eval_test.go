package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/repocompanion/judge/internal/review"
)

func TestMatch(t *testing.T) {
	expected := []ExpectedFinding{
		{DocID: "d1", CodePath: "internal/a.go", LineStart: 10, LineEnd: 12},
		{DocPath: "org/b.md", CodePath: "b.go", Line: 5},
	}
	findings := []review.Finding{
		{DocID: "d1", DocPath: "org/a.md", CodePath: "internal/a.go", Line: 11},
		{DocID: "d2", DocPath: "org/b.md", CodePath: "a/b.go", Line: 5},
		{DocID: "d1", DocPath: "org/a.md", CodePath: "internal/a.go", Line: 20},
	}
	tp, fp, fn := Match(expected, findings)
	if tp != 2 || fp != 1 || fn != 0 {
		t.Fatalf("tp, fp, fn = %d, %d, %d; want 2, 1, 0", tp, fp, fn)
	}

	tp, fp, fn = Match(expected, findings[:1])
	if tp != 1 || fp != 0 || fn != 1 {
		t.Fatalf("partial: tp, fp, fn = %d, %d, %d; want 1, 0, 1", tp, fp, fn)
	}

	tp, fp, fn = Match(nil, findings[:1])
	if tp != 0 || fp != 1 || fn != 0 {
		t.Fatalf("benign: tp, fp, fn = %d, %d, %d; want 0, 1, 0", tp, fp, fn)
	}
}

func TestEvidenceDropped(t *testing.T) {
	dropped := []review.Dropped{
		{Reason: "doc_quote_not_found"},
		{Reason: "max_findings"},
		{Reason: "message_missing"},
		{Reason: "code_path_not_changed"},
	}
	if got := evidenceDropped(dropped); got != 2 {
		t.Fatalf("evidenceDropped = %d, want 2", got)
	}
}

func TestScoreRun(t *testing.T) {
	r := &review.Report{
		Findings: []review.Finding{{DocID: "d", CodePath: "a.go", Line: 4}},
		Dropped:  []review.Dropped{{Reason: "doc_quote_not_found"}},
		Usage:    review.Usage{InputTokens: 10, OutputTokens: 2},
	}
	got := scoreRun(nil, r)
	if got.TP != 0 || got.FP != 1 || got.FN != 0 {
		t.Fatalf("tp, fp, fn = %d, %d, %d; want 0, 1, 0", got.TP, got.FP, got.FN)
	}
	if got.Precision != 0 || got.Recall != 1 || got.CitationValidity != 0.5 {
		t.Fatalf("metrics = %+v", got)
	}
	if got.Usage.InputTokens != 10 || got.Usage.OutputTokens != 2 {
		t.Fatalf("usage = %+v", got.Usage)
	}
}

func TestStability(t *testing.T) {
	if stable, unique := stability([]string{"", ""}); !stable || unique != 1 {
		t.Fatalf("stable, unique = %v, %d", stable, unique)
	}
	if stable, unique := stability([]string{"a", "b"}); stable || unique != 2 {
		t.Fatalf("stable, unique = %v, %d", stable, unique)
	}
	if stable, unique := stability(nil); stable || unique != 0 {
		t.Fatalf("stable, unique = %v, %d", stable, unique)
	}
}

func TestCompareBaseline(t *testing.T) {
	rep := &Report{
		Schema: Schema, Model: "m",
		Prompt: review.PromptInfo{Version: "1", Hash: "sha256:current"},
		Cases: []CaseResult{{
			Name:    "a",
			Metrics: Metrics{Precision: 0.5, Recall: 1, CitationValidity: 0.9, Stable: true},
		}},
		Aggregate: Metrics{Precision: 0.5, Recall: 1, CitationValidity: 0.9, Stable: true},
	}
	base := &Baseline{
		Schema: BaselineSchema, Model: "m",
		Prompt: review.PromptInfo{Version: "1", Hash: "sha256:baseline"},
		Cases: map[string]BaselineCase{
			"a": {Metrics: Metrics{Precision: 1, Recall: 1, CitationValidity: 1, Stable: true}},
		},
		Aggregate: Metrics{Precision: 1, Recall: 1, CitationValidity: 1, Stable: true},
	}
	c := Compare(rep, base, "baseline.json")
	if c == nil {
		t.Fatal("Compare returned nil")
	}
	if len(c.Regressions) != 4 {
		t.Fatalf("regressions = %v, want four", c.Regressions)
	}
	if len(c.Warnings) != 1 || !strings.Contains(c.Warnings[0], "prompt hash") {
		t.Fatalf("warnings = %v", c.Warnings)
	}

	equal := &Baseline{
		Schema: BaselineSchema, Model: "m",
		Cases: map[string]BaselineCase{
			"a": {Metrics: Metrics{Precision: 0.5, Recall: 1, CitationValidity: 0.9, Stable: true}},
		},
		Aggregate: Metrics{Precision: 0.5, Recall: 1, CitationValidity: 0.9, Stable: true},
	}
	if c := Compare(rep, equal, "baseline.json"); len(c.Regressions) != 0 {
		t.Fatalf("equal baseline regressed: %v", c.Regressions)
	}

	missing := &Baseline{
		Schema: BaselineSchema,
		Cases:  map[string]BaselineCase{},
	}
	if c := Compare(rep, missing, "baseline.json"); len(c.Regressions) != 0 {
		t.Fatalf("aggregate-only baseline regressed: %v", c.Regressions)
	}
}

func TestBuildBaseline(t *testing.T) {
	rep := &Report{
		Schema: Schema, Model: "m",
		Prompt: review.PromptInfo{ID: "review", Version: "1", Hash: "sha256:x"},
		Cases: []CaseResult{{
			Name: "a", Runs: 3,
			Metrics: Metrics{TP: 1, Precision: 1, CitationValidity: 1, Stable: true},
		}},
		Aggregate: Metrics{TP: 1, Precision: 1, CitationValidity: 1, Stable: true},
	}
	b := BuildBaseline(rep)
	if b.Schema != BaselineSchema || b.Model != "m" || len(b.Cases) != 1 {
		t.Fatalf("baseline = %+v", b)
	}
	if b.Cases["a"].Runs != 3 || b.Cases["a"].Precision != 1 {
		t.Fatalf("case = %+v", b.Cases["a"])
	}
}

func TestLoadBaselineMissing(t *testing.T) {
	b, err := LoadBaseline(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil || b != nil {
		t.Fatalf("LoadBaseline = %+v, %v; want nil, nil", b, err)
	}
}

func TestCRLFEvalFixture(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "reviews", "sqs-crlf", "change.patch"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "\r\n") {
		t.Fatal("sqs-crlf/change.patch must keep CRLF line endings; check .gitattributes")
	}
}

func TestLoadCases(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "case-a")
	if err := os.MkdirAll(filepath.Join(dir, "docs", "org"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "case.json"), `{"project":"acme/api","task":"add x"}`)
	writeFile(t, filepath.Join(dir, "expected.json"), `{"findings":[{"doc_id":"d","code_path":"x.go","line":2}]}`)
	writeFile(t, filepath.Join(dir, "change.patch"), `diff --git a/x.go b/x.go
--- a/x.go
+++ b/x.go
@@ -1 +1,2 @@
 package x
+var X = 1
`)
	os.MkdirAll(filepath.Join(root, "not-a-case"), 0o755)

	cases, err := LoadCases(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 1 || cases[0].Name != "case-a" {
		t.Fatalf("cases = %+v", cases)
	}
	if cases[0].Meta.Project != "acme/api" || len(cases[0].Expected.Findings) != 1 {
		t.Fatalf("case = %+v", cases[0])
	}
	if len(cases[0].Diff.Files) != 1 || cases[0].Diff.Files[0].NewPath != "x.go" {
		t.Fatalf("diff = %+v", cases[0].Diff)
	}
}

func TestLoadCasesRejectsBadLineRange(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "bad")
	os.MkdirAll(filepath.Join(dir, "docs", "org"), 0o755)
	writeFile(t, filepath.Join(dir, "case.json"), `{"project":"acme/api"}`)
	writeFile(t, filepath.Join(dir, "expected.json"), `{"findings":[{"doc_id":"d","code_path":"x.go","line_start":5,"line_end":2}]}`)
	writeFile(t, filepath.Join(dir, "change.patch"), `diff --git a/x.go b/x.go
--- a/x.go
+++ b/x.go
@@ -1 +1,2 @@
 package x
+var X = 1
`)
	if _, err := LoadCases(root); err == nil || !strings.Contains(err.Error(), "line_end") {
		t.Fatalf("error = %v, want a line range error", err)
	}
}

func TestMarkdown(t *testing.T) {
	rep := &Report{
		Schema: Schema, Model: "m",
		Prompt: review.PromptInfo{ID: "review", Version: "1", Hash: "sha256:abcdef"},
		Runs:   2,
		Cases: []CaseResult{{
			Name: "a", Runs: 2,
			Metrics: Metrics{TP: 1, Precision: 1, Recall: 1, CitationValidity: 1, Stable: true},
		}},
		Aggregate: Metrics{TP: 1, Precision: 1, Recall: 1, CitationValidity: 1, Stable: true},
		Baseline:  &Comparison{Path: "baseline.json"},
	}
	md := rep.Markdown()
	for _, want := range []string{"Judge eval", "| a |", "**Aggregate**", "Baseline passed"} {
		if !strings.Contains(md, want) {
			t.Fatalf("markdown missing %q:\n%s", want, md)
		}
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
