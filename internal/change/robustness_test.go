package change

import (
	"strings"
	"testing"
)

func parseOne(t *testing.T, diff string) (*Diff, File) {
	t.Helper()
	d, err := Parse([]byte(diff))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(d.Files) != 1 {
		t.Fatalf("files = %d, want 1", len(d.Files))
	}
	return d, d.Files[0]
}

func TestParseQuotedPaths(t *testing.T) {
	cases := []struct {
		name             string
		diff             string
		wantOld, wantNew string
	}{
		{
			name: "non-ascii",
			diff: `diff --git "a/caf\303\251.go" "b/caf\303\251.go"
index c89cd18..2ddffdc 100644
--- "a/caf\303\251.go"
+++ "b/caf\303\251.go"
@@ -1 +1,2 @@
 package p
+var Y = 1
`,
			wantOld: "café.go", wantNew: "café.go",
		},
		{
			name: "quoted spaces",
			diff: `diff --git "a/foo bar.go" "b/foo bar.go"
--- "a/foo bar.go"
+++ "b/foo bar.go"
@@ -1 +1,2 @@
 package p
+var Q = 1
`,
			wantOld: "foo bar.go", wantNew: "foo bar.go",
		},
		{
			name: "escaped quote",
			diff: `diff --git "a/quote\"name.go" "b/quote\"name.go"
--- "a/quote\"name.go"
+++ "b/quote\"name.go"
@@ -1 +1,2 @@
 package p
+var Z = 1
`,
			wantOld: `quote"name.go`, wantNew: `quote"name.go`,
		},
		{
			name: "unquoted spaces with b-slash directory",
			diff: "diff --git a/dir b/file.go b/dir b/file.go\n" +
				"--- a/dir b/file.go\t\n" +
				"+++ b/dir b/file.go\t\n" +
				"@@ -1,2 +1,3 @@\n" +
				" package p\n" +
				"+added\n" +
				" line\n",
			wantOld: "dir b/file.go", wantNew: "dir b/file.go",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, f := parseOne(t, tc.diff)
			if f.OldPath != tc.wantOld || f.NewPath != tc.wantNew {
				t.Fatalf("paths = %q -> %q, want %q -> %q", f.OldPath, f.NewPath, tc.wantOld, tc.wantNew)
			}
			if got := d.Paths(); len(got) != 1 || got[0] != tc.wantNew {
				t.Fatalf("Paths = %v, want [%s]", got, tc.wantNew)
			}
			added := d.AddedLines()
			if len(added) != 1 || added[0].Path != tc.wantNew {
				t.Fatalf("added = %+v", added)
			}
		})
	}
}

func TestParsePureRename(t *testing.T) {
	cases := []struct {
		name             string
		diff             string
		wantOld, wantNew string
	}{
		{
			name: "spaces",
			diff: `diff --git a/spaced name.go b/pure rename.go
similarity index 100%
rename from spaced name.go
rename to pure rename.go
`,
			wantOld: "spaced name.go", wantNew: "pure rename.go",
		},
		{
			name: "mixed quoting",
			diff: `diff --git a/spaced name.go "b/new caf\303\251.go"
similarity index 100%
rename from spaced name.go
rename to "new caf\303\251.go"
`,
			wantOld: "spaced name.go", wantNew: "new café.go",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, f := parseOne(t, tc.diff)
			if f.OldPath != tc.wantOld || f.NewPath != tc.wantNew {
				t.Fatalf("paths = %q -> %q, want %q -> %q", f.OldPath, f.NewPath, tc.wantOld, tc.wantNew)
			}
			if len(f.Hunks) != 0 {
				t.Fatalf("hunks = %d, want 0", len(f.Hunks))
			}
			if got := d.Paths(); len(got) != 1 || got[0] != tc.wantNew {
				t.Fatalf("Paths = %v, want [%s]", got, tc.wantNew)
			}
			if _, ok := d.File(tc.wantOld); !ok {
				t.Fatalf("File(%q) did not match the rename source", tc.wantOld)
			}
		})
	}
}

