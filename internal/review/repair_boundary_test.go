package review

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Direct transition tests exercise persisted external state and adapter failures
// without replaying expensive model work to reach each recovery condition.
func TestRepairStateBoundaries(t *testing.T) {
	for _, mode := range []string{"identity", "input", "unknown-phase", "pr-read", "stale", "human-read", "exhausted", "candidate-exhausted", "setup", "tree-changed", "candidate-failure", "invalid-candidate", "missing-candidate", "publish-read", "branch-changed", "fetch-failure", "invalid-delivery", "push-save"} {
		t.Run(mode, func(t *testing.T) {
			f := newRepairFixture(t)
			run := &workflowRun{ID: "run", Phase: workflowRepair, Session: managedSession{Session: Session{Report: Report{PR: f.pr}}}}
			id := hashBytes([]byte(run.ID + "/repair"))
			a := &repairAttempt{ID: id, Workspace: filepath.Join(f.c.RepairRoot, id), Tree: treeSHA, Candidate: candidateSHA, Delivery: Draft, CommitTime: time.Unix(1, 0), Session: managedSession{Repository: "o/r", Session: Session{Report: Report{PR: f.pr}}}}
			run.Repair = a
			job := &workflowJob{Repository: "o/r", PR: 1, AppID: 7, Runs: []*workflowRun{run}}
			save := func() error { return nil }
			expectError := true
			switch mode {
			case "identity":
				a.ID = "bad"
			case "input":
				a.Session.Repository = "other/repo"
			case "unknown-phase":
				run.Phase = "unknown"
			case "pr-read":
				f.c.Runner.GitHub = prFunc(func(context.Context, string, int) (PR, error) { return PR{}, errors.New("offline") })
			case "stale":
				f.pr.Base = head
				expectError = false
			case "human-read":
				run.Phase = workflowRepairWaiting
				a.Session.Report.Proposals = []Proposal{{Delivery: Draft}}
				f.api.readErr = errors.New("offline")
			case "exhausted":
				job.RepairNanos = int64(time.Minute)
				expectError = false
			case "candidate-exhausted":
				run.Phase = workflowCandidate
				job.RepairNanos = int64(time.Minute)
				expectError = false
			case "setup":
				f.c.Runner.Fetch = func(context.Context, string, string) error { return errors.New("fetch failed") }
				expectError = false
			case "tree-changed":
				run.Phase = workflowCandidate
				a.Tree = head
				expectError = false
			case "candidate-failure":
				run.Phase = workflowCandidate
				f.git.candidateErr = errors.New("disk failure")
			case "invalid-candidate":
				run.Phase = workflowCandidate
				f.git.sha = "not-sha"
			case "missing-candidate":
				run.Phase = workflowPublishRepair
				a.Candidate = ""
			case "publish-read":
				run.Phase = workflowPublishRepair
				f.c.Runner.GitHub = prFunc(func(context.Context, string, int) (PR, error) { return PR{}, errors.New("offline") })
			case "branch-changed":
				run.Phase = workflowPublishRepair
				f.pr.HeadRef = "other"
				expectError = false
			case "fetch-failure":
				run.Phase = workflowPublishRepair
				a.Delivery = Uncertain
				f.c.Runner.Fetch = func(context.Context, string, string) error { return errors.New("fetch failed") }
			case "invalid-delivery":
				run.Phase = workflowPublishRepair
				a.Delivery = "bad"
			case "push-save":
				run.Phase = workflowPublishRepair
				f.git.pushErr = errPushNotDispatched
				saves := 0
				save = func() error {
					saves++
					if saves == 2 {
						return errors.New("disk failure")
					}
					return nil
				}
			}
			err := f.c.advanceRepair(context.Background(), job, run, save)
			if (err != nil) != expectError {
				t.Fatal(mode, err)
			}
			if !expectError && run.Phase != workflowRepairResult {
				t.Fatal("failure not terminal", mode, run.Phase)
			}
			if f.git.pushes > 0 && mode != "push-save" {
				t.Fatal("invalid state dispatched push")
			}
		})
	}
}

func TestRepairWorkspaceRecoveryFailsClosed(t *testing.T) {
	for _, mode := range []string{"root", "workspace"} {
		f := newRepairFixture(t)
		id := hashBytes([]byte("id"))
		a := &repairAttempt{ID: id, Workspace: filepath.Join(f.c.RepairRoot, id), Session: managedSession{Repository: "o/r", Session: Session{Report: Report{PR: f.pr}}}}
		if mode == "root" {
			if err := os.WriteFile(f.c.RepairRoot, []byte("occupied"), 0600); err != nil {
				t.Fatal(err)
			}
		} else {
			a.Session.Messages = []Message{{Role: "system", Content: "resume"}}
			f.c.Runner.Exec = commandFunc(func(context.Context, string, string, ...string) ([]byte, error) { return nil, errors.New("missing") })
		}
		if err := f.c.executeRepair(context.Background(), a, f.c.Repair.route, *f.c.Repair, func(Report, []Message) error { return nil }); err == nil {
			t.Fatal("workspace failure hidden", mode)
		}
	}
}

