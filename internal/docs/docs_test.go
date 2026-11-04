package docs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeDoc(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadBodyContainment(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "org/patterns/sqs.md", "hello")
	if got, _, err := ReadBody(root, "org/patterns/sqs.md", 0); err != nil || got != "hello" {
		t.Fatalf("ReadBody = %q, %v", got, err)
	}
	for _, rel := range []string{"../escape.md", "/etc/passwd", "org/../../escape.md"} {
		if _, _, err := ReadBody(root, rel, 0); err == nil {
			t.Fatalf("ReadBody accepted %q", rel)
		}
	}
}

func TestReadBodyTruncates(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "doc.md", "0123456789")
	got, truncated, err := ReadBody(root, "doc.md", 4)
	if err != nil {
		t.Fatal(err)
	}
	if got != "0123" || !truncated {
		t.Fatalf("got %q truncated=%v", got, truncated)
	}
}

func TestGuidanceLoadsBodies(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "org/patterns/sqs.md", "MUST make the handler idempotent")
	src := &CompanionSource{
		Store: root, Project: "acme/api", MaxBodyBytes: 1000,
		Run: func(_ context.Context, args ...string) ([]byte, error) {
			joined := strings.Join(args, " ")
			for _, want := range []string{"guidance", "--store", "--project", "--json"} {
				if !strings.Contains(joined, want) {
					t.Fatalf("args %q missing %q", joined, want)
				}
			}
			return []byte(`[{"id":"sqs-consumer","title":"SQS consumer","path":"org/patterns/sqs.md","kind":"pattern","status":"approved","scope":"org","score":3.5}]`), nil
		},
	}
	got, err := src.Guidance(context.Background(), "implement an SQS consumer", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("docs = %d, want 1", len(got))
	}
	if got[0].ID != "sqs-consumer" || !strings.Contains(got[0].Body, "MUST make") {
		t.Fatalf("doc = %+v", got[0])
	}
}
