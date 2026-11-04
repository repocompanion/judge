# sqs-benign-log

A count-only log line is added. The approved SQS pattern forbids logging raw
message bodies, not message counts, so this change must produce no findings.

This case measures precision: any verified finding is a false positive.
