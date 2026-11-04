# sqs-rename-spaces

The consumer file is renamed into a path containing a space, and the change
adds the forbidden raw-body log. The finding must cite the new path
(`internal/consumer streaming/consumer.go`) at line 12, not the old path.

The `rename from`/`rename to` metadata and the `---`/`+++` lines with trailing
tabs are the same shape git emits for a rename with spaces.
