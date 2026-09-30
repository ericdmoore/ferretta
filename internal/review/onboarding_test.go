package review

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ericdmoore/ferretta/internal/github"
)

const qwenInfo = `{"capabilities":["tools","thinking"],"details":{"family":"qwen3"},"model_info":{"general.architecture":"qwen3","qwen3.context_length":32768}}`

func qwenPolicy(t *testing.T) Policy {
	t.Helper()
	data := strings.Replace(policyJSON, `"effort":"medium"`, `"thinking":"enabled","context_tokens":16384`, 1)
	p, err := ParsePolicy([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestThinkingAndContextCompatibility(t *testing.T) {
	p := qwenPolicy(t)
	for _, tt := range []struct {
		body string
		p    Policy
		ok   bool
	}{
		{qwenInfo, p, true}, {qwenInfo, policy(t), false}, {testModelInfo, policy(t), true}, {testModelInfo, p, false},
		{strings.Replace(qwenInfo, `"details":{"family":"qwen3"}`, `"thinking":{"values":[true,false]}`, 1), p, true},
		{strings.Replace(testModelInfo, `"details":{"family":"gptoss"}`, `"thinking":{"values":["low","medium","high"]}`, 1), policy(t), true},
		{strings.Replace(qwenInfo, `"details":{"family":"qwen3"}`, `"thinking":{"values":[false,"unsupported"]}`, 1), p, false},
		{strings.Replace(qwenInfo, `"family":"qwen3"`, `"family":"unknown"`, 1), p, false},
		{strings.Replace(qwenInfo, "32768", "8192", 1), p, false},
		{strings.Replace(qwenInfo, `"general.architecture":"qwen3"`, `"other":"qwen3"`, 1), p, false},
		{strings.Replace(qwenInfo, `"qwen3.context_length":32768`, `"qwen3.context_length":"unknown"`, 1), p, false},
	} {
		var info ModelInfo
		if err := json.Unmarshal([]byte(tt.body), &info); err != nil {
			t.Fatal(err)
		}
		if err := info.validate(tt.p); (err == nil) != tt.ok {
			t.Fatal(tt.body, err)
		}
	}
	for _, value := range []string{`"thinking":"enabled","effort":"medium"`, `"thinking":"disabled"`, `"context_tokens":4096,"effort":"medium"`, `"context_tokens":999999,"effort":"medium"`} {
		if _, err := ParsePolicy([]byte(strings.Replace(policyJSON, `"effort":"medium"`, value, 1))); err == nil {
			t.Fatal("invalid settings accepted", value)
		}
	}
	calls := 0
	o := Ollama{HTTP: httpFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["think"] != true || body["options"].(map[string]any)["num_ctx"] != float64(16384) {
			t.Fatal("boolean thinking/context not transmitted", body)
		}
		return httpResponse(200, `{"model":"qwen3:4b-thinking","done":true,"message":{"role":"assistant"}}`), nil
	})}
	if _, err := o.Turn(context.Background(), p, []Message{{Role: "user", Content: "small input"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := o.Turn(context.Background(), p, []Message{{Role: "user", Content: strings.Repeat("x", 16384)}}); err == nil || calls != 1 {
		t.Fatal("oversized context dispatched")
	}
	r := runner(&fakeModel{})
	r.Exec = commandFunc(func(context.Context, string, string, ...string) ([]byte, error) {
		return []byte(strings.Repeat("x", 15000)), nil
	})
	if got := r.Review(context.Background(), p, workspace(), nil, func(Report, []Message) error { return nil }); !strings.Contains(got.Summary, "context limit") || got.RequestedThinking != "enabled" || got.RequestedEffort != "" {
		t.Fatal(got)
	}
	if _, err := o.Info(context.Background(), "https://external.example", "model"); err == nil {
		t.Fatal("nonlocal info request accepted")
	}
}

func TestSetupSmallModelAndSuggestions(t *testing.T) {
	t.Chdir(t.TempDir())
	if suggestCheck() != "" {
		t.Fatal("invented check")
	}
	if err := os.WriteFile("go.mod", []byte("module example"), 0600); err != nil {
		t.Fatal(err)
	}
	if suggestCheck() != `["go","test","./..."]` {
		t.Fatal("Go check missing")
	}
	s := setupFixture(t)
	base := s.HTTP
	s.HTTP = httpFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/show" {
			return httpResponse(200, qwenInfo), nil
		}
		return base.Do(r)
	})
	var out bytes.Buffer
	if code := s.Run(context.Background(), nil, strings.NewReader("1\n\ny\n"), &out, io.Discard); code != 0 {
		t.Fatal(code, out.String())
	}
	data, err := os.ReadFile(".ferretta/review.json")
	if err != nil {
		t.Fatal(err)
	}
	p, err := ParsePolicy(data)
	if err != nil || p.config.Thinking != "enabled" || p.config.Effort != "" || p.config.ContextTokens != 16384 {
		t.Fatal(string(data), err)
	}
	for _, args := range [][]string{{"--repo", "bad"}, {"--effort", "medium", "--thinking", "enabled"}, {"--thinking", "wrong", "--policy", "other"}} {
		if s.Run(context.Background(), args, strings.NewReader("1\n\ny\n"), io.Discard, io.Discard) != 1 {
			t.Fatal("bad setup accepted")
		}
	}
	for _, body := range []string{`{"capabilities":["tools","thinking"]}`, `bad`} {
		s.HTTP = httpFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path == "/api/show" {
				return httpResponse(200, body), nil
			}
			return base.Do(r)
		})
		args := append(append([]string{}, unattended...), "--policy", "other")
		if s.Run(context.Background(), args, nil, io.Discard, io.Discard) != 1 {
			t.Fatal("unknown controls accepted")
		}
	}
}

