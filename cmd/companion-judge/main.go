// Command companion-judge reviews a change against a Repo Companion docs store.
//
// It is advisory: findings are reported, never approved or merged.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/repocompanion/companion-judge/internal/change"
	"github.com/repocompanion/companion-judge/internal/docs"
	"github.com/repocompanion/companion-judge/internal/provider"
	"github.com/repocompanion/companion-judge/internal/review"
)

const version = "0.1.0-dev"

const usageText = `companion-judge reviews a change against a Repo Companion docs store.

Usage:
  companion-judge review --store DIR --project OWNER/REPO --model MODEL [flags]
  companion-judge eval --model MODEL [flags]
  companion-judge version
  companion-judge help

The review is advisory. Findings are reported with document and code evidence;
nothing is approved, merged, or written back to the store.`

const reviewUsage = `Usage: companion-judge review --store DIR --project OWNER/REPO [flags]

Reviews the diff between --base and --head (or --diff / --staged) against
documents returned by:
  companion guidance --store DIR --project OWNER/REPO --json "<task>"

Every finding must cite an exact document quote and an exact added-code quote.
Findings whose evidence does not verify are dropped.

Flags:
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usageText)
		return 2
	}
	switch args[0] {
	case "version", "--version", "-v":
		fmt.Fprintf(stdout, "companion-judge %s\n", version)
		return 0
	case "help", "--help", "-h":
		fmt.Fprintln(stdout, usageText)
		return 0
	case "review":
		return runReview(args[1:], stdout, stderr)
	case "eval":
		return runEval(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s\n", args[0], usageText)
		return 2
	}
}

type reviewOptions struct {
	store, project, team, lang string
	diffFile, base, head       string
	staged                     bool
	task, title                string
	companion                  string
	providerName               string
	baseURL, model, apiKeyEnv  string
	maxDocs, maxDocBytes       int
	maxDiffBytes, maxFindings  int
	maxTokens                  int
	temperature                float64
	timeout                    time.Duration
	jsonMode, dryRun, jsonOut  bool
}

func runReview(args []string, stdout, stderr io.Writer) int {
	var o reviewOptions
	fs := flag.NewFlagSet("review", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), reviewUsage)
		fs.PrintDefaults()
	}

	fs.StringVar(&o.store, "store", "", "checked-out docs store (required)")
	fs.StringVar(&o.project, "project", "", "project area, owner/repo (required)")
	fs.StringVar(&o.team, "team", "", "team area")
	fs.StringVar(&o.lang, "lang", "", "language hint passed to guidance")
	fs.StringVar(&o.diffFile, "diff", "", "read a unified diff file instead of running git")
	fs.StringVar(&o.base, "base", "", "base ref (default origin/main)")
	fs.StringVar(&o.head, "head", "", "head ref (default HEAD)")
	fs.BoolVar(&o.staged, "staged", false, "review the staged diff")
	fs.StringVar(&o.task, "task", "", "retrieval query (default: --title or changed paths)")
	fs.StringVar(&o.title, "title", "", "change title used as the retrieval query")
	fs.StringVar(&o.companion, "companion", "companion", "path to the companion binary")
	fs.StringVar(&o.providerName, "provider", "openai-compatible", "model provider")
	fs.StringVar(&o.baseURL, "base-url", "", "OpenAI-compatible base URL (env JUDGE_BASE_URL)")
	fs.StringVar(&o.model, "model", "", "model id (env JUDGE_MODEL)")
	fs.StringVar(&o.apiKeyEnv, "api-key-env", "JUDGE_API_KEY", "env var holding the API key")
	fs.IntVar(&o.maxDocs, "max-docs", 8, "maximum documents to include")
	fs.IntVar(&o.maxDocBytes, "max-doc-bytes", 12000, "maximum bytes per document")
	fs.IntVar(&o.maxDiffBytes, "max-diff-bytes", 40000, "maximum diff bytes in the prompt")
	fs.IntVar(&o.maxFindings, "max-findings", 20, "maximum verified findings")
	fs.IntVar(&o.maxTokens, "max-tokens", 4000, "maximum output tokens")
	fs.Float64Var(&o.temperature, "temperature", 0, "model temperature")
	fs.DurationVar(&o.timeout, "timeout", 2*time.Minute, "model and guidance timeout")
	fs.BoolVar(&o.jsonMode, "json-mode", false, "send response_format=json_object")
	fs.BoolVar(&o.dryRun, "dry-run", false, "build and print the request without calling the model")
	fs.BoolVar(&o.jsonOut, "json", false, "emit JSON")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "review: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	if o.store == "" {
		fmt.Fprintln(stderr, "review: --store is required")
		return 2
	}
	if o.project == "" {
		fmt.Fprintln(stderr, "review: --project is required")
		return 2
	}
	if o.providerName != "openai-compatible" {
		fmt.Fprintf(stderr, "review: unsupported provider %q (only openai-compatible is implemented)\n", o.providerName)
		return 2
	}

	d, err := loadDiff(o)
	if err != nil {
		fmt.Fprintf(stderr, "review: %v\n", err)
		return 2
	}
	if len(d.Files) == 0 {
		fmt.Fprintln(stdout, "No changes to review.")
		return 0
	}
	if len(d.ReviewedPaths()) == 0 {
		fmt.Fprintln(stdout, "No reviewable changes (all changed files are binary).")
		return 0
	}

	task := o.task
	if task == "" {
		task = o.title
	}
	if task == "" {
		task = defaultTask(d)
	}

	ctx, cancel := context.WithTimeout(context.Background(), o.timeout)
	defer cancel()

	src := &docs.CompanionSource{
		Bin:          o.companion,
		Store:        o.store,
		Project:      o.project,
		Team:         o.team,
		Language:     o.lang,
		MaxBodyBytes: o.maxDocBytes,
	}
	items, err := src.Guidance(ctx, task, o.maxDocs)
	if err != nil {
		fmt.Fprintf(stderr, "review: %v\n", err)
		return 3
	}
	if len(items) == 0 {
		fmt.Fprintf(stderr, "review: no applicable documents in %s for task %q\n", o.store, task)
		return 3
	}

	cfg := review.Config{
		Task:         task,
		Project:      o.project,
		Team:         o.team,
		Languages:    nonEmpty(o.lang),
		Diff:         d,
		Documents:    items,
		MaxDocs:      o.maxDocs,
		MaxDocBytes:  o.maxDocBytes,
		MaxDiffBytes: o.maxDiffBytes,
		MaxFindings:  o.maxFindings,
		Temperature:  o.temperature,
		MaxTokens:    o.maxTokens,
		JSONMode:     o.jsonMode,
	}

	if o.dryRun {
		return printDryRun(stdout, stderr, cfg, task, o.jsonOut)
	}

	p, model, err := openProvider(o.baseURL, o.model, o.apiKeyEnv, o.timeout)
	if err != nil {
		fmt.Fprintf(stderr, "review: %v\n", err)
		return 2
	}
	cfg.Provider = p
	cfg.Model = model

	report, err := review.Run(ctx, cfg)
	if err != nil {
		fmt.Fprintf(stderr, "review: %v\n", err)
		return 3
	}
	if o.jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			fmt.Fprintf(stderr, "review: %v\n", err)
			return 3
		}
		return 0
	}
	fmt.Fprint(stdout, report.Markdown())
	return 0
}

func loadDiff(o reviewOptions) (*change.Diff, error) {
	if o.diffFile != "" {
		b, err := os.ReadFile(o.diffFile)
		if err != nil {
			return nil, fmt.Errorf("read --diff: %w", err)
		}
		return change.Parse(b)
	}
	args := []string{"diff", "--no-color", "--unified=3"}
	if o.staged {
		args = append(args, "--cached")
	} else {
		base := o.base
		if base == "" {
			base = "origin/main"
		}
		head := o.head
		if head == "" {
			head = "HEAD"
		}
		args = append(args, base+"..."+head)
	}
	cmd := exec.Command("git", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return change.Parse(out)
}

func defaultTask(d *change.Diff) string {
	paths := d.ReviewedPaths()
	if len(paths) > 5 {
		paths = paths[:5]
	}
	if len(paths) == 0 {
		return "review the change"
	}
	return "review changes in " + strings.Join(paths, ", ")
}

func openProvider(baseURL, model, apiKeyEnv string, timeout time.Duration) (provider.Provider, string, error) {
	if baseURL == "" {
		baseURL = os.Getenv("JUDGE_BASE_URL")
	}
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if model == "" {
		model = os.Getenv("JUDGE_MODEL")
	}
	if strings.TrimSpace(model) == "" {
		return nil, "", fmt.Errorf("--model or JUDGE_MODEL is required")
	}
	key := apiKey(apiKeyEnv)
	if key == "" && !provider.IsLoopbackURL(baseURL) {
		return nil, "", fmt.Errorf("no API key in %s or OPENAI_API_KEY", apiKeyEnv)
	}
	p, err := provider.NewOpenAI(baseURL, key, timeout)
	if err != nil {
		return nil, "", err
	}
	return p, model, nil
}

func apiKey(envName string) string {
	if envName != "" {
		if v := strings.TrimSpace(os.Getenv(envName)); v != "" {
			return v
		}
	}
	return strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
}

func nonEmpty(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return []string{v}
}

func printDryRun(stdout, stderr io.Writer, cfg review.Config, task string, asJSON bool) int {
	pack := review.BuildPack(cfg)
	type docInfo struct {
		ID        string `json:"id"`
		Path      string `json:"path"`
		Scope     string `json:"scope"`
		Status    string `json:"status"`
		Bytes     int    `json:"bytes"`
		Truncated bool   `json:"truncated"`
	}
	type summary struct {
		Task                 string    `json:"task"`
		Project              string    `json:"project"`
		Files                []string  `json:"files"`
		Skipped              []string  `json:"skipped,omitempty"`
		Documents            []docInfo `json:"documents"`
		EstimatedInputTokens int       `json:"estimated_input_tokens"`
		Warnings             []string  `json:"warnings,omitempty"`
	}
	out := summary{
		Task:                 task,
		Project:              cfg.Project,
		Files:                cfg.Diff.ReviewedPaths(),
		Skipped:              cfg.Diff.SkippedPaths(),
		EstimatedInputTokens: pack.EstimateTokens(),
		Warnings:             pack.Warnings,
	}
	for _, d := range pack.Documents {
		out.Documents = append(out.Documents, docInfo{
			ID: d.ID, Path: d.Path, Scope: d.Scope, Status: d.Status,
			Bytes: len(d.Body), Truncated: d.Truncated,
		})
	}
	if asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			fmt.Fprintf(stderr, "review: %v\n", err)
			return 3
		}
		return 0
	}
	fmt.Fprintln(stdout, "Dry run: no model call.")
	fmt.Fprintf(stdout, "Task: %s\n", task)
	fmt.Fprintf(stdout, "Files: %d (%s)\n", len(out.Files), strings.Join(out.Files, ", "))
	if len(out.Skipped) > 0 {
		fmt.Fprintf(stdout, "Skipped binary: %d (%s)\n", len(out.Skipped), strings.Join(out.Skipped, ", "))
	}
	fmt.Fprintf(stdout, "Documents: %d, estimated input tokens: %d\n", len(out.Documents), out.EstimatedInputTokens)
	for _, d := range out.Documents {
		marker := ""
		if d.Truncated {
			marker = " (truncated)"
		}
		fmt.Fprintf(stdout, "- %s  %s  %d bytes%s\n", d.ID, d.Path, d.Bytes, marker)
	}
	for _, w := range out.Warnings {
		fmt.Fprintf(stdout, "warning: %s\n", w)
	}
	return 0
}
