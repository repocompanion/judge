// Package docs retrieves applicable documents from a Companion store.
package docs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// Doc is one document selected for review.
type Doc struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Path      string   `json:"path"`
	Kind      string   `json:"kind"`
	Status    string   `json:"status"`
	Scope     string   `json:"scope"`
	Score     float64  `json:"score"`
	Snippet   string   `json:"snippet,omitempty"`
	Body      string   `json:"-"`
	Truncated bool     `json:"-"`
	Warnings  []string `json:"-"`
}

// Source returns the documents that apply to a task.
type Source interface {
	Guidance(ctx context.Context, query string, max int) ([]Doc, error)
}

// CompanionSource shells out to `companion guidance --store`.
type CompanionSource struct {
	Bin          string
	Store        string
	Project      string
	Team         string
	Language     string
	MaxBodyBytes int

	// Run overrides command execution in tests.
	Run func(ctx context.Context, args ...string) ([]byte, error)
}

func (s *CompanionSource) Guidance(ctx context.Context, query string, max int) ([]Doc, error) {
	bin := s.Bin
	if bin == "" {
		bin = "companion"
	}
	args := []string{"guidance"}
	if s.Store != "" {
		args = append(args, "--store", s.Store)
	}
	if s.Project != "" {
		args = append(args, "--project", s.Project)
	}
	if s.Team != "" {
		args = append(args, "--team", s.Team)
	}
	if s.Language != "" {
		args = append(args, "--lang", s.Language)
	}
	if max > 0 {
		args = append(args, "--max", strconv.Itoa(max))
	}
	args = append(args, "--json", query)

	var out []byte
	var err error
	if s.Run != nil {
		out, err = s.Run(ctx, args...)
	} else {
		cmd := exec.CommandContext(ctx, bin, args...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err = cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("%s guidance: %w: %s", bin, err, strings.TrimSpace(stderr.String()))
		}
	}
	if err != nil {
		return nil, err
	}

	var raw []struct {
		ID      string  `json:"id"`
		Title   string  `json:"title"`
		Path    string  `json:"path"`
		Kind    string  `json:"kind"`
		Status  string  `json:"status"`
		Scope   string  `json:"scope"`
		Score   float64 `json:"score"`
		Snippet string  `json:"snippet"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("decode guidance: %w", err)
	}

	outDocs := make([]Doc, 0, len(raw))
	for _, r := range raw {
		body, truncated, berr := ReadBody(s.Store, r.Path, s.MaxBodyBytes)
		d := Doc{
			ID: r.ID, Title: r.Title, Path: r.Path, Kind: r.Kind,
			Status: r.Status, Scope: r.Scope, Score: r.Score, Snippet: r.Snippet,
			Body: body, Truncated: truncated,
		}
		if berr != nil {
			d.Warnings = append(d.Warnings, berr.Error())
		}
		outDocs = append(outDocs, d)
	}
	return outDocs, nil
}

// ReadBody reads a document from a store checkout, rejecting absolute paths and
// traversal outside root. It truncates at max bytes when max > 0.
func ReadBody(root, rel string, max int) (string, bool, error) {
	clean := path.Clean(strings.TrimSpace(rel))
	if clean == "" || clean == "." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") || filepath.IsAbs(clean) {
		return "", false, fmt.Errorf("invalid document path %q", rel)
	}
	full := filepath.Join(root, filepath.FromSlash(clean))
	rootClean := filepath.Clean(root)
	if full != rootClean && !strings.HasPrefix(full, rootClean+string(os.PathSeparator)) {
		return "", false, fmt.Errorf("document path %q escapes the store", rel)
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return "", false, err
	}
	if max > 0 && len(b) > max {
		return string(b[:max]), true, nil
	}
	return string(b), false, nil
}