func doctorCLI(t *testing.T) CLI {
	s := setupFixture(t)
	s.Exec = commandFunc(func(_ context.Context, _ string, name string, args ...string) ([]byte, error) {
		if name != "git" {
			t.Fatal("doctor attempted tool execution")
		}
		switch strings.Join(args, " ") {
		case "--version":
			return []byte("git version"), nil
		case "remote get-url origin":
			return []byte("https://github.com/o/r.git"), nil
		}
		t.Fatal("unexpected process", args)
		return nil, nil
	})
	return CLI{Setup: s}
}
func TestDoctorReadinessAndFailures(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("policy.json", []byte(policyJSON), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"--repo", "o/r", "--policy", "policy.json"}
	for _, mode := range []string{"ready", "git", "auth", "remote", "missing", "invalid", "model", "cancel", "write"} {
		t.Run(mode, func(t *testing.T) {
			c := doctorCLI(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			a := append([]string{}, args...)
			expected := 2
			out := &bytes.Buffer{}
			var output io.Writer = out
			switch mode {
			case "ready":
				expected = 0
			case "git":
				c.Setup.Exec = commandFunc(func(context.Context, string, string, ...string) ([]byte, error) {
					return nil, errors.New("missing git")
				})
			case "auth":
				c.Setup.Auth = func(context.Context, string) (github.Identity, error) { return github.Identity{}, errors.New("no app") }
			case "remote":
				c.Setup.Exec = commandFunc(func(context.Context, string, string, ...string) ([]byte, error) { return []byte("other"), nil })
			case "missing":
				a[3] = "missing.json"
			case "invalid":
				if err := os.WriteFile("invalid.json", []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
				a[3] = "invalid.json"
			case "model":
				c.Setup.HTTP = httpFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("offline") })
			case "cancel":
				cancel()
				expected = 1
			case "write":
				output = failingIO{}
				expected = 1
			}
			if code := c.Doctor(ctx, a, output, io.Discard); code != expected {
				t.Fatal(code, out.String())
			}
			if mode == "ready" && (!strings.Contains(out.String(), "Ready for a bounded advisory review") || !strings.Contains(out.String(), "not executed")) {
				t.Fatal(out.String())
			}
		})
	}
	for _, args := range [][]string{nil, {"--bad"}, {"extra"}, {"--repo", "bad"}} {
		if doctorCLI(t).Doctor(context.Background(), args, io.Discard, io.Discard) == 0 {
			t.Fatal("bad doctor args accepted")
		}
	}
	if (CLI{}).Doctor(context.Background(), nil, io.Discard, io.Discard) != 1 {
		t.Fatal("missing adapters")
	}
}

func TestReadableReportsAndDemo(t *testing.T) {
	for _, outcome := range []string{"changes", "question", "lgtm"} {
		var out bytes.Buffer
		if (CLI{}).Demo(context.Background(), []string{"--outcome", outcome}, &out, io.Discard) != 0 {
			t.Fatal("demo failed")
		}
		for _, text := range []string{"ILLUSTRATIVE SAMPLE", "fictional", "Next:", "not reported"} {
			if !strings.Contains(out.String(), text) {
				t.Fatal("missing honest example label", out.String())
			}
		}
	}
	for _, args := range [][]string{{"--bad"}, {"extra"}, {"--outcome", "unknown"}} {
		if (CLI{}).Demo(context.Background(), args, io.Discard, io.Discard) != 1 {
			t.Fatal("bad demo args")
		}
	}
	if (CLI{}).Demo(context.Background(), nil, failingIO{}, io.Discard) != 1 {
		t.Fatal("write failure")
	}
	if (CLI{}).Demo(context.Background(), nil, &failAfterWriter{limit: 2}, io.Discard) != 1 {
		t.Fatal("report failure")
	}
	effort := "medium"
	r := Report{Status: "incomplete", Summary: "Limit reached\x1b", EffectiveEffort: &effort, StartedAt: time.Unix(100, 0), FinishedAt: time.Unix(102, 0), Attempts: []Reply{{Model: "actual", PromptTokens: 10, OutputTokens: 20, Message: Message{Thinking: "private reasoning"}}}}
	var out bytes.Buffer
	if err := PrintReport(&out, r, "text"); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"30", "2s", "Provider-reported effort: medium", "Next:"} {
		if !strings.Contains(out.String(), text) {
			t.Fatal(out.String())
		}
	}
	if strings.Contains(out.String(), "private reasoning") || strings.Contains(out.String(), "\x1b") || strings.Contains(out.String(), "Effective reasoning effort: not reported") {
		t.Fatal("leaked private details or contradictory provenance")
	}
	r.Attempts[0].Message = Message{Role: "assistant"}
	out.Reset()
	if err := PrintReport(&out, r, "json"); err != nil || !json.Valid(out.Bytes()) {
		t.Fatal(err)
	}
}

