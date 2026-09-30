package review

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testModelInfo = `{"capabilities":["tools","thinking"],"details":{"family":"gptoss"},"model_info":{"general.architecture":"gptoss","gptoss.context_length":131072}}`

const policyJSON = `{"provider":"ollama","endpoint":"http://127.0.0.1:11434","model":"local-model","effort":"medium","max_turns":5,"max_tokens_per_turn":4096,"timeout_seconds":60,"checks":[["make","check"]]}`

var head = strings.Repeat("a", 40)
var base = strings.Repeat("b", 40)

func policy(t *testing.T) Policy {
	t.Helper()
	p, e := ParsePolicy([]byte(policyJSON))
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func pr() PR {
	return PR{Number: 1, Head: head, Base: base, URL: "https://github.com/o/r/pull/1", State: "OPEN", Title: "Test PR", Body: "Implement the agreed behavior"}
}

func TestPolicyBoundary(t *testing.T) {
	for _, bad := range []string{
		`{`, policyJSON + ` {}`, policyJSON + ` garbage`, strings.Replace(policyJSON, `"provider":"ollama"`, `"provider":"hosted"`, 1),
		strings.Replace(policyJSON, `"medium"`, `"unlimited"`, 1), strings.Replace(policyJSON, `"local-model"`, `""`, 1),
		strings.Replace(policyJSON, `"max_turns":5`, `"max_turns":-1`, 1), strings.Replace(policyJSON, `"checks":[["make","check"]]`, `"checks":[]`, 1),
		strings.Replace(policyJSON, `"checks":[["make","check"]]`, `"checks":[[]]`, 1), strings.Replace(policyJSON, `"provider"`, `"unknown"`, 1),
	} {
		if _, e := ParsePolicy([]byte(bad)); e == nil {
			t.Fatalf("accepted invalid policy: %s", bad)
		}
	}
	for _, endpoint := range []string{"https://example.com", "http://localhost.evil.test", "http://user@localhost", "http://localhost/path", "http://localhost?x=1", "http://localhost#x", "%"} {
		if _, e := ParsePolicy([]byte(strings.Replace(policyJSON, "http://127.0.0.1:11434", endpoint, 1))); e == nil {
			t.Fatalf("accepted nonlocal endpoint %s", endpoint)
		}
	}
}

func finishJSON(verdict string) string {
	return fmt.Sprintf(`{"verdict":%q,"summary":"Evidence supports this result","tradeoffs":["Small explicit implementation"],"findings":[],"question":""}`, verdict)
}
func TestToolPolicyAndVerdicts(t *testing.T) {
	for _, tt := range []struct {
		name, args string
		valid      bool
	}{
		{"read_file", `{"path":"internal/review/core.go"}`, true}, {"read_file", `{"path":"../secret"}`, false}, {"read_file", `{"path":"."}`, false}, {"read_file", `{`, false},
		{"list_files", `{}`, true}, {"run_checks", `{}`, true}, {"list_files", `{"extra":true}`, false}, {"run_checks", `{} {}`, false}, {"shell", `{}`, false},
		{"finish_review", finishJSON("lgtm"), true}, {"finish_review", finishJSON("LGTM"), true}, {"finish_review", finishJSON(" lgtm "), true}, {"finish_review", `{`, false}, {"finish_review", `{}`, false}, {"finish_review", finishJSON("unknown"), false},
		{"finish_review", strings.Replace(finishJSON("lgtm"), `"question":""`, `"question":"Why?"`, 1), false},
		{"finish_review", finishJSON("changes_required"), false}, {"finish_review", finishJSON("clarification_required"), false},
		{"finish_review", strings.Replace(finishJSON("clarification_required"), `"question":""`, `"question":"PROPOSED-Scope-v1:: Is this required?"`, 1), true},
		{"finish_review", `{"verdict":"changes_required","summary":"Bug","findings":[{"path":"main.go","line":1,"explanation":"Fails for an empty input"}]}`, true},
		{"finish_review", `{"verdict":"changes_required","summary":"Bug","findings":[{"path":"../x","line":0,"explanation":""}]}`, false},
	} {
		cmd, e := Plan(tt.name, json.RawMessage(tt.args))
		if (e == nil) != tt.valid {
			t.Fatalf("%s %s: %v", tt.name, tt.args, e)
		}
		if tt.valid && cmd == nil {
			t.Fatal("missing command")
		}
	}
	cmd, _ := Plan("finish_review", json.RawMessage(finishJSON("lgtm")))
	for _, checks := range []CheckEvidence{{}, {outputs: []string{"failed"}, failure: "failed"}} {
		if Conclude(cmd.(Finish), checks).Status() != "incomplete" {
			t.Fatal("accepted LGTM without successful checks")
		}
	}
	if Conclude(cmd.(Finish), CheckEvidence{outputs: []string{"passed"}}).Status() != "lgtm" {
		t.Fatal("valid approval rejected")
	}
	if Conclude(Finish{}, CheckEvidence{}).Status() != "incomplete" {
		t.Fatal("zero value approved")
	}
}

type commandFunc func(context.Context, string, string, ...string) ([]byte, error)

func (f commandFunc) Run(c context.Context, d, n string, a ...string) ([]byte, error) {
	return f(c, d, n, a...)
}
func defaultExec(_ context.Context, _ string, name string, args ...string) ([]byte, error) {
	key := name + " " + strings.Join(args, " ")
	switch {
	case key == "git remote get-url origin":
		return []byte("https://github.com/o/r.git\n"), nil
	case strings.HasPrefix(key, "git merge-base"):
		return []byte(base), nil
	case strings.HasPrefix(key, "git show"):
		return []byte("package example\n"), nil
	case strings.HasPrefix(key, "git ls-tree"):
		return []byte("main.go\n"), nil
	case strings.HasPrefix(key, "git diff"):
		return []byte("+func example() {}"), nil
	default:
		return []byte("ok"), nil
	}
}

type fakeModel struct {
	capErr, turnErr error
	replies         []Reply
	requests        [][]Message
}

func (m *fakeModel) Capabilities(context.Context, Policy) error { return m.capErr }
func (m *fakeModel) Turn(_ context.Context, _ Policy, msg []Message) (Reply, error) {
	m.requests = append(m.requests, append([]Message(nil), msg...))
	if m.turnErr != nil {
		return Reply{}, m.turnErr
	}
	if len(m.replies) == 0 {
		return Reply{Message: Message{Role: "assistant", Content: "Still reviewing"}}, nil
	}
	r := m.replies[0]
	m.replies = m.replies[1:]
	return r, nil
}
func reply(name, args string) Reply {
	var call ToolCall
	call.Function.Name = name
	call.Function.Arguments = json.RawMessage(args)
	return Reply{Model: "actual-local-model", Done: true, DoneReason: "stop", Message: Message{Role: "assistant", Thinking: "private continuation", ToolCalls: []ToolCall{call}}, PromptTokens: 10, OutputTokens: 20}
}

type prFunc func(context.Context, string, int) (PR, error)

func (f prFunc) PullRequest(ctx context.Context, repo string, n int) (PR, error) {
	return f(ctx, repo, n)
}
func runner(m *fakeModel) *Runner {
	return &Runner{Exec: commandFunc(defaultExec), GitHub: prFunc(func(context.Context, string, int) (PR, error) { return pr(), nil }), Fetch: func(context.Context, string, string) error { return nil }, Model: m, Now: func() time.Time { return time.Unix(100, 0).UTC() }}
}

func workspace() Workspace { return Workspace{path: "scratch", mergeBase: base, pr: pr()} }

func TestPRAndWorkspaceValidation(t *testing.T) {
	r := runner(&fakeModel{})
	ctx := context.Background()
	got, e := r.PR(ctx, "o/r", 1)
	if e != nil || got.Head != head {
		t.Fatal(got, e)
	}
	if _, e := r.PR(ctx, "--evil", 0); e == nil {
		t.Fatal("bad target accepted")
	}
	for _, data := range []string{`{`, `{"number":1}`, `{"number":1,"state":"CLOSED"}`} {
		r.GitHub = prFunc(func(context.Context, string, int) (PR, error) {
			var p PR
			err := json.Unmarshal([]byte(data), &p)
			return p, err
		})
		if _, e := r.PR(ctx, "o/r", 1); e == nil {
			t.Fatal("bad PR accepted")
		}
	}
	r.GitHub = prFunc(func(context.Context, string, int) (PR, error) { return PR{}, errors.New("offline") })
	if _, e := r.PR(ctx, "o/r", 1); e == nil {
		t.Fatal("missing provider error")
	}
	r.Exec = commandFunc(defaultExec)
	if w, e := r.Prepare(ctx, "o/r", pr(), "scratch"); e != nil || w.pr.Head != head || w.mergeBase != base {
		t.Fatal(w, e)
	}
	r.Fetch = func(context.Context, string, string) error { return errors.New("fetch failed") }
	if _, e := r.Prepare(ctx, "o/r", pr(), "scratch"); e == nil {
		t.Fatal("lost fetch failure")
	}
	r.Fetch = func(context.Context, string, string) error { return nil }
	for _, operation := range []string{"remote", "merge-base", "worktree"} {
		r.Exec = commandFunc(func(ctx context.Context, d, n string, a ...string) ([]byte, error) {
			if n == "git" && a[0] == operation {
				return nil, errors.New("failed")
			}
			return defaultExec(ctx, d, n, a...)
		})
		if _, e := r.Prepare(ctx, "o/r", pr(), "scratch"); e == nil {
			t.Fatalf("lost %s failure", operation)
		}
	}
	for _, tt := range []struct{ operation, body string }{{"remote", "https://github.com/other/repo"}, {"merge-base", "not-a-sha"}} {
		r.Exec = commandFunc(func(ctx context.Context, d, n string, a ...string) ([]byte, error) {
			if a[0] == tt.operation {
				return []byte(tt.body), nil
			}
			return defaultExec(ctx, d, n, a...)
		})
		if _, e := r.Prepare(ctx, "o/r", pr(), "scratch"); e == nil {
			t.Fatal("invalid workspace accepted")
		}
	}
}

func TestReviewToolsAndApproval(t *testing.T) {
	m := &fakeModel{replies: []Reply{reply("list_files", `{}`), reply("read_file", `{"path":"main.go"}`), reply("run_checks", `{}`), reply("run_checks", `{}`), reply("finish_review", finishJSON("lgtm"))}}
	r := runner(m)
	checkCalls := 0
	r.Exec = commandFunc(func(c context.Context, d, n string, a ...string) ([]byte, error) {
		if n == "make" {
			checkCalls++
		}
		return defaultExec(c, d, n, a...)
	})
	checkpoints := 0
	got := r.Review(context.Background(), policy(t), workspace(), []byte(policyJSON), func(report Report, msg []Message) error { checkpoints++; return nil })
	if got.Status != "lgtm" || len(got.Attempts) != 5 || checkCalls != 1 || checkpoints != 9 || got.EffectiveEffort != nil || got.RequestedEffort != "medium" {
		t.Fatalf("bad report: %+v; checkpoints %d", got, checkpoints)
	}
	if m.requests[1][2].Thinking != "private continuation" || m.requests[1][3].Role != "tool" {
		t.Fatal("continuation data lost")
	}
}

func TestReviewFailureOutcomes(t *testing.T) {
	ctx := context.Background()
	p := policy(t)
	noop := func(Report, []Message) error { return nil }
	for _, m := range []*fakeModel{{capErr: errors.New("unsupported")}, {turnErr: errors.New("timeout")}, {}, {replies: []Reply{reply("finish_review", finishJSON("lgtm"))}}} {
		if got := runner(m).Review(ctx, p, workspace(), nil, noop); got.Status != "incomplete" {
			t.Fatal(got)
		}
	}
	if got := runner(&fakeModel{}).Review(ctx, Policy{}, Workspace{}, nil, noop); got.Status != "incomplete" {
		t.Fatal(got)
	}
	for _, output := range []string{"error", strings.Repeat("x", 160001)} {
		r := runner(&fakeModel{})
		r.Exec = commandFunc(func(context.Context, string, string, ...string) ([]byte, error) {
			if output == "error" {
				return nil, errors.New(output)
			}
			return []byte(output), nil
		})
		if got := r.Review(ctx, p, workspace(), nil, noop); got.Status != "incomplete" {
			t.Fatal(got)
		}
	}
	for _, failAt := range []int{1, 2} {
		calls := 0
		r := runner(&fakeModel{replies: []Reply{reply("list_files", `{}`)}})
		got := r.Review(ctx, p, workspace(), nil, func(Report, []Message) error {
			calls++
			if calls == failAt {
				return errors.New("disk full")
			}
			return nil
		})
		if !strings.Contains(got.Summary, "persist review") {
			t.Fatal(got)
		}
	}
	for _, tool := range []string{"read_file", "list_files", "run_checks", "unknown"} {
		args := `{}`
		if tool == "read_file" {
			args = `{"path":"main.go"}`
		}
		r := runner(&fakeModel{replies: []Reply{reply(tool, args)}})
		r.Exec = commandFunc(func(c context.Context, d, n string, a ...string) ([]byte, error) {
			if n == "make" || a[0] == "show" || a[0] == "ls-tree" {
				return nil, errors.New("tool failed")
			}
			return defaultExec(c, d, n, a...)
		})
		if got := r.Review(ctx, p, workspace(), nil, noop); got.Status != "incomplete" {
			t.Fatal(got)
		}
	}
	r := runner(&fakeModel{replies: []Reply{reply("list_files", `{}`)}})
	r.Exec = commandFunc(func(c context.Context, d, n string, a ...string) ([]byte, error) {
		if a[0] == "ls-tree" {
			return []byte(strings.Repeat("x", 64001)), nil
		}
		return defaultExec(c, d, n, a...)
	})
	if got := r.Review(ctx, p, workspace(), nil, noop); !strings.Contains(got.Summary, "context limit") {
		t.Fatal(got)
	}
}

func TestFindingLocations(t *testing.T) {
	for _, line := range []int{1, 99} {
		args := fmt.Sprintf(`{"verdict":"changes_required","summary":"Bug","findings":[{"path":"main.go","line":%d,"explanation":"Fails for empty input"}]}`, line)
		got := runner(&fakeModel{replies: []Reply{reply("finish_review", args)}}).Review(context.Background(), policy(t), workspace(), nil, func(Report, []Message) error { return nil })
		if line == 1 && got.Status != "changes_required" || line == 99 && got.Status != "incomplete" {
			t.Fatal(got)
		}
	}
}

type httpFunc func(*http.Request) (*http.Response, error)

func (f httpFunc) Do(r *http.Request) (*http.Response, error) { return f(r) }
func httpResponse(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body))}
}
func TestOllamaProtocol(t *testing.T) {
	p := policy(t)
	ctx := context.Background()
	o := Ollama{HTTP: httpFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != "POST" || req.Header.Get("Content-Type") != "application/json" {
			t.Fatal("bad request")
		}
		if req.URL.Path == "/api/show" {
			return httpResponse(200, testModelInfo), nil
		}
		var body map[string]json.RawMessage
		if e := json.NewDecoder(req.Body).Decode(&body); e != nil {
			t.Fatal(e)
		}
		if string(body["think"]) != `"medium"` || string(body["stream"]) != "false" || !bytes.Contains(body["tools"], []byte("finish_review")) {
			t.Fatal("model policy not transmitted")
		}
		return httpResponse(200, `{"model":"actual-model","done":true,"done_reason":"stop","message":{"role":"assistant","content":"ok"}}`), nil
	})}
	if e := o.Capabilities(ctx, p); e != nil {
		t.Fatal(e)
	}
	if result, e := o.Turn(ctx, p, []Message{{Role: "user", Content: "Review"}}); e != nil || result.Model != "actual-model" {
		t.Fatal(result, e)
	}
	for _, body := range []string{`{"capabilities":[]}`, `{"capabilities":["tools","thinking"],"remote_host":"https://remote"}`} {
		o.HTTP = httpFunc(func(*http.Request) (*http.Response, error) { return httpResponse(200, body), nil })
		if e := o.Capabilities(ctx, p); e == nil {
			t.Fatal("unsupported model accepted")
		}
	}
	for _, body := range []string{`{}`, `{`, strings.Repeat("x", (4<<20)+1)} {
		o.HTTP = httpFunc(func(*http.Request) (*http.Response, error) { return httpResponse(200, body), nil })
		if _, e := o.Turn(ctx, p, nil); e == nil {
			t.Fatal("bad reply accepted")
		}
	}
	o.HTTP = httpFunc(func(*http.Request) (*http.Response, error) { return httpResponse(500, "private details"), nil })
	if _, e := o.Turn(ctx, p, nil); e == nil || strings.Contains(e.Error(), "private details") {
		t.Fatal(e)
	}
	o.HTTP = httpFunc(func(*http.Request) (*http.Response, error) { return nil, context.Canceled })
	if _, e := o.Turn(ctx, p, nil); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if _, e := (Ollama{}).Turn(ctx, p, nil); e == nil {
		t.Fatal("nil client accepted")
	}
}

func TestSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "checkpoint.json")
	if e := Save(path, map[string]string{"status": "incomplete"}); e != nil {
		t.Fatal(e)
	}
	if e := Save(path, map[string]string{"status": "lgtm"}); e != nil {
		t.Fatal(e)
	}
	data, e := os.ReadFile(path)
	if e != nil || !bytes.Contains(data, []byte("lgtm")) {
		t.Fatal(string(data), e)
	}
	if e := Save(path, make(chan int)); e == nil {
		t.Fatal("invalid value accepted")
	}
	if e := Save(filepath.Join(path, "missing"), "value"); e == nil {
		t.Fatal("lost file error")
	}
	dir := filepath.Join(t.TempDir(), "directory")
	if e := os.Mkdir(dir, 0700); e != nil {
		t.Fatal(e)
	}
	if e := Save(dir, "value"); e == nil {
		t.Fatal("lost rename error")
	}
}

func TestReviewTurnAllowance(t *testing.T) {
	for _, turns := range []int{0, 1, 100, 200, 1000} {
		data := strings.Replace(policyJSON, `"max_turns":5`, fmt.Sprintf(`"max_turns":%d`, turns), 1)
		if p, err := ParsePolicy([]byte(data)); err != nil || p.config.MaxTurns != turns {
			t.Fatal(turns, err)
		}
	}
	for _, turns := range []int{-1, -100} {
		data := strings.Replace(policyJSON, `"max_turns":5`, fmt.Sprintf(`"max_turns":%d`, turns), 1)
		if _, err := ParsePolicy([]byte(data)); err == nil {
			t.Fatal("invalid turn allowance accepted", turns)
		}
	}
	p, err := ParsePolicy([]byte(strings.Replace(policyJSON, `"max_turns":5`, `"max_turns":100`, 1)))
	if err != nil {
		t.Fatal(err)
	}
	var replies []Reply
	for i := 0; i < 35; i++ {
		replies = append(replies, reply("run_checks", `{"cmd":"not authorized"}`))
	}
	replies = append(replies, reply("run_checks", `{}`), reply("finish_review", finishJSON("lgtm")))
	m := &fakeModel{replies: replies}
	r := runner(m)
	checkCalls := 0
	r.Exec = commandFunc(func(c context.Context, d, n string, a ...string) ([]byte, error) {
		if n == "make" {
			checkCalls++
		}
		return defaultExec(c, d, n, a...)
	})
	got := r.Review(context.Background(), p, workspace(), nil, func(Report, []Message) error { return nil })
	if got.Status != "lgtm" || len(got.Attempts) != 37 || checkCalls != 1 {
		t.Fatalf("extended allowance failed: status=%s turns=%d checks=%d", got.Status, len(got.Attempts), checkCalls)
	}
}
