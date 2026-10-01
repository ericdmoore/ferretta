package review

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func searchCommand(t *testing.T, raw string) Search {
	t.Helper()
	c, err := Plan("search", json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	return c.(Search)
}

func TestSearchPolicy(t *testing.T) {
	for _, args := range []string{`{`, `{} {}`, `null`, `{"query":"x","extra":1}`, `{"query":""}`, `{"query":"a\nb"}`, `{"query":"a\u0000b"}`, `{"query":"x","path":"../outside"}`, `{"query":"x","path":"/outside"}`, `{"query":"x","path":"a\nb"}`, `{"query":"x","max_results":0}`, `{"query":"x","max_results":101}`, `{"query":"x","offset":-1}`, `{"query":"x","offset":10001}`, `{"query":"[","regex":true}`, `{"query":"(?i)word","regex":true}`, `{"query":"` + strings.Repeat("x", 1025) + `"}`} {
		if _, err := Plan("search", json.RawMessage(args)); err == nil {
			t.Fatalf("accepted %s", args)
		}
	}
	defaultSearch := searchCommand(t, `{"query":"["}`)
	if defaultSearch.regex || defaultSearch.limit != 20 || defaultSearch.skip != 0 {
		t.Fatal(defaultSearch)
	}
	searchCommand(t, `{"query":"^word[0-9]+$","path":".","regex":true,"max_results":100,"offset":10000}`).command()
}

func searchRecord(path string, line int, text string) string {
	return fmt.Sprintf("%s:%s\x00%d\x00%s\n", head, path, line, text)
}

func TestSearchPages(t *testing.T) {
	input := searchRecord("a.go", 1, "first") + searchRecord("b.go", 2, "second") + searchRecord("c.go", 3, "third")
	page, err := collectSearch(strings.NewReader(input), head, searchCommand(t, `{"query":"x","max_results":1,"offset":1}`))
	if err != nil || len(page.Matches) != 1 || page.Matches[0].Path != "b.go" || page.Matches[0].Line != 2 || !page.HasMore || page.NextOffset == nil || *page.NextOffset != 2 {
		t.Fatal(page, err)
	}
	last, err := collectSearch(strings.NewReader(input), head, searchCommand(t, `{"query":"x","offset":2}`))
	if err != nil || len(last.Matches) != 1 || last.Matches[0].Text != "third" || last.HasMore || last.NextOffset != nil {
		t.Fatal(last, err)
	}
	empty, err := collectSearch(strings.NewReader(input), head, searchCommand(t, `{"query":"x","offset":3}`))
	if err != nil || len(empty.Matches) != 0 || empty.HasMore {
		t.Fatal(empty, err)
	}
	long := searchRecord("line\nbreak:name.go", 8, strings.Repeat("x", 1<<20))
	page, err = collectSearch(strings.NewReader(long), head, searchCommand(t, `{"query":"x"}`))
	if err != nil || len(page.Matches) != 1 || !page.Matches[0].TextTruncated || len(page.Matches[0].Text) != 2048 || page.Matches[0].Path != "line\nbreak:name.go" {
		t.Fatal(page, err)
	}
	page, err = collectSearch(strings.NewReader(strings.Repeat(searchRecord("a.go", 1, strings.Repeat("x", 2048)), 100)), head, searchCommand(t, `{"query":"x","max_results":100}`))
	encoded, _ := json.Marshal(page)
	if err != nil || !page.HasMore || len(page.Matches) >= 100 || len(encoded) >= 64000 || *page.NextOffset != len(page.Matches) {
		t.Fatal(len(encoded), page, err)
	}
	page, err = collectSearch(strings.NewReader(strings.Repeat(searchRecord("a", 1, "x"), 10002)), head, searchCommand(t, `{"query":"x","offset":10000,"max_results":1}`))
	if err != nil || !page.HasMore || page.NextOffset != nil || page.Note == "" {
		t.Fatal(page, err)
	}
}

func TestSearchMalformedOutput(t *testing.T) {
	for _, input := range []string{"broken", base + ":a\x001\x00x\n", head + ":" + strings.Repeat("x", 8192) + "\x001\x00x\n", head + ":a\x00wrong\x00x\n", head + ":a\x00" + strings.Repeat("1", 40) + "\x00x\n", head + ":a\x000\x00x\n", head + ":a\x001\x00unfinished", searchRecord(strings.Repeat("\x01", 8100), 1, strings.Repeat("\x01", 2048))} {
		if _, err := collectSearch(strings.NewReader(input), head, searchCommand(t, `{"query":"x"}`)); err == nil {
			t.Fatal("accepted malformed or oversized result")
		}
	}
	if _, err := collectSearch(errorReader{}, head, searchCommand(t, `{"query":"x"}`)); err == nil {
		t.Fatal("ignored read failure")
	}
	var log cappedSearchLog
	if n, err := log.Write([]byte(strings.Repeat("x", 20000))); err != nil || n != 20000 || log.Len() != 8192 {
		t.Fatal(n, err, log.Len())
	}
	_, _ = log.Write([]byte("ignored"))
	if log.Len() != 8192 {
		t.Fatal("stderr grew beyond bound")
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestGitSearchExactRevision(t *testing.T) {
	dir := t.TempDir()
	process := Process{}
	git := func(args ...string) string {
		t.Helper()
		out, err := process.Run(context.Background(), dir, "git", args...)
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	git("config", "core.hooksPath", t.TempDir())
	git("config", "commit.gpgsign", "false")
	git("config", "user.name", "Ferretta Test")
	git("config", "user.email", "test@example.invalid")
	if err := os.Mkdir(filepath.Join(dir, "src"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"src/a.go": "a.b\nacb\n--option\n$(touch injected)\n", "src/b.go": "a.b\n", "src/a:odd\nname": "a.b\n", "binary": "\x00a.b\n", ":(glob)*": "a.b\n", "outside.go": "a.b\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	git("add", ".")
	git("commit", "-qm", "fixture")
	revision := git("rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(dir, "src/a.go"), []byte("new revision only\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-qm", "new head")
	// User Git presentation settings must not change the machine protocol.
	git("config", "grep.column", "true")
	git("config", "grep.lineNumber", "false")
	git("config", "grep.fullName", "false")
	git("config", "submodule.recurse", "true")
	for _, test := range []struct {
		args  string
		count int
	}{
		{`{"query":"a.b"}`, 5},
		{`{"query":"a.b","path":"src"}`, 3},
		{`{"query":"a.b","regex":true,"path":"src/a.go"}`, 2},
		{`{"query":"new revision only"}`, 0},
		{`{"query":"missing"}`, 0},
		{`{"query":"a.b","path":":(glob)*"}`, 1},
		{`{"query":"--option"}`, 1},
		{`{"query":"$(touch injected)"}`, 1},
	} {
		page, err := process.Search(context.Background(), dir, revision, searchCommand(t, test.args))
		if err != nil || len(page.Matches) != test.count || page.Revision != revision {
			t.Fatalf("%s: %+v %v", test.args, page, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "injected")); !os.IsNotExist(err) {
		t.Fatal("query executed as a shell command")
	}
	first, err := process.Search(context.Background(), dir, revision, searchCommand(t, `{"query":"a.b","path":"src","max_results":1}`))
	if err != nil || !first.HasMore || first.NextOffset == nil {
		t.Fatal(first, err)
	}
	second, err := process.Search(context.Background(), dir, revision, searchCommand(t, `{"query":"a.b","path":"src","max_results":1,"offset":1}`))
	if err != nil || first.Matches[0].Path == second.Matches[0].Path {
		t.Fatal(second, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := process.Search(ctx, dir, revision, searchCommand(t, `{"query":"a.b"}`)); err == nil {
		t.Fatal("ignored cancellation")
	}
	for _, bad := range []struct {
		dir, revision string
		command       Search
	}{{dir, "HEAD", searchCommand(t, `{"query":"x"}`)}, {dir, revision, Search{}}, {t.TempDir(), revision, searchCommand(t, `{"query":"x"}`)}, {dir, strings.Repeat("f", 40), searchCommand(t, `{"query":"x"}`)}} {
		if _, err := process.Search(context.Background(), bad.dir, bad.revision, bad.command); err == nil {
			t.Fatal("accepted invalid search target")
		}
	}
}

func TestSearchRejectsMalformedExecutorOutput(t *testing.T) {
	// A local fake executable exercises the adapter boundary without network,
	// inference, or timing assumptions. It stands in for a failing Git process.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte("#!/bin/sh\nprintf broken\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	if _, err := (Process{}).Search(context.Background(), dir, head, searchCommand(t, `{"query":"x"}`)); err == nil {
		t.Fatal("malformed search output became successful empty evidence")
	}
	// Git's no-match exit code is valid only when it did not return matches.
	script := "#!/bin/sh\nprintf '" + head + ":a\\0001\\000x\\n'\nexit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := (Process{}).Search(context.Background(), dir, head, searchCommand(t, `{"query":"x"}`)); err == nil {
		t.Fatal("inconsistent no-match status accepted")
	}
}

type searchFunc func(context.Context, string, string, Search) (SearchPage, error)

func (f searchFunc) Search(ctx context.Context, dir, revision string, command Search) (SearchPage, error) {
	return f(ctx, dir, revision, command)
}

func TestSearchReviewAndRecovery(t *testing.T) {
	for _, fail := range []bool{false, true} {
		m := &fakeModel{replies: []Reply{reply("search", `{"query":"target","path":"src"}`), reply("run_checks", `{}`), reply("finish_review", finishJSON("lgtm"))}}
		r := runner(m)
		calls := 0
		r.Searcher = searchFunc(func(_ context.Context, dir, revision string, c Search) (SearchPage, error) {
			calls++
			if dir != workspace().path || revision != head || c.query != "target" || c.path != "src" {
				t.Fatal("lost search scope")
			}
			if fail {
				return SearchPage{}, errors.New("object unavailable")
			}
			return SearchPage{Revision: revision, Matches: []SearchMatch{{Path: "src/a.go", Line: 7, Text: "target"}}}, nil
		})
		report := r.Review(context.Background(), policy(t), workspace(), nil, func(Report, []Message) error { return nil })
		if report.Status != "lgtm" || calls != 1 {
			t.Fatal(report, calls)
		}
		content := m.requests[1][len(m.requests[1])-1].Content
		if fail {
			var problem toolProblem
			if err := json.Unmarshal([]byte(content), &problem); err != nil || problem.Error != "execution_failed" || problem.Message != "object unavailable" {
				t.Fatal(content, err)
			}
		} else {
			var page SearchPage
			if err := json.Unmarshal([]byte(content), &page); err != nil || !reflect.DeepEqual(page.Matches, []SearchMatch{{Path: "src/a.go", Line: 7, Text: "target"}}) {
				t.Fatal(content, err)
			}
		}
	}
	m := &fakeModel{replies: []Reply{reply("search", `{"query":"x"}`), reply("request_intent_confirmation", askJSON)}}
	runner(m).Review(context.Background(), policy(t), workspace(), nil, func(Report, []Message) error { return nil })
	var problem toolProblem
	_ = json.Unmarshal([]byte(m.requests[1][len(m.requests[1])-1].Content), &problem)
	if problem.Error != "execution_failed" {
		t.Fatal(problem)
	}
}
