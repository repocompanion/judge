package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/repocompanion/judge/internal/eval"
)

const evalUsage = `Usage: judge eval --model MODEL [flags]

Runs every case in --cases. Each case has:
  case.json       project, team, language and retrieval task
  change.patch    unified diff under review
  docs/           pinned docs store for the case
  expected.json   expected findings (empty for a benign case)

Metrics: precision, recall, citation validity, hallucination rate, stability
across runs, and token usage. The result is compared with --baseline.

Exit codes: 0 pass, 2 usage/input, 3 eval could not complete, 4 baseline
regression. --emit-baseline prints a baseline document; redirect it to the
baseline path instead of writing it from the binary.

Flags:
`

type evalOptions struct {
	cases        string
	runs         int
	baseline     string
	noBaseline   bool
	emitBaseline bool
	companion    string
	baseURL      string
	model        string
	apiKeyEnv    string
	maxDocs      int
	maxDocBytes  int
	maxDiffBytes int
	maxFindings  int
	maxTokens    int
	temperature  float64
	timeout      time.Duration
	jsonMode     bool
	jsonOut      bool
}

func runEval(args []string, stdout, stderr io.Writer) int {
	var o evalOptions
	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), evalUsage)
		fs.PrintDefaults()
	}

	fs.StringVar(&o.cases, "cases", "testdata/reviews", "directory of eval cases")
	fs.IntVar(&o.runs, "runs", 3, "runs per case")
	fs.StringVar(&o.baseline, "baseline", "testdata/evals/baseline.json", "baseline file")
	fs.BoolVar(&o.noBaseline, "no-baseline", false, "skip baseline comparison")
	fs.BoolVar(&o.emitBaseline, "emit-baseline", false, "print a baseline JSON document and exit")
	fs.StringVar(&o.companion, "companion", "companion", "path to the companion binary")
	fs.StringVar(&o.baseURL, "base-url", "", "OpenAI-compatible base URL (env JUDGE_BASE_URL)")
	fs.StringVar(&o.model, "model", "", "model id (env JUDGE_MODEL)")
	fs.StringVar(&o.apiKeyEnv, "api-key-env", "JUDGE_API_KEY", "env var holding the API key")
	fs.IntVar(&o.maxDocs, "max-docs", 8, "maximum documents per case")
	fs.IntVar(&o.maxDocBytes, "max-doc-bytes", 12000, "maximum bytes per document")
	fs.IntVar(&o.maxDiffBytes, "max-diff-bytes", 40000, "maximum diff bytes in the prompt")
	fs.IntVar(&o.maxFindings, "max-findings", 20, "maximum verified findings per run")
	fs.IntVar(&o.maxTokens, "max-tokens", 4000, "maximum completion tokens")
	fs.Float64Var(&o.temperature, "temperature", 0, "model temperature")
	fs.DurationVar(&o.timeout, "timeout", 2*time.Minute, "timeout per case or run")
	fs.BoolVar(&o.jsonMode, "json-mode", false, "send response_format=json_object")
	fs.BoolVar(&o.jsonOut, "json", false, "emit JSON")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "eval: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	if o.runs < 1 {
		fmt.Fprintln(stderr, "eval: --runs must be at least 1")
		return 2
	}
	if strings.TrimSpace(o.cases) == "" {
		fmt.Fprintln(stderr, "eval: --cases is required")
		return 2
	}
	if o.emitBaseline && o.jsonOut {
		fmt.Fprintln(stderr, "eval: --emit-baseline and --json cannot be combined")
		return 2
	}

	p, model, err := openProvider(o.baseURL, o.model, o.apiKeyEnv, o.timeout)
	if err != nil {
		fmt.Fprintf(stderr, "eval: %v\n", err)
		return 2
	}

	rep, err := eval.Run(context.Background(), eval.Options{
		CasesDir:     o.cases,
		Runs:         o.runs,
		Provider:     p,
		Model:        model,
		Companion:    o.companion,
		Timeout:      o.timeout,
		MaxDocs:      o.maxDocs,
		MaxDocBytes:  o.maxDocBytes,
		MaxDiffBytes: o.maxDiffBytes,
		MaxFindings:  o.maxFindings,
		MaxTokens:    o.maxTokens,
		Temperature:  o.temperature,
		JSONMode:     o.jsonMode,
	})
	if err != nil {
		fmt.Fprintf(stderr, "eval: %v\n", err)
		return 3
	}

	if o.emitBaseline {
		if len(rep.Errors) > 0 {
			printEvalErrors(stderr, rep.Errors)
			return 3
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(eval.BuildBaseline(rep)); err != nil {
			fmt.Fprintf(stderr, "eval: %v\n", err)
			return 3
		}
		return 0
	}

	if !o.noBaseline {
		base, err := eval.LoadBaseline(o.baseline)
		if err != nil {
			fmt.Fprintf(stderr, "eval: %v\n", err)
			return 2
		}
		if base == nil {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("no baseline at %s; reporting only", o.baseline))
		} else {
			rep.Baseline = eval.Compare(rep, base, o.baseline)
		}
	}

	if o.jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			fmt.Fprintf(stderr, "eval: %v\n", err)
			return 3
		}
	} else {
		fmt.Fprint(stdout, rep.Markdown())
	}

	if len(rep.Errors) > 0 {
		printEvalErrors(stderr, rep.Errors)
		return 3
	}
	if rep.Baseline != nil && len(rep.Baseline.Regressions) > 0 {
		return 4
	}
	return 0
}

func printEvalErrors(w io.Writer, errs []string) {
	for _, e := range errs {
		fmt.Fprintf(w, "eval: %s\n", e)
	}
}
