package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"version"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if got, want := stdout.String(), "judge "+version+"\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "review") {
		t.Fatalf("help = %q", stdout.String())
	}
}

func TestRunUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"deploy"}, &stdout, &stderr); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "unknown command") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestReviewRequiresStore(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"review"}, &stdout, &stderr); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "--store") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestReviewRejectsUnknownProvider(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"review", "--store", t.TempDir(), "--project", "acme/api", "--provider", "anthropic"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "openai-compatible") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestReviewAllBinary(t *testing.T) {
	dir := t.TempDir()
	diff := filepath.Join(dir, "binary.diff")
	body := "diff --git a/img.png b/img.png\n" +
		"index 9532b4e..6c460dc 100644\n" +
		"Binary files a/img.png and b/img.png differ\n"
	if err := os.WriteFile(diff, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"review", "--store", dir, "--project", "acme/api", "--diff", diff}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "No reviewable changes") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}
