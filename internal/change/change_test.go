package change

import (
	"strings"
	"testing"
)

const sample = `diff --git a/internal/consumer/consumer.go b/internal/consumer/consumer.go
index 1111111..2222222 100644
--- a/internal/consumer/consumer.go
+++ b/internal/consumer/consumer.go
@@ -10,3 +10,6 @@ func Consume(body string) error {
 	msgs := receive()
+	for _, m := range msgs {
+		handle(m)
+	}
 	return nil
 }
`

func TestParseAddedLines(t *testing.T) {
	d, err := Parse([]byte(sample))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(d.Files) != 1 {
		t.Fatalf("files = %d, want 1", len(d.Files))
	}
	f := d.Files[0]
	if f.NewPath != "internal/consumer/consumer.go" {
		t.Fatalf("path = %q", f.NewPath)
	}
	if len(f.Hunks) != 1 {
		t.Fatalf("hunks = %d, want 1", len(f.Hunks))
	}
	if f.Hunks[0].NewStart != 10 || f.Hunks[0].NewCount != 6 {
		t.Fatalf("hunk = %+v", f.Hunks[0])
	}
	added := d.AddedLines()
	if len(added) != 3 {
		t.Fatalf("added = %d, want 3: %+v", len(added), added)
	}
	if added[1].Line != 12 || strings.TrimSpace(added[1].Text) != "handle(m)" {
		t.Fatalf("second added line = %+v", added[1])
	}
}

func TestPathsAndFile(t *testing.T) {
	d, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if got := d.Paths(); len(got) != 1 || got[0] != "internal/consumer/consumer.go" {
		t.Fatalf("paths = %v", got)
	}
	if _, ok := d.File("internal/consumer/consumer.go"); !ok {
		t.Fatal("File did not match")
	}
	if _, ok := d.File("missing.go"); ok {
		t.Fatal("File matched a missing path")
	}
}
