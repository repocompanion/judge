# Security policy

## Supported versions

This is pre-1.0 software. Security fixes land on `main`; there are no
maintained release branches yet.

## Reporting a vulnerability

Use GitHub's private vulnerability reporting:

https://github.com/repocompanion/judge/security/advisories/new

Do not open a public issue for a suspected vulnerability. Do not include real
provider API keys, customer code, or document-store contents in a report;
redact them or use synthetic examples. We will coordinate disclosure after a
fix is available. Response times are best effort.

## Scope

In scope:

- credential exposure in the CLI, prompts, reports, logs, or CI;
- prompt-injection or unsafe-input handling in the diff and document pack;
- writes, approvals, merges, or network calls outside the documented model
  endpoint and the read-only `companion guidance --store` call;
- evidence-verifier bypasses that let a finding survive without a matching
  document quote and added-code quote.

Out of scope:

- the quality or correctness of a model provider;
- findings that are wrong but still pass evidence verification;
- vulnerabilities in `companion`, the docs store, or third-party endpoints.

The judge is advisory and read-only; it never approves, merges, or writes back.
