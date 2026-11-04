// Package eval runs review fixtures and scores precision, recall, citation
// validity and stability against a recorded baseline.
package eval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/repocompanion/judge/internal/change"
	"github.com/repocompanion/judge/internal/docs"
	"github.com/repocompanion/judge/internal/provider"
	"github.com/repocompanion/judge/internal/review"
)

const (
	// Schema is the eval report schema.
	Schema = "companion.judge.eval/v1"
	// BaselineSchema is the baseline schema.
	BaselineSchema = "companion.judge.eval-baseline/v1"
)

// CaseMeta is the case.json contract.
type CaseMeta struct {
	Project  string `json:"project"`
	Team     string `json:"team,omitempty"`
	Language string `json:"language,omitempty"`
	Task     string `json:"task,omitempty"`
}

// ExpectedFinding is one finding the fixture expects.
type ExpectedFinding struct {
	DocID     string `json:"doc_id,omitempty"`
	DocPath   string `json:"doc_path,omitempty"`
	CodePath  string `json:"code_path"`
	Line      int    `json:"line,omitempty"`
	LineStart int    `json:"line_start,omitempty"`
	LineEnd   int    `json:"line_end,omitempty"`
	Severity  string `json:"severity,omitempty"`
	Category  string `json:"category,omitempty"`
}

// Expected is expected.json.
type Expected struct {
	Findings []ExpectedFinding `json:"findings"`
}

// Case is one loaded fixture.
type Case struct {
	Name     string
	Dir      string
	Meta     CaseMeta
	Expected Expected
	Diff     *change.Diff
}

// Options configures an eval run.
type Options struct {
	CasesDir     string
	Runs         int
	Provider     provider.Provider
	Model        string
	Companion    string
	Timeout      time.Duration
	MaxDocs      int
	MaxDocBytes  int
	MaxDiffBytes int
	MaxFindings  int
	MaxTokens    int
	Temperature  float64
	JSONMode     bool
}

// Metrics are the per-run and per-case scores.
type Metrics struct {
	TP                int     `json:"tp"`
	FP                int     `json:"fp"`
	FN                int     `json:"fn"`
	Verified          int     `json:"verified"`
	EvidenceDropped   int     `json:"evidence_dropped"`
	Precision         float64 `json:"precision"`
	Recall            float64 `json:"recall"`
	CitationValidity  float64 `json:"citation_validity"`
	HallucinationRate float64 `json:"hallucination_rate"`
	Stable            bool    `json:"stable"`
	InputTokens       int     `json:"input_tokens"`
	OutputTokens      int     `json:"output_tokens"`
}

// RunResult is one model run of one case.
type RunResult struct {
	Findings         []review.Finding `json:"findings,omitempty"`
	Dropped          []review.Dropped `json:"dropped,omitempty"`
	Usage            review.Usage     `json:"usage"`
	TP               int              `json:"tp"`
	FP               int              `json:"fp"`
	FN               int              `json:"fn"`
	Precision        float64          `json:"precision"`
	Recall           float64          `json:"recall"`
	CitationValidity float64          `json:"citation_validity"`
	Error            string           `json:"error,omitempty"`
}

// CaseResult is all runs of one case.
type CaseResult struct {
	Name string `json:"name"`
	Runs int    `json:"runs"`
	Metrics
	UniqueOutcomes int         `json:"unique_outcomes"`
	Results        []RunResult `json:"results"`
	Error          string      `json:"error,omitempty"`
	prompt         review.PromptInfo
}

// Report is the eval result.
type Report struct {
	Schema    string            `json:"schema"`
	Model     string            `json:"model"`
	Prompt    review.PromptInfo `json:"prompt"`
	Runs      int               `json:"runs"`
	Cases     []CaseResult      `json:"cases"`
	Aggregate Metrics           `json:"aggregate"`
	Baseline  *Comparison       `json:"baseline,omitempty"`
	Warnings  []string          `json:"warnings,omitempty"`
	Errors    []string          `json:"errors,omitempty"`
}