func TestParseBinaryPaths(t *testing.T) {
	cases := []struct {
		name             string
		diff             string
		wantOld, wantNew string
	}{
		{
			name: "quoted non-ascii",
			diff: `diff --git "a/bin caf\303\251.png" "b/bin caf\303\251.png"
index 9532b4e..6c460dc 100644
Binary files "a/bin caf\303\251.png" and "b/bin caf\303\251.png" differ
`,
			wantOld: "bin café.png", wantNew: "bin café.png",
		},
		{
			name: "unquoted spaces",
			diff: `diff --git a/bin name.png b/bin name.png
index 9532b4e..6c460dc 100644
Binary files a/bin name.png and b/bin name.png differ
`,
			wantOld: "bin name.png", wantNew: "bin name.png",
		},
		{
			name: "new binary file",
			diff: `diff --git a/new.bin b/new.bin
new file mode 100644
index 0000000..1111111
Binary files /dev/null and b/new.bin differ
`,
			wantOld: "", wantNew: "new.bin",
		},
		{
			name: "git binary patch",
			diff: `diff --git a/img.png b/img.png
new file mode 100644
index 0000000..1111111
GIT binary patch
literal 10
RcmZQzWJ=1+ODw8n000X)0*L?s
`,
			wantOld: "", wantNew: "img.png",
		},
		{
			name: "deleted binary file",
			diff: `diff --git a/old.bin b/old.bin
deleted file mode 100644
index 1111111..0000000
Binary files a/old.bin and /dev/null differ
`,
			wantOld: "old.bin", wantNew: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, f := parseOne(t, tc.diff)
			if f.OldPath != tc.wantOld || f.NewPath != tc.wantNew {
				t.Fatalf("paths = %q -> %q, want %q -> %q", f.OldPath, f.NewPath, tc.wantOld, tc.wantNew)
			}
			if !f.Binary {
				t.Fatal("file was not marked binary")
			}
			if got := d.AddedLines(); len(got) != 0 {
				t.Fatalf("added lines = %+v, want none", got)
			}
			if got := d.ReviewedPaths(); len(got) != 0 {
				t.Fatalf("ReviewedPaths = %v, want none", got)
			}
			wantSkipped := tc.wantNew
			if wantSkipped == "" {
				wantSkipped = tc.wantOld
			}
			if got := d.SkippedPaths(); len(got) != 1 || got[0] != wantSkipped {
				t.Fatalf("SkippedPaths = %v, want [%s]", got, wantSkipped)
			}
		})
	}
}

func TestParseModeOnlyPathWithBDirectory(t *testing.T) {
	_, f := parseOne(t, `diff --git a/dir b/file.go b/dir b/file.go
old mode 100644
new mode 100755
`)
	if f.OldPath != "dir b/file.go" || f.NewPath != "dir b/file.go" {
		t.Fatalf("paths = %q -> %q", f.OldPath, f.NewPath)
	}
}

func TestNormalizePathKeepsRealBDirectory(t *testing.T) {
	d, f := parseOne(t, `diff --git a/b/config.go b/b/config.go
--- a/b/config.go
+++ b/b/config.go
@@ -1 +1,2 @@
 package b
+var X = 1
`)
	if f.NewPath != "b/config.go" {
		t.Fatalf("NewPath = %q", f.NewPath)
	}
	if got := NormalizePath("b/config.go"); got != "b/config.go" {
		t.Fatalf("NormalizePath = %q, want the real b/ directory kept", got)
	}
	if _, ok := d.File("b/config.go"); !ok {
		t.Fatal("File did not match b/config.go")
	}
	if _, ok := d.File("a/b/config.go"); !ok {
		t.Fatal("File did not tolerate an a/ prefix")
	}
	if _, ok := d.File("config.go"); ok {
		t.Fatal("File stripped a real top-level b/ directory")
	}
}

