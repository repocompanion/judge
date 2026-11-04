---
id: sqs-consumer
kind: pattern
title: SQS consumer
status: approved
topics: [sqs, queue, consumer, messaging]
languages: [go]
owners: [payments]
last_reviewed: 2026-11-04
---
# SQS consumer

Use a dead-letter queue, idempotency keys and bounded concurrency.

## When to use

Use this pattern when a Go service consumes an SQS queue.

## Rules

- MUST configure a dead-letter queue.
- MUST make the handler idempotent.
- SHOULD bound worker concurrency.
- MUST NOT log raw message bodies.
