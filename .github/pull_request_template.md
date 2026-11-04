## Summary

<!-- What changes, and why? -->

## Verification

- [ ] `make check`
- [ ] `go test -race ./...` when touching concurrency, provider, or eval code
- [ ] `judge eval --runs 3` when changing a prompt, model, fixture, or baseline
- [ ] `make smoke COMPANION=/path/to/companion` for review-path changes

## Checklist

- [ ] No credentials, provider keys, customer code, or private documents
- [ ] New diff and docs fixtures are synthetic or public
- [ ] Schema or prompt changes bump the version and refresh the baseline
- [ ] The change stays read-only and adds no write, approval, or merge paths