func TestParseHunkLinesThatLookLikeHeaders(t *testing.T) {
	d, f := parseOne(t, `diff --git a/x.txt b/x.txt
--- a/x.txt
+++ b/x.txt
@@ -1,2 +1,2 @@
--- old
+++ new
 keep
`)
	if f.OldPath != "x.txt" || f.NewPath != "x.txt" {
		t.Fatalf("paths = %q -> %q, want x.txt", f.OldPath, f.NewPath)
	}
	if len(f.Hunks) != 1 || len(f.Hunks[0].Lines) != 3 {
		t.Fatalf("hunks = %+v", f.Hunks)
	}
	if l := f.Hunks[0].Lines[0]; l.Kind != '-' || l.Text != "-- old" {
		t.Fatalf("first line = %+v, want removed '-- old'", l)
	}
	if l := f.Hunks[0].Lines[1]; l.Kind != '+' || l.Text != "++ new" {
		t.Fatalf("second line = %+v, want added '++ new'", l)
	}
	if l := f.Hunks[0].Lines[2]; l.Kind != ' ' || l.Text != "keep" {
		t.Fatalf("third line = %+v, want context 'keep'", l)
	}
	if got := d.AddedLines(); len(got) != 1 || got[0].Text != "++ new" {
		t.Fatalf("added = %+v", got)
	}
}

func TestParsePlainUnifiedDiff(t *testing.T) {
	d, err := Parse([]byte(`--- a/one.txt
+++ b/one.txt
@@ -1 +1,2 @@
 one
+two
--- a/two.txt
+++ b/two.txt
@@ -1 +1,2 @@
 alpha
+beta
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Files) != 2 {
		t.Fatalf("files = %d, want 2: %+v", len(d.Files), d.Files)
	}
	if got := d.Paths(); len(got) != 2 || got[0] != "one.txt" || got[1] != "two.txt" {
		t.Fatalf("Paths = %v", got)
	}
	added := d.AddedLines()
	if len(added) != 2 || added[0].Text != "two" || added[1].Text != "beta" {
		t.Fatalf("added = %+v", added)
	}
}

func TestParseCRLF(t *testing.T) {
	d, f := parseOne(t, "diff --git a/x.go b/x.go\r\n"+
		"--- a/x.go\r\n"+
		"+++ b/x.go\r\n"+
		"@@ -1 +1,2 @@\r\n"+
		" package p\r\n"+
		"+var X = 1\r\n")
	if strings.Contains(d.Raw, "\r") {
		t.Fatal("Raw still contains carriage returns")
	}
	if f.NewPath != "x.go" {
		t.Fatalf("path = %q", f.NewPath)
	}
	added := d.AddedLines()
	if len(added) != 1 {
		t.Fatalf("added = %+v", added)
	}
	if added[0].Text != "var X = 1" || added[0].Line != 2 {
		t.Fatalf("added line = %+v", added[0])
	}
}

func TestReviewableTextSkipsBinary(t *testing.T) {
	d, err := Parse([]byte("From 123\nSubject: test\n\n" + `diff --git a/a.txt b/a.txt
--- a/a.txt
+++ b/a.txt
@@ -1 +1,2 @@
 one
+two
diff --git a/img.png b/img.png
index 9532b4e..6c460dc 100644
Binary files a/img.png and b/img.png differ
diff --git a/b.txt b/b.txt
--- a/b.txt
+++ b/b.txt
@@ -1 +1,2 @@
 alpha
+beta
`))
	if err != nil {
		t.Fatal(err)
	}
	text := d.ReviewableText()
	if strings.Contains(text, "img.png") {
		t.Fatalf("ReviewableText included the binary section:\n%s", text)
	}
	for _, want := range []string{"+two", "+beta"} {
		if !strings.Contains(text, want) {
			t.Fatalf("ReviewableText missing %q:\n%s", want, text)
		}
	}
	if got := d.ReviewedPaths(); len(got) != 2 || got[0] != "a.txt" || got[1] != "b.txt" {
		t.Fatalf("ReviewedPaths = %v", got)
	}
	if got := d.SkippedPaths(); len(got) != 1 || got[0] != "img.png" {
		t.Fatalf("SkippedPaths = %v", got)
	}
	if got := d.Paths(); len(got) != 3 {
		t.Fatalf("Paths = %v, want all three", got)
	}
}
