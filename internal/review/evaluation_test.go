package review

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func evaluationPolicyJSON() string {
	return strings.Replace(policyJSON, `,"checks":[["make","check"]]`, "", 1)
}
func judgePolicy(t *testing.T) EvaluationPolicy {
	t.Helper()
	p, err := ParseEvaluationPolicy([]byte(evaluationPolicyJSON()))
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func reportFixture() Report {
	return Report{PR: pr(), MergeBase: base, Status: "lgtm", Summary: "Checks and implementation support the requested behavior", Checks: []string{"make check: passed"}, PolicySHA256: strings.Repeat("c", 64), FinishedAt: time.Unix(200, 0), RequestedModel: "AUTHOR_IDENTITY", Attempts: []Reply{reply("finish_review", finishJSON("lgtm"))}}
}
func snapshotFixture(t *testing.T) Snapshot {
	t.Helper()
	s, err := SnapshotReview(reportFixture())
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func assessmentFixture() Assessment {
	n := 2
	g := DimensionGrade{Score: &n, Explanation: "The published claim is supported within its stated scope.", Evidence: []string{"review"}}
	r := RoleGrades{g, g, g, g, g, g}
	return Assessment{Summary: "Adequate review; this is an assessment rather than verified correctness.", Worker: r, Oversight: r}
}
func assessmentJSON() string { data, _ := json.Marshal(assessmentFixture()); return string(data) }

func TestEvaluationBoundaries(t *testing.T) {
	if _, err := ParseEvaluationPolicy([]byte(policyJSON)); err == nil {
		t.Fatal("judge accepted executable checks")
	}
	if _, err := ParseEvaluationPolicy([]byte(`{}`)); err == nil {
		t.Fatal("invalid route accepted")
	}
	p := judgePolicy(t)
	for _, name := range []string{"finish_review", "request_intent_confirmation", "run_checks"} {
		if bytes.Contains(p.route.tools(), []byte(`"name":"`+name+`"`)) {
			t.Fatal("write capability advertised", name)
		}
	}
	for _, name := range []string{"finish_evaluation", "read_evidence", "search"} {
		if !bytes.Contains(p.route.tools(), []byte(name)) {
			t.Fatal("missing judge affordance", name)
		}
	}
	r := reportFixture()
	r.Proposals = []Proposal{{Request: requestFixture(), Body: "AUTHOR_IDENTITY"}}
	s, err := SnapshotReview(r)
	if err != nil {
		t.Fatal(err)
	}
	r.Checks[0] = "mutated"
	r.Proposals[0].Request.Question = "mutated"
	data, _ := json.Marshal(s.input)
	if bytes.Contains(data, []byte("AUTHOR_IDENTITY")) || bytes.Contains(data, []byte("private continuation")) || bytes.Contains(data, []byte("mutated")) {
		t.Fatal("snapshot leaks authorship or aliases mutable report")
	}
	for _, bad := range []Report{{}, func() Report { r := reportFixture(); r.Status = "awaiting_intent"; return r }(), func() Report { r := reportFixture(); r.PolicySHA256 = strings.Repeat("z", 64); return r }()} {
		if _, err := SnapshotReview(bad); err == nil {
			t.Fatal("unfinished input accepted")
		}
	}
	if elapsed(time.Unix(2, 0), time.Unix(1, 0)) != 0 {
		t.Fatal("negative usage")
	}
	for _, raw := range []string{`{`, `{}`, assessmentJSON() + ` {}`, strings.Replace(assessmentJSON(), `"summary":`, `"extra":1,"summary":`, 1)} {
		if _, err := parseAssessment([]byte(raw), map[string]string{"review": "evidence"}); err == nil {
			t.Fatal("invalid assessment accepted", raw)
		}
	}
	for _, mutate := range []func(*Assessment){
		func(a *Assessment) { a.Summary = "" },
		func(a *Assessment) { a.Worker.Correctness.Explanation = "" },
		func(a *Assessment) { a.Worker.Correctness.Score = nil },
		func(a *Assessment) { n := 4; a.Worker.Correctness.Score = &n },
		func(a *Assessment) { a.Worker.Correctness.NotAssessed = "not_applicable" },
		func(a *Assessment) { a.Worker.Correctness.Evidence = nil },
		func(a *Assessment) { a.Worker.Correctness.Evidence = []string{"fabricated"} },
		func(a *Assessment) { a.Worker.Correctness.Evidence = make([]string, 13) },
	} {
		a := assessmentFixture()
		mutate(&a)
		raw, _ := json.Marshal(a)
		if _, err := parseAssessment(raw, map[string]string{"review": "evidence"}); err == nil {
			t.Fatal("invalid grade accepted", a)
		}
	}
	a := assessmentFixture()
	a.Oversight.Correctness.Score = nil
	a.Oversight.Correctness.NotAssessed = "insufficient_evidence"
	raw, _ := json.Marshal(a)
	if _, err := parseAssessment(raw, map[string]string{"review": "evidence"}); err != nil {
		t.Fatal(err)
	}
}

func TestEvaluatorUsesEvidenceWithoutEffects(t *testing.T) {
	m := &fakeModel{replies: []Reply{reply("read_evidence", `{"id":"checks"}`), reply("read_diff", `{}`), reply("read_file", `{"path":"main.go"}`), reply("list_files", `{}`), reply("search", `{"query":"target"}`), reply("run_checks", `{}`), reply("finish_evaluation", assessmentJSON())}}
	r := runner(m)
	r.Searcher = searchFunc(func(_ context.Context, _ string, revision string, _ Search) (SearchPage, error) {
		if revision != head {
			t.Fatal("wrong revision")
		}
		return SearchPage{Revision: revision}, nil
	})
	r.Exec = commandFunc(func(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
		if name != "git" {
			t.Fatal("judge executed check")
		}
		return defaultExec(ctx, dir, name, args...)
	})
	p := judgePolicy(t)
	p.route.config.MaxTurns = 0
	stamp := time.Unix(100, 0)
	r.Now = func() time.Time { stamp = stamp.Add(time.Millisecond); return stamp }
	got := r.Evaluate(context.Background(), p, snapshotFixture(t), nil, "attempt", ".", func(Evaluation, []Message) error { return nil })
	if got.Assessment == nil || got.Failure != "" || got.Head != head || got.EffectiveEffort != nil || len(got.Usage) != 13 || len(got.ObservedModels) != 1 {
		t.Fatalf("%+v", got)
	}
	for _, request := range m.requests {
		if strings.Contains(request[1].Content, "AUTHOR_IDENTITY") || strings.Contains(request[1].Content, "private continuation") {
			t.Fatal("review transcript exposed")
		}
	}
	if got.Usage[11].Outcome != "failed" {
		t.Fatal("forbidden tool succeeded")
	}
	text := EvaluationMarkdown(got)
	if !strings.Contains(text, "Oversight decision") || !strings.Contains(text, "2/3") || strings.Contains(text, "private continuation") {
		t.Fatal(text)
	}
	got.Assessment.Worker.Correctness.Score = nil
	got.Assessment.Worker.Correctness.NotAssessed = "insufficient_evidence"
	got.Usage = append(got.Usage, UsageEvent{Kind: "model"})
	if !strings.Contains(EvaluationMarkdown(got), "not assessed") {
		t.Fatal("unknown became a score")
	}
	got.Assessment.Worker.Evidence.Evidence = []string{"tool-4", "tool-99"}
	got.Evidence["tool-99"] = strings.Repeat("x", 600) + "\nPRIVATE_RAW_TOOL_OUTPUT"
	text = EvaluationMarkdown(got)
	if !strings.Contains(text, "read_diff") || !strings.Contains(text, "Evidence index") || strings.Contains(text, "PRIVATE_RAW_TOOL_OUTPUT") || strings.Contains(text, strings.Repeat("x", 513)) {
		t.Fatal("evidence index exposes full bodies or omits source description", text)
	}
}

func TestEvaluationFailuresAreNotGrades(t *testing.T) {
	for _, mode := range []string{"capability", "model", "cancel", "context", "checkpoint-before", "checkpoint-reply", "checkpoint-tool", "oversized", "no-tools", "bad-grade", "multiple-finish", "zero"} {
		t.Run(mode, func(t *testing.T) {
			m := &fakeModel{replies: []Reply{reply("list_files", `{}`)}}
			r := runner(m)
			p := judgePolicy(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s := snapshotFixture(t)
			switch mode {
			case "capability":
				m.capErr = errors.New("unsupported")
			case "model":
				m.turnErr = errors.New("uncertain provider response")
			case "cancel":
				cancel()
			case "context":
				p.route.config.ContextTokens = 1
			case "oversized":
				r.Exec = commandFunc(func(context.Context, string, string, ...string) ([]byte, error) {
					return []byte(strings.Repeat("x", 64001)), nil
				})
			case "no-tools":
				m.replies = nil
			case "bad-grade":
				m.replies = []Reply{reply("finish_evaluation", `{}`)}
			case "multiple-finish":
				v := reply("finish_evaluation", assessmentJSON())
				v.Message.ToolCalls = append(v.Message.ToolCalls, v.Message.ToolCalls[0])
				m.replies = []Reply{v}
			case "zero":
				s = Snapshot{}
			}
			calls := 0
			got := r.Evaluate(ctx, p, s, nil, "attempt", ".", func(Evaluation, []Message) error {
				calls++
				if mode == "checkpoint-before" && calls == 1 || mode == "checkpoint-reply" && calls == 2 || mode == "checkpoint-tool" && calls == 3 {
					return errors.New("storage failed")
				}
				return nil
			})
			if got.Assessment != nil || got.Failure == "" {
				t.Fatal(got)
			}
			if !strings.Contains(EvaluationMarkdown(got), "Evaluation incomplete") {
				t.Fatal("failure hidden")
			}
		})
	}
	got := (Runner{}).Evaluate(context.Background(), EvaluationPolicy{}, Snapshot{}, nil, "", "", nil)
	if got.Assessment != nil {
		t.Fatal(got)
	}
}

func TestJudgeReadOnlyToolFailures(t *testing.T) {
	r := runner(&fakeModel{})
	for _, tt := range []struct{ name, args string }{{"read_evidence", "{"}, {"read_evidence", `{"id":"checks","extra":true}`}, {"read_evidence", `{"id":"checks"} {}`}, {"read_evidence", `{"id":"missing"}`}, {"read_file", `{"path":"../outside"}`}, {"search", `{"query":"x"}`}, {"finish_review", `{}`}} {
		if _, err := r.evaluationTool(context.Background(), workspace(), map[string]string{}, tt.name, json.RawMessage(tt.args)); err == nil {
			t.Fatal(tt)
		}
	}
	r.Searcher = searchFunc(func(context.Context, string, string, Search) (SearchPage, error) {
		return SearchPage{}, errors.New("search unavailable")
	})
	if _, err := r.evaluationTool(context.Background(), workspace(), nil, "search", json.RawMessage(`{"query":"x"}`)); err == nil {
		t.Fatal("lost search failure")
	}
}

func TestEvaluationCLI(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("judge.json", []byte(evaluationPolicyJSON()), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Save("report.json", reportFixture()); err != nil {
		t.Fatal(err)
	}
	args := []string{"--run", ".", "--policy", "judge.json"}
	m := &fakeModel{replies: []Reply{reply("finish_evaluation", assessmentJSON())}}
	c := CLI{Runner: *runner(m)}
	var out bytes.Buffer
	if code := c.Eval(context.Background(), args, &out, io.Discard); code != 0 {
		t.Fatal(code, out.String())
	}
	paths, _ := filepath.Glob("evaluation-*/scorecard.json")
	if len(paths) != 1 {
		t.Fatal(paths)
	}
	data, err := os.ReadFile(paths[0])
	if err != nil || bytes.Contains(data, []byte("private continuation")) {
		t.Fatal(err, string(data))
	}
	for _, bad := range [][]string{nil, {"--unknown"}, {"--run", ".", "--policy", "missing"}, {"--run", "missing", "--policy", "judge.json"}} {
		if code := c.Eval(context.Background(), bad, io.Discard, io.Discard); code != 1 {
			t.Fatal(bad, code)
		}
	}
	if code := c.Eval(context.Background(), args, io.Discard, io.Discard); code != 2 {
		t.Fatal("incomplete judge approved", code)
	}
	m.replies = []Reply{reply("finish_evaluation", assessmentJSON())}
	if code := c.Eval(context.Background(), args, failingIO{}, io.Discard); code != 1 {
		t.Fatal("output failure", code)
	}
	for i, bad := range []string{`{`, `{}`} {
		if i == 0 {
			_ = os.WriteFile("judge.json", []byte(bad), 0600)
		} else {
			_ = os.WriteFile("judge.json", []byte(evaluationPolicyJSON()), 0600)
			_ = os.WriteFile("report.json", []byte(bad), 0600)
		}
		if code := c.Eval(context.Background(), args, io.Discard, io.Discard); code != 1 {
			t.Fatal(fmt.Sprint(i, code))
		}
	}
	_ = Save("report.json", Report{})
	if code := c.Eval(context.Background(), args, io.Discard, io.Discard); code != 1 {
		t.Fatal("invalid report", code)
	}
}

func TestEvaluationStorageFailures(t *testing.T) {
	for _, mode := range []string{"malformed-report", "read-only-dir", "scorecard-write"} {
		t.Run(mode, func(t *testing.T) {
			t.Chdir(t.TempDir())
			_ = os.Mkdir("run", 0700)
			_ = os.WriteFile("judge.json", []byte(evaluationPolicyJSON()), 0600)
			_ = Save("run/report.json", reportFixture())
			r := runner(&fakeModel{replies: []Reply{reply("finish_evaluation", assessmentJSON())}})
			switch mode {
			case "malformed-report":
				_ = os.WriteFile("run/report.json", []byte("{"), 0600)
			case "read-only-dir":
				if os.Geteuid() == 0 {
					t.Skip("root bypasses permissions")
				}
				_ = os.Chmod("run", 0500)
				defer os.Chmod("run", 0700)
			case "scorecard-write":
				r.Now = func() time.Time {
					dirs, _ := filepath.Glob("run/evaluation-*")
					for _, dir := range dirs {
						_ = os.Mkdir(filepath.Join(dir, "scorecard.json"), 0700)
					}
					return time.Unix(1, 0)
				}
			}
			if code := (CLI{Runner: *r}).Eval(context.Background(), []string{"--run", "run", "--policy", "judge.json"}, io.Discard, io.Discard); code != 1 {
				t.Fatal(mode, code)
			}
		})
	}
}

func TestManualReviewEvaluationHook(t *testing.T) {
	for _, mode := range []string{"success", "judge-failure", "json", "missing-policy", "invalid-policy", "proposal-conflict"} {
		t.Run(mode, func(t *testing.T) {
			t.Chdir(t.TempDir())
			_ = os.WriteFile("review.json", []byte(policyJSON), 0600)
			_ = os.WriteFile("judge.json", []byte(evaluationPolicyJSON()), 0600)
			m := &fakeModel{replies: workflowReplies()}
			args := []string{"--repo", "o/r", "--pr", "1", "--policy", "review.json", "--eval-policy", "judge.json"}
			switch mode {
			case "judge-failure":
				m.replies = m.replies[:2]
			case "json":
				args = append(args, "--format", "json")
			case "missing-policy":
				_ = os.Remove("judge.json")
			case "invalid-policy":
				_ = os.WriteFile("judge.json", []byte("{}"), 0600)
			case "proposal-conflict":
				args = append(args, "--publish-proposals")
			}
			var out bytes.Buffer
			code := (CLI{Runner: *runner(m)}).Run(context.Background(), args, &out, io.Discard)
			if mode == "success" || mode == "json" {
				if code != 0 {
					t.Fatal(mode, code)
				}
			} else if code == 0 {
				t.Fatal("failure accepted", mode)
			}
			if mode == "json" {
				var report Report
				if err := json.Unmarshal(out.Bytes(), &report); err != nil {
					t.Fatal("evaluation corrupted JSON", err)
				}
			}
			if mode == "judge-failure" {
				paths, _ := filepath.Glob(".ferretta/runs/*/report.json")
				data, _ := os.ReadFile(paths[0])
				var r Report
				_ = json.Unmarshal(data, &r)
				if r.Status != "lgtm" {
					t.Fatal("judge failure changed review")
				}
			}
		})
	}
}