func TestWatchRepairOptIn(t *testing.T) {
	for _, mode := range []string{"valid", "comments", "missing", "invalid", "root"} {
		t.Run(mode, func(t *testing.T) {
			f := newRepairFixture(t)
			dir := t.TempDir()
			review := filepath.Join(dir, "review.json")
			judge := filepath.Join(dir, "judge.json")
			repair := filepath.Join(dir, "repair.json")
			_ = os.WriteFile(review, []byte(policyJSON), 0600)
			_ = os.WriteFile(judge, []byte(evaluationPolicyJSON()), 0600)
			data := repairPolicyJSON()
			if mode == "invalid" {
				data = `{`
			}
			_ = os.WriteFile(repair, []byte(data), 0600)
			if mode == "missing" {
				repair += "-missing"
			}
			publication := "checks"
			if mode == "comments" {
				publication = "comments"
			}
			state := filepath.Join(dir, "state")
			if mode == "root" {
				state = "relative"
			}
			f.model.replies = workflowReplies()
			f.api.pulls = []PR{f.pr}
			code := f.c.Watch(context.Background(), []string{"--repo", "o/r", "--pr", "1", "--state", state, "--review-policy", review, "--judge-policy", judge, "--repair-policy", repair, "--humans", "human", "--publication", publication, "--once"}, io.Discard, io.Discard)
			if (code == 0) != (mode == "valid") {
				t.Fatal(mode, code)
			}
		})
	}
	f := newRepairFixture(t)
	f.c.RepairRoot = "relative"
	if _, err := f.advance(); err == nil {
		t.Fatal("relative repair workspace admitted")
	}
	f = newRepairFixture(t)
	f.pr.HeadRepository = "fork/repo"
	f.model.replies = []Reply{reply("finish_review", changesJSON)}
	f.api.rejectTitle = "Repair not admitted"
	if _, err := f.advance(); err == nil {
		t.Fatal("failed Check hidden")
	}
}

func TestRepairToolFailureRecovery(t *testing.T) {
	for _, mode := range []string{"patch", "formatter", "batch-finish", "no-changes", "original-read", "search", "files", "diff", "check-mutation", "check-tree-error", "prose"} {
		t.Run(mode, func(t *testing.T) {
			f := newRepairFixture(t)
			f.c.Repair.route.config.MaxTurns = 5
			tools := []Reply{reply("run_tests", `{}`), reply("finish_repair", `{"summary":"Done"}`)}
			original := f.c.Runner.Exec
			f.c.Runner.Exec = commandFunc(func(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
				key := name + " " + strings.Join(args, " ")
				if mode == "formatter" && name == "gofmt" {
					return nil, errors.New("formatter failed")
				}
				if mode == "original-read" && strings.HasSuffix(key, "^{tree}") {
					return nil, errors.New("missing object")
				}
				if mode == "no-changes" && strings.HasSuffix(key, "^{tree}") {
					return []byte(treeSHA), nil
				}
				if (mode == "files" || mode == "search") && len(args) > 0 && args[0] == "ls-files" {
					return nil, errors.New("files failed")
				}
				if mode == "diff" && len(args) > 0 && args[0] == "add" {
					return nil, errors.New("stage failed")
				}
				if name == "make" && mode == "check-mutation" {
					f.tree = head
				}
				if f.checks > 0 && mode == "check-tree-error" && len(args) > 0 && args[0] == "write-tree" {
					return nil, errors.New("tree failed")
				}
				return original.Run(ctx, dir, name, args...)
			})
			switch mode {
			case "patch":
				tools = append([]Reply{reply("apply_patch", `{"path":"main.go","old_text":"not there","new_text":"new"}`)}, tools...)
			case "formatter":
				tools = append([]Reply{reply("run_formatter", `{}`)}, tools...)
			case "batch-finish":
				r := reply("finish_repair", `{"summary":"Done"}`)
				r.Message.ToolCalls = append(r.Message.ToolCalls, reply("list_files", `{}`).Message.ToolCalls[0])
				tools = append([]Reply{r}, tools...)
			case "search":
				tools = append([]Reply{reply("grep", `{"query":"main"}`)}, tools...)
			case "files":
				tools = append([]Reply{reply("list_files", `{}`)}, tools...)
			case "diff":
				tools = append([]Reply{reply("read_diff", `{}`)}, tools...)
			case "prose":
				tools = append([]Reply{{Model: "local", Done: true, Message: Message{Role: "assistant", Content: "I fixed it"}}}, tools...)
			}
			f.model.replies = append([]Reply{reply("finish_review", changesJSON)}, tools...)
			f.model.replies = append(f.model.replies, reply("finish_evaluation", assessmentJSON()))
			job, err := f.advance()
			if err != nil {
				t.Fatal(err)
			}
			report := job.Runs[0].Repair.Session.Report
			bad := mode == "original-read" || mode == "no-changes" || mode == "diff" || mode == "check-tree-error"
			if bad && report.Status == "repaired" {
				t.Fatal("bad repair accepted", mode)
			}
			if mode == "check-mutation" && report.CheckFailure == "" {
				t.Fatal("mutating checks accepted")
			}
		})
	}
}
