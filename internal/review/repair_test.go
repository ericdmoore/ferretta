package review

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func repairPolicyJSON() string {
	return `{"model_policy":` + strings.Replace(policyJSON, `"max_turns":5`, `"max_turns":0`, 1) + `,"max_cycles":2,"formatter":["gofmt","-w","main.go"],"protected_paths":["lint.conf"]}`
}
func repairPolicy(t *testing.T) RepairPolicy {
	t.Helper()
	p, e := ParseRepairPolicy([]byte(repairPolicyJSON()))
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func repairPR() PR { p := pr(); p.HeadRepository = "o/r"; p.HeadRef = "feature/fix"; return p }

const changesJSON = `{"verdict":"changes_required","summary":"Fails for empty input","findings":[{"path":"main.go","line":1,"explanation":"The result is wrong"}]}`

var candidateSHA = strings.Repeat("c", 40)
var treeSHA = strings.Repeat("d", 40)

type fakeRepairGit struct {
	candidates, pushes    int
	candidateErr, pushErr error
	sha                   string
	onPush                func()
}

func (g *fakeRepairGit) Candidate(context.Context, string, string, string, string, time.Time) (string, error) {
	g.candidates++
	if g.candidateErr != nil {
		return "", g.candidateErr
	}
	if g.sha != "" {
		return g.sha, nil
	}
	return candidateSHA, nil
}
func (g *fakeRepairGit) Push(context.Context, string, string, string, string) error {
	g.pushes++
	if g.onPush != nil {
		g.onPush()
	}
	return g.pushErr
}

type repairFixture struct {
	c         CLI
	api       *checkAPI
	model     *fakeModel
	store     *memoryWorkflow
	git       *fakeRepairGit
	pr        PR
	checks    int
	failCheck bool
	tree      string
}

func newRepairFixture(t *testing.T) *repairFixture {
	t.Helper()
	f := &repairFixture{api: &checkAPI{}, model: &fakeModel{}, store: &memoryWorkflow{}, git: &fakeRepairGit{}, pr: repairPR(), tree: treeSHA}
	f.c = checksCLI(f.model, f.api)
	p := repairPolicy(t)
	f.c.Repair = &p
	f.c.RepairRoot = filepath.Join(t.TempDir(), "repairs")
	f.c.RepairGit = f.git
	f.c.Runner.GitHub = prFunc(func(context.Context, string, int) (PR, error) { return f.pr, nil })
	f.c.Runner.Exec = commandFunc(func(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
		key := name + " " + strings.Join(args, " ")
		switch {
		case strings.HasPrefix(key, "git worktree add"):
			path := args[3]
			if err := os.MkdirAll(path, 0700); err != nil {
				return nil, err
			}
			return nil, os.WriteFile(filepath.Join(path, "main.go"), []byte("package main\n"), 0644)
		case key == "git write-tree":
			return []byte(f.tree), nil
		case key == "git diff --cached --name-only -z "+head+" --":
			return []byte("main.go\x00"), nil
		case key == "git ls-files --cached --others --exclude-standard -z":
			return []byte("main.go\x00"), nil
		case key == "git show --no-patch --format=%H%x20%P%x20%T "+candidateSHA:
			return []byte(candidateSHA + " " + head + " " + treeSHA), nil
		case key == "git rev-parse HEAD":
			return []byte(head), nil
		case key == "git rev-parse "+head+"^{tree}":
			return []byte(base), nil
		case name == "make":
			f.checks++
			if f.failCheck {
				return []byte("failed assertion"), errors.New("checks failed")
			}
			return []byte("all passed"), nil
		}
		return defaultExec(ctx, dir, name, args...)
	})
	f.git.onPush = func() { f.pr.Head = candidateSHA }
	return f
}
func (f *repairFixture) advance() (*workflowJob, error) {
	return f.c.advanceWorkflow(context.Background(), f.store, "o/r", f.pr, policyForRepair(), []byte(policyJSON), judgePolicyRaw(), []byte(evaluationPolicyJSON()), []string{"human"})
}
func policyForRepair() Policy { p, _ := ParsePolicy([]byte(policyJSON)); return p }
func judgePolicyRaw() EvaluationPolicy {
	p, _ := ParseEvaluationPolicy([]byte(evaluationPolicyJSON()))
	return p
}
func repairReplies() []Reply {
	return []Reply{reply("finish_review", changesJSON), reply("apply_patch", `{"path":"main.go","old_text":"package main","new_text":"package main // fixed"}`), reply("run_tests", `{}`), reply("finish_repair", `{"summary":"Fixed the documented defect; checks passed."}`)}
}

func TestBoundedRepairEndToEnd(t *testing.T) {
	f := newRepairFixture(t)
	f.model.replies = repairReplies()
	job, err := f.advance()
	if err != nil {
		t.Fatal(err)
	}
	r := job.Runs[0]
	if r.Phase != workflowDone || r.Repair.Delivery != Posted || r.Repair.Candidate != candidateSHA || f.git.pushes != 1 || r.Checks["repair"].Result.Conclusion != "success" {
		t.Fatalf("repair not published: %+v", r)
	}
	if r.Checks["evaluation"].Result.Conclusion != "cancelled" {
		t.Fatal("old revision judge left queued")
	}
	if !strings.Contains(r.Checks["repair"].Result.Output.Text, "candidate commit") || !strings.Contains(f.api.writes[0].Output.Text, "Optional repairer") {
		t.Fatal("repair not visible")
	}
	// New head receives new review, checks and judge. Old findings/LGTM not reused.
	f.model.replies = workflowReplies()
	job, err = f.advance()
	if err != nil {
		t.Fatal(err)
	}
	if len(job.Runs) != 2 || job.Runs[1].Session.Report.PR.Head != candidateSHA || job.Runs[1].Session.Report.Status != "lgtm" || f.checks != 2 {
		t.Fatal("new revision skipped evidence", job, f.checks)
	}
	if _, err = f.advance(); err != nil || f.git.pushes != 1 || len(f.model.requests) != 7 {
		t.Fatal("repeated delivery spent twice", err)
	}
}

func TestRepairPolicyAndTools(t *testing.T) {
	for _, s := range []string{`{`, repairPolicyJSON() + ` {}`, strings.Replace(repairPolicyJSON(), `"max_cycles":2`, `"max_cycles":0`, 1), strings.Replace(repairPolicyJSON(), `"timeout_seconds":60`, `"timeout_seconds":0`, 1), strings.Replace(repairPolicyJSON(), `"gofmt"`, `""`, 1), strings.Replace(repairPolicyJSON(), `"lint.conf"`, `"../outside"`, 1), strings.Replace(repairPolicyJSON(), `"ollama"`, `"remote"`, 1)} {
		if _, err := ParseRepairPolicy([]byte(s)); err == nil {
			t.Fatal("invalid policy accepted", s)
		}
	}
	p := repairPolicy(t)
	for _, tt := range []struct {
		name, args string
		valid      bool
	}{
		{"apply_patch", `{"path":"a.go","old_text":"old","new_text":"new"}`, true}, {"apply_patch", `{`, false}, {"apply_patch", `{"path":".Git","old_text":"old","new_text":"new"}`, false}, {"apply_patch", `{} {}`, false}, {"apply_patch", `{"path":"Makefile","old_text":"x","new_text":"y"}`, false}, {"apply_patch", `{"path":"lint.conf","old_text":"x","new_text":"y"}`, false}, {"apply_patch", `{"path":"a","old_text":"x","new_text":"x"}`, false}, {"run_tests", `{}`, true}, {"run_formatter", `{}`, true}, {"run_formatter", `{"cmd":"sudo search"}`, false}, {"finish_repair", `{`, false}, {"finish_repair", `{}`, false}, {"finish_repair", `{"summary":"Addressed findings"}`, true}, {"read_file", `{"path":"a.go"}`, true}, {"shell", `{}`, false}, {"finish_review", finishJSON("lgtm"), false},
	} {
		cmd, err := planRepair(tt.name, json.RawMessage(tt.args), p)
		if (err == nil) != tt.valid {
			t.Fatal(tt, err)
		}
		if cmd != nil {
			cmd.command()
		}
	}
	p.formatter = nil
	if _, err := planRepair("run_formatter", json.RawMessage(`{}`), p); err == nil {
		t.Fatal("unauthorized formatter")
	}
	if !json.Valid(repairTools()) {
		t.Fatal("invalid tools")
	}
}

func TestRepairChecksAreInvalidated(t *testing.T) {
	f := newRepairFixture(t)
	f.model.replies = []Reply{reply("finish_review", changesJSON), reply("run_tests", `{}`), reply("apply_patch", `{"path":"main.go","old_text":"package main","new_text":"package main // repaired"}`), reply("finish_repair", `{"summary":"Should reject stale checks"}`), reply("run_formatter", `{}`), reply("run_tests", `{}`), reply("finish_repair", `{"summary":"Checks now apply to edited tree"}`)}
	job, err := f.advance()
	if err != nil {
		t.Fatal(err)
	}
	if job.Runs[0].Repair.Delivery != Posted || f.checks != 2 {
		t.Fatal("check invalidation missing")
	}
	evidence, _ := json.Marshal(job.Runs[0].Repair.Session.Messages)
	if !strings.Contains(string(evidence), "Checks must pass on the exact repaired tree") {
		t.Fatal("accepted stale evidence")
	}
}

func TestRepairHumanWaitAndCarry(t *testing.T) {
	f := newRepairFixture(t)
	f.model.replies = []Reply{reply("finish_review", changesJSON), reply("request_intent_confirmation", askJSON)}
	job, err := f.advance()
	if err != nil {
		t.Fatal(err)
	}
	if job.Runs[0].Phase != workflowRepairWaiting {
		t.Fatal("not waiting")
	}
	used := job.RepairNanos
	if _, err = f.advance(); err != nil {
		t.Fatal(err)
	}
	if len(f.model.requests) != 2 || f.api.posts != 1 {
		t.Fatal("human wait spent or duplicated")
	}
	proposal := job.Runs[0].Repair.Session.Report.Proposals[0]
	f.api.comments = append(f.api.comments, commentFixture(5, fmt.Sprintf("CONFIRMED-%s-v1:: A", proposal.Topic), "human", "User"))
	f.model.replies = repairReplies()[1:]
	job, err = f.advance()
	if err != nil {
		t.Fatal(err)
	}
	if job.RepairNanos != used || job.Runs[0].Repair.Delivery != Posted {
		t.Fatal("wait incorrectly charged", job.RepairNanos, used)
	} // Injected fixed clock.
	f.model.replies = workflowReplies()
	job, err = f.advance()
	if err != nil {
		t.Fatal(err)
	}
	if len(job.Runs[1].Session.Report.Proposals) != 1 {
		t.Fatal("confirmed repair intent dropped")
	}
	found := false
	for _, m := range f.model.requests[len(f.model.requests)-3] {
		if strings.Contains(m.Content, "CONFIRMED") {
			found = true
		}
	}
	if !found {
		t.Fatal("new reviewer missed confirmed intent")
	}
}