// Comparison is the baseline comparison.
type Comparison struct {
	Path        string   `json:"path"`
	Model       string   `json:"model,omitempty"`
	Regressions []string `json:"regressions,omitempty"`
	Warnings    []string `json:"warnings,omitempty"`
}

// Baseline is the committed baseline document.
type Baseline struct {
	Schema    string                  `json:"schema"`
	Model     string                  `json:"model"`
	Prompt    review.PromptInfo       `json:"prompt"`
	Cases     map[string]BaselineCase `json:"cases"`
	Aggregate Metrics                 `json:"aggregate"`
}

// BaselineCase is one case in the baseline.
type BaselineCase struct {
	Metrics
	Runs int `json:"runs"`
}

// LoadCases reads every directory under root that has a case.json.
func LoadCases(root string) ([]Case, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read cases %s: %w", root, err)
	}
	var out []Case
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		if _, err := os.Stat(filepath.Join(dir, "case.json")); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("read case %s: %w", e.Name(), err)
		}
		c, err := loadCase(dir, e.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no eval cases found in %s", root)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func loadCase(dir, name string) (Case, error) {
	var meta CaseMeta
	if err := readJSON(filepath.Join(dir, "case.json"), &meta); err != nil {
		return Case{}, fmt.Errorf("case %s: %w", name, err)
	}
	if strings.TrimSpace(meta.Project) == "" {
		return Case{}, fmt.Errorf("case %s: case.json needs project", name)
	}
	if info, err := os.Stat(filepath.Join(dir, "docs")); err != nil || !info.IsDir() {
		return Case{}, fmt.Errorf("case %s: docs directory is missing", name)
	}
	var expected Expected
	if err := readJSON(filepath.Join(dir, "expected.json"), &expected); err != nil {
		return Case{}, fmt.Errorf("case %s: %w", name, err)
	}
	for i, e := range expected.Findings {
		if strings.TrimSpace(e.CodePath) == "" {
			return Case{}, fmt.Errorf("case %s: finding %d needs code_path", name, i+1)
		}
		if strings.TrimSpace(e.DocID) == "" && strings.TrimSpace(e.DocPath) == "" {
			return Case{}, fmt.Errorf("case %s: finding %d needs doc_id or doc_path", name, i+1)
		}
		if e.Line < 0 || e.LineStart < 0 || e.LineEnd < 0 {
			return Case{}, fmt.Errorf("case %s: finding %d has a negative line", name, i+1)
		}
		if e.LineStart > 0 && e.LineEnd > 0 && e.LineEnd < e.LineStart {
			return Case{}, fmt.Errorf("case %s: finding %d has line_end before line_start", name, i+1)
		}
		if e.Line > 0 && (e.LineStart > 0 || e.LineEnd > 0) {
			return Case{}, fmt.Errorf("case %s: finding %d mixes line and line_start/line_end", name, i+1)
		}
	}
	patch, err := os.ReadFile(filepath.Join(dir, "change.patch"))
	if err != nil {
		return Case{}, fmt.Errorf("case %s: read change.patch: %w", name, err)
	}
	diff, err := change.Parse(patch)
	if err != nil {
		return Case{}, fmt.Errorf("case %s: parse change.patch: %w", name, err)
	}
	if len(diff.Files) == 0 {
		return Case{}, fmt.Errorf("case %s: change.patch has no files", name)
	}
	return Case{Name: name, Dir: dir, Meta: meta, Expected: expected, Diff: diff}, nil
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("decode %s: %w", filepath.Base(path), err)
	}
	return nil
}

