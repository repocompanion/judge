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
	oldRemaining, newRemaining := 0, 0

	for i := 0; i < len(lines); i++ {
		ln := lines[i]
		if strings.HasPrefix(ln, "diff --git ") {
			oldPath, newPath := parseGitLine(ln)
			d.Files = append(d.Files, File{OldPath: oldPath, NewPath: newPath})
			cur = &d.Files[len(d.Files)-1]
			hunk, inHunk = nil, false
			continue
		}

		// Inside a hunk every line starts with ' ', '+', '-' or '\'. Check this
		// before headers: a removed line whose content starts with "-- " is
		// literally "--- ..." in the diff, and an added "++ ..." is "+++ ...".
		if inHunk && hunk != nil {
			if strings.HasPrefix(ln, "@@ ") {
				h, err := parseHunk(ln)
				if err != nil {
					return nil, err
				}
				cur.Hunks = append(cur.Hunks, *h)
				hunk = &cur.Hunks[len(cur.Hunks)-1]
				oldLine, newLine = h.OldStart, h.NewStart
				oldRemaining, newRemaining = h.OldCount, h.NewCount
				inHunk = oldRemaining > 0 || newRemaining > 0
				continue
			}
			if ln != "" {
				switch ln[0] {
				case ' ':
					hunk.Lines = append(hunk.Lines, Line{Kind: ' ', Text: ln[1:], OldLine: oldLine, NewLine: newLine})
					oldLine++
					newLine++
					oldRemaining--
					newRemaining--
				case '+':
					hunk.Lines = append(hunk.Lines, Line{Kind: '+', Text: ln[1:], NewLine: newLine})
					newLine++
					newRemaining--
				case '-':
					hunk.Lines = append(hunk.Lines, Line{Kind: '-', Text: ln[1:], OldLine: oldLine})
					oldLine++
					oldRemaining--
				case '\\':
					// "\ No newline at end of file" does not consume a line.
				default:
					inHunk = false
					i--
					continue
				}
				if oldRemaining <= 0 && newRemaining <= 0 {
					inHunk = false
				}
				continue
			}
			inHunk = false
			continue
		}

		if cur == nil {
			// A plain unified diff starts with ---/+++ and has no diff --git.
			if strings.HasPrefix(ln, "--- ") {
				d.Files = append(d.Files, File{})
				cur = &d.Files[len(d.Files)-1]
				cur.OldPath = markerPath(ln[4:])
			}
			continue
		}

		switch {
		case strings.HasPrefix(ln, "Binary files "):
			cur.Binary = true
			// The ---/+++ or rename lines are better evidence for existing
			// files. Only parse this line when a side is still unknown, such
			// as for an added or deleted binary file.
			if cur.OldPath == "" || cur.NewPath == "" {
				if a, b, ok := parseBinaryLine(ln); ok {
					cur.OldPath, cur.NewPath = a, b
				}
			}
		case strings.HasPrefix(ln, "GIT binary patch"):
			cur.Binary = true
		case strings.HasPrefix(ln, "new file mode "):
			cur.OldPath = ""
		case strings.HasPrefix(ln, "deleted file mode "):
			cur.NewPath = ""
		case strings.HasPrefix(ln, "rename from "):
			cur.OldPath = unquoteGitPath(strings.TrimSpace(ln[len("rename from "):]))
		case strings.HasPrefix(ln, "rename to "):
			cur.NewPath = unquoteGitPath(strings.TrimSpace(ln[len("rename to "):]))
		case strings.HasPrefix(ln, "copy from "):
			cur.OldPath = unquoteGitPath(strings.TrimSpace(ln[len("copy from "):]))
		case strings.HasPrefix(ln, "copy to "):
			cur.NewPath = unquoteGitPath(strings.TrimSpace(ln[len("copy to "):]))
		case strings.HasPrefix(ln, "--- "):
			// In a plain diff this starts the next file. In a git diff the
			// header for the current file has no hunks yet.
			if len(cur.Hunks) > 0 {
				d.Files = append(d.Files, File{})
				cur = &d.Files[len(d.Files)-1]
			}
			cur.OldPath = markerPath(ln[4:])
		case strings.HasPrefix(ln, "+++ "):
			cur.NewPath = markerPath(ln[4:])
		case strings.HasPrefix(ln, "@@ "):
			h, err := parseHunk(ln)
			if err != nil {
				return nil, err
			}
			cur.Hunks = append(cur.Hunks, *h)
			hunk = &cur.Hunks[len(cur.Hunks)-1]
			oldLine, newLine = h.OldStart, h.NewStart
			oldRemaining, newRemaining = h.OldCount, h.NewCount
			inHunk = oldRemaining > 0 || newRemaining > 0
		}
	}
	return d, nil
}

// parseGitLine parses "diff --git <old> <new>". Git quotes only paths that
// need C-style escaping (non-ASCII, quotes, backslashes, control characters),
// not spaces, so an unquoted header is ambiguous when a path contains " b/".
// Authoritative ---/+++/rename lines override this best-effort result.
func parseGitLine(ln string) (string, string) {
	rest := strings.TrimPrefix(ln, "diff --git ")
	if a, b, ok := parseQuotedPair(rest); ok {
		return cleanDiffPath(a), cleanDiffPath(b)
	}

	var fallbackOld, fallbackNew string
	for i := 0; ; {
		idx := indexOutsideQuotes(rest[i:], " b/")
		if idx < 0 {
			break
		}
		idx += i
		old := cleanDiffPath(rest[:idx])
		// The " b/" separator already consumes the new path's prefix.
		newPath := unquoteGitPath(strings.TrimSpace(rest[idx+3:]))
		if old != "" && old == newPath {
			// Mode-only changes have the same path on both sides.
			return old, newPath
		}
		if fallbackOld == "" && fallbackNew == "" {
			fallbackOld, fallbackNew = old, newPath
		}
		i = idx + 3
	}
	if fallbackOld != "" || fallbackNew != "" {
		return fallbackOld, fallbackNew
	}
	return "", ""
}

