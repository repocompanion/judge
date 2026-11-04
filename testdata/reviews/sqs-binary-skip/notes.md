# sqs-binary-skip

The change touches a PNG and a Go file. The binary section must be removed
from the prompt and reported under `change.skipped`; only the text file is
reviewed.

The expected finding is the raw-body log in the Go file at line 12. A finding
against the binary cannot verify because the binary contributes no added line.
