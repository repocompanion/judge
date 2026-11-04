// Package change parses unified diffs into files, hunks and added lines.
package change

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Line is one line inside a hunk. Kind is ' ', '+' or '-'.
type Line struct {
	Kind    byte
	Text    string
	OldLine int
	NewLine int
}

// Hunk is one unified-diff hunk.
type Hunk struct {
	OldStart, OldCount int
	NewStart, NewCount int
	Header             string
	Lines              []Line
}

// File is one changed file.
type File struct {
	OldPath string
	NewPath string
	Hunks   []Hunk
	Binary  bool
}

// Diff is a parsed unified diff.
type Diff struct {
	Raw   string
	Files []File
}

// AddedLine is a line introduced by the change.
type AddedLine struct {
	Path string
	Line int
	Text string
}

var hunkHeaderRE = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@(.*)$`)

// Parse parses a unified diff. It accepts the combined "git diff" format and
// plain unified diffs with ---/+++ headers.
func Parse(raw []byte) (*Diff, error) {
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	d := &Diff{Raw: text}
	lines := strings.Split(text, "\n")

	var cur *File
	var hunk *Hunk
	inHunk := false
	oldLine, newLine := 0, 0

	for i := 0; i < len(lines); i++ {
		ln := lines[i]
		if strings.HasPrefix(ln, "diff --git ") {
			oldPath, newPath := parseGitLine(ln)
			d.Files = append(d.Files, File{OldPath: oldPath, NewPath: newPath})
			cur = &d.Files[len(d.Files)-1]
			hunk, inHunk = nil, false
			continue
		}
		if cur == nil {
			continue
		}
		switch {
		case strings.HasPrefix(ln, "Binary files ") || strings.HasPrefix(ln, "GIT binary patch"):
			cur.Binary = true
			inHunk = false
		case strings.HasPrefix(ln, "--- "):
			cur.OldPath = markerPath(ln[4:])
			inHunk = false
		case strings.HasPrefix(ln, "+++ "):
			cur.NewPath = markerPath(ln[4:])
			inHunk = false
		case strings.HasPrefix(ln, "@@ "):
			h, err := parseHunk(ln)
			if err != nil {
				return nil, err
			}
			cur.Hunks = append(cur.Hunks, *h)
			hunk = &cur.Hunks[len(cur.Hunks)-1]
			inHunk = true
			oldLine, newLine = h.OldStart, h.NewStart
		case inHunk && hunk != nil:
			if ln == "" {
				inHunk = false
				continue
			}
			switch ln[0] {
			case ' ':
				hunk.Lines = append(hunk.Lines, Line{Kind: ' ', Text: ln[1:], OldLine: oldLine, NewLine: newLine})
				oldLine++
				newLine++
			case '+':
				hunk.Lines = append(hunk.Lines, Line{Kind: '+', Text: ln[1:], NewLine: newLine})
				newLine++
			case '-':
				hunk.Lines = append(hunk.Lines, Line{Kind: '-', Text: ln[1:], OldLine: oldLine})
				oldLine++
			case '\\':
				// No newline at end of file marker.
			default:
				inHunk = false
				i--
			}
		}
	}
	return d, nil
}

func parseGitLine(ln string) (string, string) {
	rest := strings.TrimPrefix(ln, "diff --git ")
	idx := strings.Index(rest, " b/")
	if idx < 0 {
		return "", ""
	}
	return unquoteGitPath(rest[:idx]), unquoteGitPath(rest[idx+3:])
}

func markerPath(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\t'); i >= 0 {
		s = s[:i]
	}
	if s == "/dev/null" {
		return ""
	}
	s = strings.TrimPrefix(s, "a/")
	s = strings.TrimPrefix(s, "b/")
	return unquoteGitPath(s)
}

func unquoteGitPath(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`) {
		if u, err := strconv.Unquote(s); err == nil {
			return u
		}
	}
	return s
}

func parseHunk(ln string) (*Hunk, error) {
	m := hunkHeaderRE.FindStringSubmatch(ln)
	if m == nil {
		return nil, fmt.Errorf("invalid hunk header %q", ln)
	}
	count := func(s string) int {
		if s == "" {
			return 1
		}
		n, _ := strconv.Atoi(s)
		return n
	}
	return &Hunk{
		OldStart: count(m[1]),
		OldCount: count(m[2]),
		NewStart: count(m[3]),
		NewCount: count(m[4]),
		Header:   strings.TrimSpace(m[5]),
	}, nil
}

// Paths returns changed paths in diff order, without duplicates.
func (d *Diff) Paths() []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range d.Files {
		p := f.NewPath
		if p == "" {
			p = f.OldPath
		}
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// AddedLines returns every introduced line with its new file position.
func (d *Diff) AddedLines() []AddedLine {
	var out []AddedLine
	for _, f := range d.Files {
		p := f.NewPath
		if p == "" {
			p = f.OldPath
		}
		for _, h := range f.Hunks {
			for _, l := range h.Lines {
				if l.Kind == '+' {
					out = append(out, AddedLine{Path: p, Line: l.NewLine, Text: l.Text})
				}
			}
		}
	}
	return out
}

// File returns the changed file for path.
func (d *Diff) File(path string) (File, bool) {
	path = NormalizePath(path)
	for _, f := range d.Files {
		if NormalizePath(f.NewPath) == path || NormalizePath(f.OldPath) == path {
			return f, true
		}
	}
	return File{}, false
}

// NormalizePath strips diff prefixes and leading separators.
func NormalizePath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimPrefix(p, "a/")
	p = strings.TrimPrefix(p, "b/")
	p = strings.TrimPrefix(p, "/")
	return p
}