// parseQuotedPair handles a header where both paths are quoted.
func parseQuotedPair(s string) (string, string, bool) {
	s = strings.TrimSpace(s)
	a, rest, ok := scanQuoted(s)
	if !ok {
		return "", "", false
	}
	rest = strings.TrimLeft(rest, " ")
	b, rest, ok := scanQuoted(rest)
	if !ok || strings.TrimSpace(rest) != "" {
		return "", "", false
	}
	return a, b, true
}

func scanQuoted(s string) (string, string, bool) {
	if len(s) == 0 || s[0] != '"' {
		return "", s, false
	}
	for i := 1; i < len(s); i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		if s[i] == '"' {
			return s[:i+1], s[i+1:], true
		}
	}
	return "", s, false
}

// indexOutsideQuotes returns the index of sep in s, skipping quoted spans.
func indexOutsideQuotes(s, sep string) int {
	inQuote := false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			if inQuote {
				i++
			}
		case '"':
			inQuote = !inQuote
		}
		if !inQuote && strings.HasPrefix(s[i:], sep) {
			return i
		}
	}
	return -1
}

// parseBinaryLine parses:
//
//	Binary files <old> and <new> differ
func parseBinaryLine(ln string) (string, string, bool) {
	rest := strings.TrimPrefix(ln, "Binary files ")
	rest = strings.TrimSuffix(rest, " differ")
	idx := indexOutsideQuotes(rest, " and ")
	if idx < 0 {
		return "", "", false
	}
	return cleanDiffPath(rest[:idx]), cleanDiffPath(rest[idx+len(" and "):]), true
}

// markerPath parses a ---/+++ path, dropping a trailing timestamp.
func markerPath(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\t'); i >= 0 {
		s = s[:i]
	}
	return cleanDiffPath(s)
}

// cleanDiffPath unquotes a diff path and strips one a/ or b/ diff prefix.
func cleanDiffPath(s string) string {
	s = unquoteGitPath(strings.TrimSpace(s))
	if s == "" || s == "/dev/null" {
		return ""
	}
	switch {
	case strings.HasPrefix(s, "a/"):
		return s[2:]
	case strings.HasPrefix(s, "b/"):
		return s[2:]
	}
	return s
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

// Paths returns every changed path in diff order, without duplicates.
func (d *Diff) Paths() []string {
	return d.paths(func(File) bool { return true })
}

// ReviewedPaths returns the changed paths a review can act on: binaries are
// excluded. Pure renames are included.
func (d *Diff) ReviewedPaths() []string {
	return d.paths(func(f File) bool { return !f.Binary })
}

// SkippedPaths returns changed paths excluded from review.
func (d *Diff) SkippedPaths() []string {
	return d.paths(func(f File) bool { return f.Binary })
}

func (d *Diff) paths(include func(File) bool) []string {
	seen := map[string]bool{}
	out := make([]string, 0)
	for _, f := range d.Files {
		if !include(f) {
			continue
		}
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

// ReviewableText returns the raw diff with binary file sections removed.
func (d *Diff) ReviewableText() string {
	if d == nil {
		return ""
	}
	if len(d.Files) == 0 {
		return d.Raw
	}
	sections := splitDiffSections(d.Raw)
	if len(sections) != len(d.Files) {
		return d.Raw
	}
	var b strings.Builder
	for i, section := range sections {
		if d.Files[i].Binary {
			continue
		}
		b.WriteString(section)
	}
	return b.String()
}

func splitDiffSections(raw string) []string {
	var sections []string
	var b strings.Builder
	headers := 0
	for _, ln := range strings.SplitAfter(raw, "\n") {
		if strings.HasPrefix(ln, "diff --git ") {
			if headers > 0 {
				sections = append(sections, b.String())
				b.Reset()
			}
			headers++
		}
		b.WriteString(ln)
	}
	if b.Len() > 0 {
		sections = append(sections, b.String())
	}
	return sections
}

// AddedLines returns every introduced line with its new file position.
func (d *Diff) AddedLines() []AddedLine {
	var out []AddedLine
	for _, f := range d.Files {
		if f.Binary {
			continue
		}
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

// File returns the changed file for path. A leading a/ or b/ is tolerated.
func (d *Diff) File(path string) (File, bool) {
	for _, cand := range PathCandidates(path) {
		for _, f := range d.Files {
			if NormalizePath(f.NewPath) == cand || NormalizePath(f.OldPath) == cand {
				return f, true
			}
		}
	}
	return File{}, false
}

// NormalizePath cleans a path without assuming it carries a diff prefix: a
// top-level directory may genuinely be named a/ or b/.
func NormalizePath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimPrefix(p, "/")
	return p
}

// PathCandidates returns p and the versions with one a/ or b/ diff prefix
// stripped, so evidence that copied a header path still matches.
func PathCandidates(p string) []string {
	n := NormalizePath(p)
	out := []string{n}
	for _, prefix := range []string{"a/", "b/"} {
		if trimmed := strings.TrimPrefix(n, prefix); trimmed != n {
			out = append(out, trimmed)
		}
	}
	return out
}