// Run executes every case opts.Runs times and scores the results.
func Run(ctx context.Context, opts Options) (*Report, error) {
	if opts.Provider == nil {
		return nil, errors.New("provider is required")
	}
	if strings.TrimSpace(opts.Model) == "" {
		return nil, errors.New("model is required")
	}
	if opts.Runs < 1 {
		return nil, errors.New("runs must be at least 1")
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 2 * time.Minute
	}
	if strings.TrimSpace(opts.Companion) == "" {
		opts.Companion = "companion"
	}
	cases, err := LoadCases(opts.CasesDir)
	if err != nil {
		return nil, err
	}

	rep := &Report{Schema: Schema, Model: opts.Model, Runs: opts.Runs}
	for i := range cases {
		cr := runCase(ctx, &cases[i], opts)
		rep.Cases = append(rep.Cases, cr)
		if cr.Error != "" {
			rep.Errors = append(rep.Errors, cr.Name+": "+cr.Error)
		}
		for _, rr := range cr.Results {
			if rr.Error != "" {
				rep.Errors = append(rep.Errors, cr.Name+": run: "+rr.Error)
			}
		}
		if rep.Prompt.Hash == "" && cr.prompt.Hash != "" {
			rep.Prompt = cr.prompt
		}
	}
	rep.Aggregate = aggregateCases(rep.Cases)
	return rep, nil
}

func runCase(ctx context.Context, c *Case, opts Options) CaseResult {
	cr := CaseResult{Name: c.Name, Runs: opts.Runs}

	docsCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	items, err := loadDocs(docsCtx, c, opts)
	cancel()
	if err != nil {
		cr.Error = err.Error()
		return cr
	}

	var keys []string
	for run := 0; run < opts.Runs; run++ {
		runCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
		r, err := review.Run(runCtx, review.Config{
			Task:         taskFor(c),
			Project:      c.Meta.Project,
			Team:         c.Meta.Team,
			Languages:    languagesFor(c.Meta.Language),
			Diff:         c.Diff,
			Documents:    items,
			MaxDocs:      opts.MaxDocs,
			MaxDocBytes:  opts.MaxDocBytes,
			MaxDiffBytes: opts.MaxDiffBytes,
			MaxFindings:  opts.MaxFindings,
			Provider:     opts.Provider,
			Model:        opts.Model,
			Temperature:  opts.Temperature,
			MaxTokens:    opts.MaxTokens,
			JSONMode:     opts.JSONMode,
		})
		cancel()
		if err != nil {
			cr.Results = append(cr.Results, RunResult{Error: err.Error()})
			continue
		}
		if cr.prompt.Hash == "" {
			cr.prompt = r.Prompt
		}
		cr.Results = append(cr.Results, scoreRun(c.Expected.Findings, r))
		keys = append(keys, findingSetKey(r.Findings))
	}

	cr.Runs = len(cr.Results)
	cr.Metrics = aggregateRuns(cr.Results)
	cr.Stable, cr.UniqueOutcomes = stability(keys)
	return cr
}

func loadDocs(ctx context.Context, c *Case, opts Options) ([]docs.Doc, error) {
	src := &docs.CompanionSource{
		Bin:          opts.Companion,
		Store:        filepath.Join(c.Dir, "docs"),
		Project:      c.Meta.Project,
		Team:         c.Meta.Team,
		Language:     c.Meta.Language,
		MaxBodyBytes: opts.MaxDocBytes,
	}
	items, err := src.Guidance(ctx, taskFor(c), opts.MaxDocs)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("no applicable documents for task %q", taskFor(c))
	}
	return items, nil
}

func taskFor(c *Case) string {
	if strings.TrimSpace(c.Meta.Task) != "" {
		return c.Meta.Task
	}
	return "review the change"
}

func languagesFor(lang string) []string {
	if strings.TrimSpace(lang) == "" {
		return nil
	}
	return []string{lang}
}

func scoreRun(expected []ExpectedFinding, r *review.Report) RunResult {
	tp, fp, fn := Match(expected, r.Findings)
	evidence := evidenceDropped(r.Dropped)
	cit, _ := citation(len(r.Findings), evidence)
	return RunResult{
		Findings:         r.Findings,
		Dropped:          r.Dropped,
		Usage:            r.Usage,
		TP:               tp,
		FP:               fp,
		FN:               fn,
		Precision:        precision(tp, fp),
		Recall:           recall(tp, fn),
		CitationValidity: cit,
	}
}

