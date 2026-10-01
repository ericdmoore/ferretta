package review

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"
)

// The workspace is private to one attempt. Check evidence binds to a Git tree,
// not a mutable bool; even unexpected edits between tools invalidate it.
type repairWorkspace struct {
	policy      RepairPolicy
	checkedTree string
}

func repairRead(path, name string) ([]byte, error) {
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	// Refuse symlinks in every component, including aliases into protected
	// directories. OpenRoot additionally prevents traversal outside this workspace.
	var info fs.FileInfo
	for i := range strings.Split(name, "/") {
		prefix := strings.Join(strings.Split(name, "/")[:i+1], "/")
		info, err = root.Lstat(prefix)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("repair paths cannot traverse symlinks")
		}
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil, fmt.Errorf("repair tools require regular files no larger than 1 MiB")
	}
	return root.ReadFile(name)
}

func repairPatch(path string, patch ApplyPatch) error {
	root, err := os.OpenRoot(path)
	if err != nil {
		return err
	}
	defer root.Close()
	data, err := repairRead(path, patch.path)
	if patch.old == "" {
		parts := strings.Split(patch.path, "/")
		for i := 1; i < len(parts); i++ {
			info, err := root.Lstat(strings.Join(parts[:i], "/"))
			if err != nil || !info.IsDir() {
				return fmt.Errorf("new-file parent must be an existing ordinary directory")
			}
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("new-file patch requires a missing file")
		}
		f, err := root.OpenFile(patch.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = f.WriteString(patch.replacement)
		return err
	}
	if err != nil {
		return err
	}
	if !utf8.Valid(data) || strings.ContainsRune(string(data), '\x00') || strings.Count(string(data), patch.old) != 1 {
		return fmt.Errorf("old_text must match exactly once in a text file; read_file and retry")
	}
	return root.WriteFile(patch.path, []byte(strings.Replace(string(data), patch.old, patch.replacement, 1)), 0644)
}

func (r Runner) repairTree(ctx context.Context, w Workspace) (string, error) {
	// Stage ignored-file exclusions using normal Git rules, then bind all changes
	// to an immutable tree. This never commits or pushes anything.
	if _, err := r.Exec.Run(ctx, w.path, "git", "add", "-A", "--", "."); err != nil {
		return "", err
	}
	data, err := r.Exec.Run(ctx, w.path, "git", "diff", "--cached", "--name-only", "-z", w.pr.Head, "--")
	if err != nil {
		return "", err
	}
	for _, path := range strings.Split(strings.TrimSuffix(string(data), "\x00"), "\x00") {
		if path != "" && w.repair.policy.protectedPath(path) {
			return "", fmt.Errorf("repair changed protected control %q; candidate refused", path)
		}
	}
	data, err = r.Exec.Run(ctx, w.path, "git", "write-tree")
	if err != nil {
		return "", err
	}
	tree := strings.TrimSpace(string(data))
	if !shaPattern.MatchString(tree) {
		return "", fmt.Errorf("invalid repair tree")
	}
	return tree, nil
}

func (r Runner) repairFiles(ctx context.Context, w Workspace) ([]string, error) {
	data, err := r.Exec.Run(ctx, w.path, "git", "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimSuffix(string(data), "\x00"), "\x00"), nil
}

func (r Runner) repairSearch(ctx context.Context, w Workspace, s Search) (string, error) {
	files, err := r.repairFiles(ctx, w)
	if err != nil {
		return "", err
	}
	page := SearchPage{Revision: "working-tree", Matches: []SearchMatch{}}
	var pattern *regexp.Regexp
	if s.regex {
		pattern, _ = regexp.CompilePOSIX(s.query)
	} // Already validated by Plan.
	found := 0
	for _, file := range files {
		if s.path != "" && s.path != "." && file != s.path && !strings.HasPrefix(file, s.path+"/") {
			continue
		}
		data, err := repairRead(w.path, file)
		if err != nil || strings.ContainsRune(string(data), '\x00') {
			continue
		}
		for i, line := range strings.Split(string(data), "\n") {
			matches := strings.Contains(line, s.query)
			if pattern != nil {
				matches = pattern.MatchString(line)
			}
			if !matches {
				continue
			}
			found++
			if found <= s.skip {
				continue
			}
			if len(page.Matches) == s.limit {
				page.HasMore = true
				next := s.skip + len(page.Matches)
				page.NextOffset = &next
				break
			}
			truncated := len(line) > 400
			if truncated {
				line = line[:400]
			}
			page.Matches = append(page.Matches, SearchMatch{Path: file, Line: i + 1, Text: line, TextTruncated: truncated})
		}
		if page.HasMore {
			break
		}
	}
	data, _ := json.Marshal(page)
	return string(data), nil
}
