# Companion Judge

Agentic reviewer for Repo Companion. It reviews a change against the
organization's approved documentation and returns findings that cite both the
document and the code they refer to.

**Status: pre-alpha.** This repository is a scaffold. The review pipeline is not
implemented yet; `companion-judge version` and `help` are the only commands.

## What it will do

- Read a pinned docs-store checkout through `companion guidance --store <dir>`
  and `companion context --store <dir>`.
- Review a base...head change with a language model.
- Require evidence: a finding is reported only if its document quote and code
  quote both verify against the retrieved documents and the diff.
- Stay read-only and advisory. It never writes, approves, or merges.

## Relationship to Repo Companion

- `companion` authors and reads the docs store.
- `companion-enforcer` (planned) is the deterministic gate.
- `companion-judge` reviews the prose principles that are not yet rules.

Findings are advisory by default. A recurring finding should become a
deterministic rule, so the rule set grows from real misses instead of guesses.

## Development

Requires Go 1.27.3 or newer.

```bash
go build ./cmd/companion-judge
go test ./...
make fmt vet test
```

## License

Apache-2.0. See [LICENSE](LICENSE).
