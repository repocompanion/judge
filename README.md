# Companion Judge

Agentic reviewer for Repo Companion. It reviews a change against the
organization's approved documentation and returns findings that cite both the
document and the code they refer to.

**Status: early alpha.** The `review` command works against any
OpenAI-compatible chat-completions endpoint. Anthropic support is planned.
Findings are advisory: nothing is approved, merged, or written back.

## What it does

1. Reads a pinned docs-store checkout through
   `companion guidance --store <dir> --json "<task>"`.
2. Builds a bounded context pack: applicable approved documents, the diff,
   project and team, and an explicit precedence note.
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
| `--json-mode` | false | Send `response_format: json_object` |
| `--dry-run` | false | Print the pack summary, no model call |

## Report shape

`companion.judge/v1` includes the model, prompt version and hash, document
counts, changed files, verified findings, dropped findings with reasons, token
usage, and warnings. It contains no timestamps, so the same inputs produce the
same report structure. `findings` and `dropped` are always arrays, including
when empty.

Only documents with `status: approved` are policy. Proposed, deprecated and
superseded documents are excluded from the pack and cannot back a finding.

## Development

```bash
go test ./...
go vet ./...
make fmt vet test
```

The checked-in fixture under
[`testdata/reviews/sqs-idempotency`](testdata/reviews/sqs-idempotency) exercises
the guidance contract and evidence verification:

```bash
# Dry-run the fixture against a companion build with --store.
make smoke COMPANION=/path/to/companion

# Run the end-to-end test against the real companion binary.
COMPANION_BIN=/path/to/companion go test ./cmd/companion-judge
```

The other end-to-end tests use a loopback model stub and a companion stub, so
they need no endpoint or API key.

## License

Apache-2.0. See [LICENSE](LICENSE).
