package review

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// Search is constructed by Plan; its zero value cannot execute a search.
type Search struct {
	query, path string
	regex       bool
	limit, skip int
}

func (Search) command() {}

type searchArgs struct {
	Query      string `json:"query"`
	Path       string `json:"path,omitempty"`
	Regex      bool   `json:"regex,omitempty"`
	MaxResults *int   `json:"max_results,omitempty"`
	Offset     int    `json:"offset,omitempty"`
}

func (a searchArgs) plan() (Search, error) {
	if a.Query == "" || len(a.Query) > 1024 || strings.ContainsAny(a.Query, "\x00\r\n") {
		return Search{}, fmt.Errorf("query must contain 1–1024 bytes with no NUL or newline")
	}
	if a.Path != "" && (!fs.ValidPath(a.Path) || strings.ContainsAny(a.Path, "\x00\r\n")) {
		return Search{}, fmt.Errorf("path must be a literal repository-relative file or directory; omit for the whole commit")
	}
	limit := 20
	if a.MaxResults != nil {
		limit = *a.MaxResults
	}
	if limit < 1 || limit > 100 || a.Offset < 0 || a.Offset > 10000 {
		return Search{}, fmt.Errorf("max_results must be 1–100 (default 20); offset must be 0–10000")
	}
	if a.Regex {
		if _, err := regexp.CompilePOSIX(a.Query); err != nil {
			return Search{}, fmt.Errorf("invalid POSIX extended regex: %w; use regex:false for literal text", err)
		}
	}
	return Search{query: a.Query, path: a.Path, regex: a.Regex, limit: limit, skip: a.Offset}, nil
}

type SearchMatch struct {
	Path          string `json:"path"`
	Line          int    `json:"line"`
	Text          string `json:"text"`
	TextTruncated bool   `json:"text_truncated,omitempty"`
}

type SearchPage struct {
	Revision   string        `json:"revision"`
	Matches    []SearchMatch `json:"matches"`
	HasMore    bool          `json:"has_more"`
	NextOffset *int          `json:"next_offset,omitempty"`
	Note       string        `json:"note,omitempty"`
}

type Searcher interface {
	Search(context.Context, string, string, Search) (SearchPage, error)
}

// Search uses Git's object database, never working-tree content or a shell.
// Output is consumed incrementally; stopping a page also stops the child process.
func (Process) Search(ctx context.Context, dir, revision string, search Search) (SearchPage, error) {
	if !shaPattern.MatchString(revision) || search.query == "" || search.limit < 1 {
		return SearchPage{}, fmt.Errorf("search requires a validated command and exact revision")
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	mode := "-F"
	if search.regex {
		mode = "-E"
	}
	args := []string{"-c", "grep.threads=1", "grep", "--no-color", "--no-heading", "--no-break", "--no-column", "--no-textconv", "--no-recurse-submodules", "--full-name", "-n", "-z", "-I", mode, "-e", search.query, revision, "--"}
	if search.path != "" && search.path != "." {
		args = append(args, ":(literal)"+search.path)
	}
	cmd := exec.CommandContext(runCtx, "git", args...)
	cmd.Dir, cmd.Env = dir, cleanEnvironment(os.Environ())
	var stderr cappedSearchLog
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return SearchPage{}, err
	}
	if err := cmd.Start(); err != nil {
		return SearchPage{}, err
	}
	page, readErr := collectSearch(stdout, revision, search)
	if readErr != nil || page.HasMore {
		cancel()
	}
	waitErr := cmd.Wait()
	if err := errors.Join(ctx.Err(), readErr); err != nil {
		return SearchPage{}, err
	}
	if waitErr != nil && !page.HasMore {
		var exit *exec.ExitError
		if !errors.As(waitErr, &exit) || exit.ExitCode() != 1 || len(page.Matches) != 0 {
			return SearchPage{}, fmt.Errorf("git search failed: %w: %s", waitErr, stderr.String())
		}
	}
	return page, nil
}

type cappedSearchLog struct{ bytes.Buffer }

func (b *cappedSearchLog) Write(p []byte) (int, error) {
	n := len(p)
	if left := 8192 - b.Len(); left > 0 {
		_, _ = b.Buffer.Write(p[:min(left, n)])
	}
	return n, nil
}

// readSearchField discards bytes beyond the bound without buffering a long line.
func readSearchField(r *bufio.Reader, delimiter byte, limit int) (string, bool, error) {
	var b strings.Builder
	truncated := false
	for {
		part, err := r.ReadSlice(delimiter)
		if err == nil {
			part = part[:len(part)-1]
		}
		n := min(len(part), limit-b.Len())
		b.Write(part[:n])
		truncated = truncated || n < len(part)
		if err != bufio.ErrBufferFull {
			return b.String(), truncated, err
		}
	}
}

func collectSearch(input io.Reader, revision string, search Search) (SearchPage, error) {
	page := SearchPage{Revision: revision, Matches: []SearchMatch{}}
	r := bufio.NewReader(input)
	used := 0
	for seen := 0; ; seen++ {
		path, oversized, err := readSearchField(r, 0, 8192)
		if err == io.EOF && path == "" {
			return page, nil
		}
		if err != nil || oversized || !strings.HasPrefix(path, revision+":") {
			return SearchPage{}, fmt.Errorf("cannot read search path for the reviewed revision")
		}
		lineText, oversized, err := readSearchField(r, 0, 32)
		line, parseErr := strconv.Atoi(lineText)
		if err != nil || oversized || parseErr != nil || line < 1 {
			return SearchPage{}, fmt.Errorf("cannot read search line number")
		}
		text, truncated, err := readSearchField(r, '\n', 2048)
		if err != nil {
			return SearchPage{}, fmt.Errorf("cannot read complete search result: %w", err)
		}
		if seen < search.skip {
			continue
		}
		match := SearchMatch{Path: strings.TrimPrefix(path, revision+":"), Line: line, Text: text, TextTruncated: truncated}
		encoded, _ := json.Marshal(match)
		if len(encoded) > 48000 {
			return SearchPage{}, fmt.Errorf("search result exceeds page byte limit; narrow the path or query")
		}
		if len(page.Matches) == search.limit || used+len(encoded) > 48000 {
			page.HasMore = true
			next := search.skip + len(page.Matches)
			if next <= 10000 {
				page.NextOffset = &next
			} else {
				page.Note = "Pagination limit reached; narrow path or query to inspect remaining matches."
			}
			return page, nil
		}
		page.Matches = append(page.Matches, match)
		used += len(encoded)
	}
}
