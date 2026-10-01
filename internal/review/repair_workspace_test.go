package review

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepairFileBoundary(t *testing.T) {
	dir := t.TempDir()
	if err := repairPatch(dir, ApplyPatch{"new.txt", "", "created"}); err != nil {
		t.Fatal(err)
	}
	if data, err := repairRead(dir, "new.txt"); err != nil || string(data) != "created" {
		t.Fatal(string(data), err)
	}
	for _, patch := range []ApplyPatch{{"new.txt", "", "exists"}, {"missing.txt", "old", "new"}, {"new.txt", "wrong", "new"}, {"missing/file", "", "x"}} {
		if err := repairPatch(dir, patch); err == nil {
			t.Fatal("invalid patch accepted", patch)
		}
	}
	_ = os.WriteFile(filepath.Join(dir, "binary"), []byte{'x', 0, 'x'}, 0644)
	if err := repairPatch(dir, ApplyPatch{"binary", "x", "new"}); err == nil {
		t.Fatal("binary edit accepted")
	}
	_ = os.Symlink("new.txt", filepath.Join(dir, "alias"))
	_ = os.Mkdir(filepath.Join(dir, "real"), 0700)
	_ = os.Symlink("real", filepath.Join(dir, "aliasdir"))
	if err := repairPatch(dir, ApplyPatch{"aliasdir/new", "", "new"}); err == nil {
		t.Fatal("symlink parent accepted")
	}
	if _, err := repairRead(dir, "alias"); err == nil {
		t.Fatal("symlink file read")
	}
	if _, err := repairRead(dir, "real"); err == nil {
		t.Fatal("directory read")
	}
	if _, err := repairRead(dir+"/missing", "a"); err == nil {
		t.Fatal("missing root")
	}
	if err := repairPatch(dir+"/missing", ApplyPatch{"a", "b", "c"}); err == nil {
		t.Fatal("missing patch root")
	}
	if err := repairPatch(dir, ApplyPatch{"real/new", "", "created"}); err != nil {
		t.Fatal(err)
	}
	if err := repairPatch(dir, ApplyPatch{"real/new", "created", "changed"}); err != nil {
		t.Fatal(err)
	}
}

func TestRepairInspectionSeesEdits(t *testing.T) {
	f := newRepairFixture(t)
	f.model.replies = []Reply{reply("finish_review", changesJSON), reply("apply_patch", `{"path":"main.go","old_text":"package main","new_text":"package main // repaired"}`), reply("read_file", `{"path":"main.go"}`), reply("list_files", `{}`), reply("search", `{"query":"repaired"}`), reply("read_diff", `{}`), reply("run_tests", `{}`), reply("finish_repair", `{"summary":"Verified working tree"}`)}
	job, err := f.advance()
	if err != nil {
		t.Fatal(err)
	}
	messages := job.Runs[0].Repair.Session.Messages
	file, search := false, false
	for _, m := range messages {
		if m.Role == "tool" && strings.Contains(m.Content, "repaired") {
			file = file || m.ToolName == "read_file"
			search = search || m.ToolName == "search"
		}
	}
	if !file || !search || f.git.pushes != 1 {
		t.Fatal("tools missed edits", messages)
	}
}

func TestRepairSearchPagingAndErrors(t *testing.T) {
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "a"), []byte("needle\nneedle "+strings.Repeat("x", 500)+"\nneedle\n"), 0644)
	_ = os.WriteFile(filepath.Join(root, "bin"), []byte{0}, 0644)
	r := Runner{Exec: commandFunc(func(context.Context, string, string, ...string) ([]byte, error) {
		return []byte("missing\x00bin\x00a\x00"), nil
	})}
	w := Workspace{path: root}
	for _, regex := range []bool{true, false} {
		result, err := r.repairSearch(context.Background(), w, Search{query: "needle", regex: regex, limit: 1, skip: 1})
		var page SearchPage
		_ = json.Unmarshal([]byte(result), &page)
		if err != nil || !page.HasMore || len(page.Matches) != 1 || !page.Matches[0].TextTruncated || *page.NextOffset != 2 {
			t.Fatal(result, err)
		}
	}
	if result, err := r.repairSearch(context.Background(), w, Search{query: "needle", path: "other", limit: 10}); err != nil || strings.Contains(result, `"path":"a"`) {
		t.Fatal(result, err)
	}
	r.Exec = commandFunc(func(context.Context, string, string, ...string) ([]byte, error) { return nil, errors.New("git failed") })
	if _, err := r.repairSearch(context.Background(), w, Search{query: "x", limit: 1}); err == nil {
		t.Fatal("search failure hidden")
	}
}

func TestRepairTreeBoundary(t *testing.T) {
	for _, fail := range []string{"add", "diff", "write-tree", "invalid-tree"} {
		r := Runner{Exec: commandFunc(func(_ context.Context, _ string, _ string, args ...string) ([]byte, error) {
			if args[0] == fail {
				return nil, errors.New("git failed")
			}
			if args[0] == "write-tree" {
				return []byte("invalid"), nil
			}
			return nil, nil
		})}
		if _, err := r.repairTree(context.Background(), Workspace{pr: pr(), repair: &repairWorkspace{policy: repairPolicy(t)}}); err == nil {
			t.Fatal(fail)
		}
	}
}

func TestRepairCreateFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory write permissions")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0700)
	if err := repairPatch(dir, ApplyPatch{"new", "", "content"}); err == nil {
		t.Fatal("read-only workspace creation succeeded")
	}
}
