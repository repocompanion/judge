# sqs-idempotency

A consumer loop is added without an idempotency key. The approved SQS pattern
requires an idempotent handler, so the added `process(m)` line is a valid
finding.

The store also contains a deprecated `legacy-queue` document whose rule
contradicts the approved pattern. It must never be packed or cited as policy.

`expected.json` records the intended finding for the eval harness.