func aggregateRuns(results []RunResult) Metrics {
	var tp, fp, fn, verified, evidence, in, out int
	for _, r := range results {
		if r.Error != "" {
			continue
		}
		tp += r.TP
		fp += r.FP
		fn += r.FN
		verified += len(r.Findings)
		evidence += evidenceDropped(r.Dropped)
		in += r.Usage.InputTokens
		out += r.Usage.OutputTokens
	}
	return metricsFrom(tp, fp, fn, verified, evidence, in, out)
}

func aggregateCases(cases []CaseResult) Metrics {
	var tp, fp, fn, verified, evidence, in, out int
	stable := len(cases) > 0
	for _, c := range cases {
		tp += c.TP
		fp += c.FP
		fn += c.FN
		verified += c.Verified
		evidence += c.EvidenceDropped
		in += c.InputTokens
		out += c.OutputTokens
		if c.Error != "" || !c.Stable {
			stable = false
		}
	}
	m := metricsFrom(tp, fp, fn, verified, evidence, in, out)
	m.Stable = stable
	return m
}

func metricsFrom(tp, fp, fn, verified, evidence, in, out int) Metrics {
	cit, hall := citation(verified, evidence)
	return Metrics{
		TP:                tp,
		FP:                fp,
		FN:                fn,
		Verified:          verified,
		EvidenceDropped:   evidence,
		Precision:         precision(tp, fp),
		Recall:            recall(tp, fn),
		CitationValidity:  cit,
		HallucinationRate: hall,
		InputTokens:       in,
		OutputTokens:      out,
	}
}

func precision(tp, fp int) float64 {
	if tp+fp == 0 {
		return 1
	}
	return float64(tp) / float64(tp+fp)
}

func recall(tp, fn int) float64 {
	if tp+fn == 0 {
		return 1
	}
	return float64(tp) / float64(tp+fn)
}

func citation(verified, evidence int) (validity, hallucination float64) {
	total := verified + evidence
	if total == 0 {
		return 1, 0
	}
	hallucination = float64(evidence) / float64(total)
	return 1 - hallucination, hallucination
}

func stability(keys []string) (bool, int) {
	if len(keys) == 0 {
		return false, 0
	}
	unique := map[string]bool{}
	for _, k := range keys {
		unique[k] = true
	}
	return len(unique) == 1, len(unique)
}

func findingSetKey(findings []review.Finding) string {
	keys := make([]string, 0, len(findings))
	for _, f := range findings {
		keys = append(keys, f.DocID+"\x00"+change.NormalizePath(f.DocPath)+"\x00"+change.NormalizePath(f.CodePath)+"\x00"+strconv.Itoa(f.Line))
	}
	sort.Strings(keys)
	return strings.Join(keys, "\x01")
}

var evidenceReasons = map[string]bool{
	"doc_not_in_pack":       true,
	"doc_not_approved":      true,
	"doc_quote_not_found":   true,
	"code_path_not_changed": true,
	"code_quote_not_found":  true,
	"code_quote_too_short":  true,
}

func evidenceDropped(dropped []review.Dropped) int {
	n := 0
	for _, d := range dropped {
		if evidenceReasons[d.Reason] {
			n++
		}
	}
	return n
}

// Match compares expected findings with verified findings. Extra verified
// findings are false positives; unmatched expected findings are false
// negatives.
func Match(expected []ExpectedFinding, findings []review.Finding) (tp, fp, fn int) {
	used := make([]bool, len(findings))
	for _, e := range expected {
		for i, f := range findings {
			if used[i] || !matches(e, f) {
				continue
			}
			used[i] = true
			tp++
			break
		}
	}
	return tp, len(findings) - tp, len(expected) - tp
}

func matches(e ExpectedFinding, f review.Finding) bool {
	if e.DocID != "" && e.DocID != f.DocID {
		return false
	}
	if e.DocPath != "" && !samePath(e.DocPath, f.DocPath) {
		return false
	}
	if !samePath(e.CodePath, f.CodePath) {
		return false
	}
	start, end := lineRange(e)
	if start > 0 && (f.Line < start || f.Line > end) {
		return false
	}
	return true
}

