# sqs-body-logging

The change logs the raw SQS message body. The approved SQS pattern forbids that
(`MUST NOT log raw message bodies.`), and the added `log.Printf("message body:
%s", m.Body)` line is the exact evidence.

The store also contains the DLQ and idempotency rules and a deprecated
`legacy-queue` document whose rule contradicts the approved pattern. This diff
does not exercise those rules; they are here to test precision and the
approved-only filter. The deprecated document must never be packed or cited.

`benign.patch` adds a count-only log line, which the same store must accept
without a finding.

`expected.json` records the intended finding for the eval harness.

Baseline on 2026-11-04: `deepseek-flash` returned this finding in 3/3 runs and
`deepseek-v4-pro` in 1/1 run (with `--max-tokens 12000`); every citation
verified.
