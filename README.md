# Companion Judge

Agentic reviewer for Repo Companion. It reviews a change against the
organization's approved documentation and returns findings that cite both the
document and the code they refer to.

**Status: early alpha.** The `review` command works against any
OpenAI-compatible chat-completions endpoint. The `eval` command scores fixtures
and gates a committed baseline. Anthropic support is planned. Findings are
advisory: nothing is approved, merged, or written back.

## What it does

1. Reads a pinned docs-store checkout through
   `companion guidance --store <dir> --json "<task>"`.
2. Builds a bounded context pack: applicable approved documents, the diff,
   project and team, and an explicit precedence note. Renames and quoted paths
   are normalized, CRLF is removed, and binary files are skipped.
3. Asks the model for findings that cite an exact document quote and an exact
   added-code quote.
4. Verifies every quote against the documents and the diff, drops findings that
   do not verify, and reports what was dropped.
5. Prints a Markdown review or a JSON report (`--json`).

No document, no finding. The model is not allowed to use general best
practices, and it never gets write access.

## Requirements

- Go 1.27.3+ to build.
- A `companion` build that supports `companion guidance --store` (the read-only
  store override, added in PR #14; the 0.2.0 Homebrew release predates it).
- A docs store checkout.
- An OpenAI-compatible endpoint and model.

## Usage

```bash
export JUDGE_API_KEY=...
export JUDGE_BASE_URL=https://api.openai.com/v1   # or a local/self-hosted endpoint
export JUDGE_MODEL=your-model-id

# Inspect the request first. No model call, no API key needed.
companion-judge review \
  --store ../docs-store \
  --project acme/api \
  --base origin/main \
  --title "add checkout analytics" \
  --dry-run

# Run the review.
companion-judge review \
  --store ../docs-store \
  --project acme/api \
  --base origin/main \
  --title "add checkout analytics"
```

The diff can also come from a file or the index:

```bash
companion-judge review --store ../docs-store --project acme/api --diff pr.diff
companion-judge review --store ../docs-store --project acme/api --staged
```

Output:

```bash
companion-judge review ... --json    # companion.judge/v1 report
```

Exit codes: `0` review completed (findings do not fail the command), `2` usage
or input error, `3` the review could not be completed (no documents, model
failure, invalid model output).

## Configuration

| Flag | Default | Purpose |
|---|---|---|
| `--store` | required | Checked-out docs store |
| `--project` | required | Project area, `owner/repo` |
| `--team` | | Team area for guidance |
| `--lang` | | Language hint for guidance |
| `--diff` | | Unified diff file instead of `git diff` |
| `--base` / `--head` | `origin/main` / `HEAD` | Git range |
| `--staged` | false | Review the staged diff |
| `--task` / `--title` | changed paths | Retrieval query |
| `--companion` | `companion` | Companion binary |
| `--provider` | `openai-compatible` | Only this provider is implemented |
| `--base-url` | `JUDGE_BASE_URL` | API root; `http` only for loopback |
| `--model` | `JUDGE_MODEL` | Model id |
| `--api-key-env` | `JUDGE_API_KEY` | Env var holding the key |
| `--max-tokens` | 4000 | Maximum completion tokens, including reasoning |
| `--max-diff-bytes` | 40000 | Maximum diff bytes sent to the model |
| `--json-mode` | false | Send `response_format: json_object` |
| `--dry-run` | false | Print the pack summary, no model call |

Reasoning models such as `deepseek-v4-pro` spend completion tokens on reasoning.
If the budget is exhausted before the JSON answer, the review fails with a
truncation error that reports the token counts; raise `--max-tokens` (12000 was
enough for the checked-in fixture).

## Diff handling

- Both `git diff` output and plain unified diffs (`---`/`+++` without
  `diff --git`) are accepted.
- Git's C-style quoted paths (non-ASCII, quotes, backslashes) and unquoted paths
  containing spaces are decoded; rename and copy metadata is honored.
- CRLF is normalized before parsing.
- Hunk line counts decide where a hunk ends, so a removed line whose content
  starts with `--` is not mistaken for a `---` file header.
- Binary files are excluded from the prompt and listed in `change.skipped`. A
  review with only binary changes exits 0 without a model call.
- A diff larger than `--max-diff-bytes` is cut at a line boundary without
  splitting a UTF-8 rune, and the prompt says it was truncated.


## Report shape

`companion.judge/v1` includes the model, prompt version and hash, document
counts, changed files, verified findings, dropped findings with reasons, token
usage, and warnings. It contains no timestamps, so the same inputs produce the
same report structure. `findings` and `dropped` are always arrays, including
when empty. `change` records total `files`, `reviewed`, reviewed `paths`, and
`skipped` binary paths.

Only documents with `status: approved` are policy. Proposed, deprecated and
superseded documents are excluded from the pack and cannot back a finding.

## Evaluation

Fixtures live under `testdata/reviews/<case>/`:

```text
case.json       project, team, language and retrieval task
change.patch    the diff under review
docs/           the pinned docs store for this case
expected.json   expected findings; empty for a benign case
notes.md        why the case exists
```

`case.json`:

```json
{
  "project": "acme/api",
  "team": "payments",
  "language": "go",
  "task": "add SQS consumer"
}
```

`expected.json` findings match on `doc_id` or `doc_path`, `code_path`, and a
`line` or `line_start`/`line_end` range. Extra verified findings are false
positives; unmatched expected findings are false negatives. Severity and
category are informational and do not affect matching.

Run every case against the same endpoint used for reviews:

```bash
export JUDGE_API_KEY=...
companion-judge eval \
  --model deepseek-flash \
  --base-url https://api.deepseek.com/v1 \
  --runs 3
```

It uses the same `companion` build with `--store` support; pass `--companion`
if it is not on `PATH`. `--cases` defaults to `testdata/reviews` and
`--baseline` to `testdata/evals/baseline.json`.

The report includes per-case and aggregate precision, recall, citation
validity, hallucination rate, stability across runs, and token usage. TP/FP/FN
and token counts are summed across runs. Citation validity is verified findings
divided by verified plus evidence-dropped findings; the max-findings cap and a
missing message are not counted as hallucinations. The baseline at
`testdata/evals/baseline.json` gates precision and citation validity per case
and in aggregate; recall and stability losses are warnings. Use `--json` for
the machine-readable report and `--no-baseline` to report without gating.

`--emit-baseline` prints a baseline to stdout; the binary never writes it:

```bash
companion-judge eval ... --runs 3 --emit-baseline > testdata/evals/baseline.json
```

Exit codes: `0` pass, `2` usage or input error, `3` the eval could not complete,
`4` a baseline regression. The checked-in baseline was recorded with
`deepseek-flash` and prompt `review` v1; changing either is expected to require
a new baseline.

## Development

```bash
go test ./...
go vet ./...
make fmt vet test
```

The checked-in fixture under
[`testdata/reviews/sqs-body-logging`](testdata/reviews/sqs-body-logging) exercises
the guidance contract and evidence verification:

```bash
# Dry-run the fixture against a companion build with --store.
make smoke COMPANION=/path/to/companion

# Run the end-to-end test against the real companion binary.
COMPANION_BIN=/path/to/companion go test ./cmd/companion-judge
```

The other end-to-end tests use a loopback model stub and a companion stub, so
they need no endpoint or API key. That includes the eval scoring and baseline
gate; the checked-in baseline itself is refreshed against a real endpoint with
the eval command above.

## License

Apache-2.0. See [LICENSE](LICENSE).