func lineRange(e ExpectedFinding) (int, int) {
	if e.LineStart > 0 {
		end := e.LineEnd
		if end < e.LineStart {
			end = e.LineStart
		}
		return e.LineStart, end
	}
	if e.Line > 0 {
		return e.Line, e.Line
	}
	return 0, 0
}

func samePath(a, b string) bool {
	na, nb := change.NormalizePath(a), change.NormalizePath(b)
	if na == "" || nb == "" {
		return false
	}
	if na == nb {
		return true
	}
	for _, c := range change.PathCandidates(a) {
		if c == nb {
			return true
		}
	}
	for _, c := range change.PathCandidates(b) {
		if c == na {
			return true
		}
	}
	return false
}

// BuildBaseline turns a successful report into a baseline document.
func BuildBaseline(rep *Report) *Baseline {
	b := &Baseline{
		Schema:    BaselineSchema,
		Model:     rep.Model,
		Prompt:    rep.Prompt,
		Aggregate: rep.Aggregate,
		Cases:     make(map[string]BaselineCase, len(rep.Cases)),
	}
	for _, c := range rep.Cases {
		b.Cases[c.Name] = BaselineCase{Metrics: c.Metrics, Runs: c.Runs}
	}
	return b
}

// LoadBaseline reads a baseline. A missing file returns nil, nil.
func LoadBaseline(path string) (*Baseline, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read baseline: %w", err)
	}
	var out Baseline
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("decode baseline: %w", err)
	}
	return &out, nil
}

// Compare reports metric regressions against a baseline. Precision and
// citation validity are gated; recall and stability losses are warnings,
// matching the design's requirement to fail on precision or citation validity.
func Compare(rep *Report, base *Baseline, path string) *Comparison {
	if base == nil {
		return nil
	}
	c := &Comparison{Path: path, Model: base.Model}
	if base.Schema != "" && base.Schema != BaselineSchema {
		c.Warnings = append(c.Warnings, fmt.Sprintf("baseline schema %q is not %q", base.Schema, BaselineSchema))
	}
	if base.Model != "" && base.Model != rep.Model {
		c.Warnings = append(c.Warnings, fmt.Sprintf("baseline model %q, current %q", base.Model, rep.Model))
	}
	if base.Prompt.Hash != "" && rep.Prompt.Hash != "" && base.Prompt.Hash != rep.Prompt.Hash {
		c.Warnings = append(c.Warnings, fmt.Sprintf("baseline prompt hash %s, current %s", shortHash(base.Prompt.Hash), shortHash(rep.Prompt.Hash)))
	}
	if base.Prompt.Version != "" && rep.Prompt.Version != "" && base.Prompt.Version != rep.Prompt.Version {
		c.Warnings = append(c.Warnings, fmt.Sprintf("baseline prompt version %s, current %s", base.Prompt.Version, rep.Prompt.Version))
	}

	current := make(map[string]CaseResult, len(rep.Cases))
	for _, cr := range rep.Cases {
		current[cr.Name] = cr
	}
	names := make([]string, 0, len(base.Cases))
	for name := range base.Cases {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		bc := base.Cases[name]
		cr, ok := current[name]
		if !ok {
			c.Regressions = append(c.Regressions, fmt.Sprintf("case %s is missing", name))
			continue
		}
		if cr.Precision+1e-9 < bc.Precision {
			c.Regressions = append(c.Regressions, fmt.Sprintf("case %s precision %.3f < baseline %.3f", name, cr.Precision, bc.Precision))
		}
		if cr.CitationValidity+1e-9 < bc.CitationValidity {
			c.Regressions = append(c.Regressions, fmt.Sprintf("case %s citation validity %.3f < baseline %.3f", name, cr.CitationValidity, bc.CitationValidity))
		}
		if cr.Recall+1e-9 < bc.Recall {
			c.Warnings = append(c.Warnings, fmt.Sprintf("case %s recall %.3f < baseline %.3f", name, cr.Recall, bc.Recall))
		}
		if bc.Stable && !cr.Stable {
			c.Warnings = append(c.Warnings, fmt.Sprintf("case %s is no longer stable", name))
		}
	}
	for _, cr := range rep.Cases {
		if _, ok := base.Cases[cr.Name]; !ok {
			c.Warnings = append(c.Warnings, fmt.Sprintf("case %s is not in the baseline", cr.Name))
		}
	}

	if rep.Aggregate.Precision+1e-9 < base.Aggregate.Precision {
		c.Regressions = append(c.Regressions, fmt.Sprintf("aggregate precision %.3f < baseline %.3f", rep.Aggregate.Precision, base.Aggregate.Precision))
	}
	if rep.Aggregate.CitationValidity+1e-9 < base.Aggregate.CitationValidity {
		c.Regressions = append(c.Regressions, fmt.Sprintf("aggregate citation validity %.3f < baseline %.3f", rep.Aggregate.CitationValidity, base.Aggregate.CitationValidity))
	}
	if rep.Aggregate.Recall+1e-9 < base.Aggregate.Recall {
		c.Warnings = append(c.Warnings, fmt.Sprintf("aggregate recall %.3f < baseline %.3f", rep.Aggregate.Recall, base.Aggregate.Recall))
	}
	if base.Aggregate.Stable && !rep.Aggregate.Stable {
		c.Warnings = append(c.Warnings, "aggregate stability was lost")
	}
	return c
}

