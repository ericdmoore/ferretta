package review

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type failingIO struct{}

func (failingIO) Read([]byte) (int, error)  { return 0, errors.New("read failed") }
func (failingIO) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestModelTransportFailures(t *testing.T) {
	p := policy(t)
	o := Ollama{HTTP: httpFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(failingIO{})}, nil
	})}
	if e := o.Capabilities(context.Background(), p); e == nil {
		t.Fatal("lost response read failure")
	}
	if _, e := o.Turn(context.Background(), p, []Message{{ToolCalls: []ToolCall{{Function: struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}{Arguments: json.RawMessage("invalid")}}}}}); e == nil {
		t.Fatal("invalid continuation accepted")
	}
	client := NewCLI().Runner.Model.(Ollama).HTTP.(*http.Client)
	if e := client.CheckRedirect(&http.Request{}, nil); e == nil {
		t.Fatal("local model could redirect off machine")
	}
}

func TestProcess(t *testing.T) {
	data, e := (Process{}).Run(context.Background(), t.TempDir(), "sh", "-c", "printf output")
	if e != nil || string(data) != "output" {
		t.Fatal(string(data), e)
	}
	data, e = (Process{}).Run(context.Background(), "", "sh", "-c", "printf partial; printf failed >&2; exit 1")
	if e == nil || string(data) != "partial" || !strings.Contains(e.Error(), "failed") {
		t.Fatal(string(data), e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := (Process{}).Run(ctx, "", "sh", "-c", "exit 0"); e == nil {
		t.Fatal("ignored cancellation")
	}
}

func TestReviewCLI(t *testing.T) {
	for _, mode := range []string{"success", "stale", "fetch_failed", "prepare_failed", "temp_failed", "storage_failed", "storage_unwritable", "report_failed", "write_failed", "cleanup_failed", "incomplete"} {
		t.Run(mode, func(t *testing.T) {
			t.Chdir(t.TempDir())
			if e := os.WriteFile("policy.json", []byte(policyJSON), 0600); e != nil {
				t.Fatal(e)
			}
			m := &fakeModel{replies: []Reply{reply("run_checks", `{}`), reply("finish_review", finishJSON("lgtm"))}}
			if mode == "incomplete" {
				m.capErr = errors.New("missing thinking capability")
			}
			r := runner(m)
			fetches := 0
			r.Exec = commandFunc(func(c context.Context, d, n string, a ...string) ([]byte, error) {
				if n == "gh" {
					fetches++
					if mode == "fetch_failed" {
						return nil, errors.New("offline")
					}
					if fetches == 2 && mode == "stale" {
						changed := pr()
						changed.Head = strings.Repeat("c", 40)
						return json.Marshal(changed)
					}
					if fetches == 2 && mode == "report_failed" {
						dirs, _ := filepath.Glob(".ferretta/runs/*")
						if e := os.Mkdir(filepath.Join(dirs[0], "report.json"), 0700); e != nil {
							t.Fatal(e)
						}
					}
				}
				if n == "git" && a[0] == "remote" && mode == "prepare_failed" {
					return nil, errors.New("wrong remote")
				}
				if n == "git" && a[0] == "worktree" && a[1] == "remove" && mode == "cleanup_failed" {
					return nil, errors.New("cleanup failed")
				}
				return defaultExec(c, d, n, a...)
			})
			if mode == "temp_failed" {
				t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
			}
			if mode == "storage_failed" {
				if e := os.Mkdir(".ferretta", 0700); e != nil {
					t.Fatal(e)
				}
				if e := os.WriteFile(".ferretta/runs", nil, 0600); e != nil {
					t.Fatal(e)
				}
			}
			if mode == "storage_unwritable" {
				if os.Geteuid() == 0 {
					t.Skip("root can write read-only directories")
				}
				if e := os.MkdirAll(".ferretta/runs", 0700); e != nil {
					t.Fatal(e)
				}
				if e := os.Chmod(".ferretta/runs", 0500); e != nil {
					t.Fatal(e)
				}
				defer os.Chmod(".ferretta/runs", 0700)
			}
			var out, errs bytes.Buffer
			var output io.Writer = &out
			if mode == "write_failed" {
				output = failingIO{}
			}
			code := (CLI{Runner: r}).Run(context.Background(), []string{"--repo", "o/r", "--pr", "1", "--policy", "policy.json"}, output, &errs)
			if mode == "success" || mode == "cleanup_failed" {
				if code != 0 || !strings.Contains(out.String(), `"status":"lgtm"`) {
					t.Fatalf("code %d, output %s, errors %s", code, &out, &errs)
				}
				if strings.Contains(out.String(), "private continuation") {
					t.Fatal("public report leaked reasoning")
				}
				dirs, _ := filepath.Glob(".ferretta/runs/*")
				data, e := os.ReadFile(filepath.Join(dirs[0], "session.json"))
				if e != nil || !bytes.Contains(data, []byte("private continuation")) {
					t.Fatal("checkpoint lost continuation", e)
				}
			} else if code == 0 {
				t.Fatalf("%s incorrectly approved", mode)
			}
		})
	}
}

func TestReviewCLIArguments(t *testing.T) {
	t.Chdir(t.TempDir())
	c := CLI{Runner: runner(&fakeModel{})}
	for _, args := range [][]string{nil, {"--unknown"}, {"--repo", "o/r", "--pr", "1"}} {
		if code := c.Run(context.Background(), args, io.Discard, io.Discard); code != 1 {
			t.Fatal("invalid input accepted")
		}
	}
	if e := os.WriteFile("bad.json", []byte("{}"), 0600); e != nil {
		t.Fatal(e)
	}
	if code := c.Run(context.Background(), []string{"--repo", "o/r", "--pr", "1", "--policy", "bad.json"}, io.Discard, io.Discard); code != 1 {
		t.Fatal("bad policy accepted")
	}
}
