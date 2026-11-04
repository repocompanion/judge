# sqs-crlf

The patch file is stored with CRLF line endings, protected by `.gitattributes`
so checkout cannot normalize it away. The parser normalizes CRLF before
splitting lines; the finding must still land on line 12.

This case exists to keep CRLF handling covered end to end, not just in the
parser unit tests.