func probeModel() *fakeModel {
	return &fakeModel{replies: []Reply{reply("list_files", `{}`), reply("read_file", `{"path":"probe.txt"}`)}}
}

func TestBoundedToolProbe(t *testing.T) {
	if replies, err := Probe(context.Background(), probeModel(), policy(t)); err != nil || len(replies) != 2 {
		t.Fatal(replies, err)
	}
	for _, m := range []*fakeModel{
		{capErr: errors.New("no tools")},
		{replies: []Reply{{Message: Message{Role: "assistant", Content: "no tool"}}}},
		{replies: []Reply{reply("wrong", `{}`)}},
		{replies: []Reply{reply("read_file", `{"path":"probe.txt"}`)}},
		{replies: []Reply{reply("list_files", `{}`), reply("read_file", `{"path":"wrong.txt"}`)}},
		{replies: []Reply{reply("list_files", `{}`), reply("list_files", `{}`)}},
	} {
		if _, err := Probe(context.Background(), m, policy(t)); err == nil {
			t.Fatal("invalid protocol accepted")
		}
	}
	if _, err := Probe(context.Background(), nil, policy(t)); err == nil {
		t.Fatal("nil model")
	}
	m := probeModel()
	m.turnErr = errors.New("provider failed")
	if _, err := Probe(context.Background(), m, policy(t)); err == nil {
		t.Fatal("failure lost")
	}
	t.Chdir(t.TempDir())
	if err := os.WriteFile("policy.json", []byte(policyJSON), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"--policy", "policy.json"}
	if (CLI{Runner: Runner{Model: probeModel()}}).Probe(context.Background(), args, io.Discard, io.Discard) != 0 {
		t.Fatal("probe command")
	}
	if (CLI{Runner: Runner{Model: probeModel()}}).Probe(context.Background(), args, failingIO{}, io.Discard) != 1 {
		t.Fatal("output failure lost")
	}
	if (CLI{}).Probe(context.Background(), args, io.Discard, io.Discard) != 1 {
		t.Fatal("missing model")
	}
	for _, a := range [][]string{nil, {"--unknown"}, {"extra"}} {
		if (CLI{}).Probe(context.Background(), a, io.Discard, io.Discard) != 1 {
			t.Fatal("bad probe args")
		}
	}
	if err := os.WriteFile("policy.json", []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if (CLI{}).Probe(context.Background(), args, io.Discard, io.Discard) != 1 {
		t.Fatal("invalid policy")
	}
}

func TestDiscoveryRequiresClient(t *testing.T) {
	if _, err := discover(context.Background(), nil, probe{}); err == nil {
		t.Fatal("missing inventory client accepted")
	}
}

func TestOllamaIncompleteDiagnostics(t *testing.T) {
	o := Ollama{HTTP: httpFunc(func(*http.Request) (*http.Response, error) {
		return httpResponse(200, `{"model":"actual","done":true,"done_reason":"length","eval_count":4096,"message":{"role":"assistant","thinking":"private continuation"}}`), nil
	})}
	_, err := o.Turn(context.Background(), policy(t), nil)
	if err == nil || !strings.Contains(err.Error(), `reason="length"`) || !strings.Contains(err.Error(), "output_tokens=4096") || strings.Contains(err.Error(), "private continuation") {
		t.Fatal("incomplete diagnostics missing or exposed private continuation", err)
	}
}

func TestInvalidInventoryURLDoesNotReachTransport(t *testing.T) {
	client := httpFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid URL reached transport")
		return nil, nil
	})
	if _, err := discover(context.Background(), client, probe{endpoint: "http://[invalid", path: "/api/tags"}); err == nil {
		t.Fatal("invalid inventory URL accepted")
	}
}