func shortHash(h string) string {
	if len(h) > 19 {
		return h[:19]
	}
	return h
}

// Markdown renders the human-readable eval report.
func (r *Report) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Companion Judge eval — `%s`\n\n", r.Model)
	if r.Prompt.Hash != "" {
		fmt.Fprintf(&b, "Prompt `%s` v%s, hash `%s`.\n\n", r.Prompt.ID, r.Prompt.Version, shortHash(r.Prompt.Hash))
	}
	fmt.Fprintf(&b, "%d case(s), %d run(s) per case.\n\n", len(r.Cases), r.Runs)

	b.WriteString("| Case | TP | FP | FN | Precision | Recall | Citation | Stable | In | Out |\n")
	b.WriteString("|---|---:|---:|---:|---:|---:|---:|:---:|---:|---:|\n")
	for _, c := range r.Cases {
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %.3f | %.3f | %.3f | %s | %d | %d |\n",
			c.Name, c.TP, c.FP, c.FN, c.Precision, c.Recall, c.CitationValidity, yesNo(c.Stable), c.InputTokens, c.OutputTokens)
	}
	fmt.Fprintf(&b, "| **Aggregate** | **%d** | **%d** | **%d** | **%.3f** | **%.3f** | **%.3f** | **%s** | **%d** | **%d** |\n",
		r.Aggregate.TP, r.Aggregate.FP, r.Aggregate.FN, r.Aggregate.Precision, r.Aggregate.Recall,
		r.Aggregate.CitationValidity, yesNo(r.Aggregate.Stable), r.Aggregate.InputTokens, r.Aggregate.OutputTokens)

	if r.Baseline == nil {
		b.WriteString("\nNo baseline compared.\n")
	} else {
		if len(r.Baseline.Regressions) > 0 {
			b.WriteString("\n**Baseline regression**\n\n")
			for _, reg := range r.Baseline.Regressions {
				fmt.Fprintf(&b, "- %s\n", reg)
			}
		} else {
			fmt.Fprintf(&b, "\nBaseline passed (`%s`).\n", r.Baseline.Path)
		}
		for _, w := range r.Baseline.Warnings {
			fmt.Fprintf(&b, "- %s\n", w)
		}
	}
	if len(r.Errors) > 0 {
		b.WriteString("\n**Errors**\n\n")
		for _, e := range r.Errors {
			fmt.Fprintf(&b, "- %s\n", e)
		}
	}
	if len(r.Warnings) > 0 {
		b.WriteString("\nNotes:\n")
		for _, w := range r.Warnings {
			fmt.Fprintf(&b, "- %s\n", w)
		}
	}
	return b.String()
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}
