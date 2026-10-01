package review

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ericdmoore/ferretta/internal/github"
	"github.com/ericdmoore/ferretta/internal/service"
)

func TestRepairUncertainPushReconciliation(t *testing.T) {
	for _, mode := range []string{"delivered", "descendant", "not-delivered", "preflight-rejected"} {
		t.Run(mode, func(t *testing.T) {
			f := newRepairFixture(t)
			f.model.replies = repairReplies()
			f.git.pushErr = errors.New("connection lost")
			if mode == "not-delivered" || mode == "preflight-rejected" {
				f.git.onPush = nil
			}
			if mode == "preflight-rejected" {
				f.git.pushErr = errPushNotDispatched
			}
			if _, err := f.advance(); err == nil {
				t.Fatal("uncertainty hidden")
			}
			calls := len(f.model.requests)
			f.git.pushErr = nil
			if mode == "not-delivered" {
				original := f.c.Runner.Exec
				f.c.Runner.Exec = commandFunc(func(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
					if len(args) > 1 && args[0] == "merge-base" && args[1] == "--is-ancestor" {
						return nil, errors.New("not ancestor")
					}
					return original.Run(ctx, dir, name, args...)
				})
			}
			if mode == "descendant" {
				f.pr.Head = strings.Repeat("e", 40)
			}
			f.model.replies = workflowReplies()
			job, err := f.advance()
			if mode == "not-delivered" {
				if err == nil || f.git.pushes != 1 || len(f.model.requests) != calls {
					t.Fatal("uncertain push repeated", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := 1
			if mode == "preflight-rejected" {
				want = 2
			}
			if f.git.pushes != want || job.Runs[0].Repair.Delivery != Posted {
				t.Fatal("lost receipt or duplicate push")
			}
		})
	}
}

func TestRepairSaveBoundaryRecovery(t *testing.T) {
	complete := newRepairFixture(t)
	complete.model.replies = repairReplies()
	if _, err := complete.advance(); err != nil {
		t.Fatal(err)
	}
	// Every persisted boundary is interrupted once. On recovery, recorded model
	// calls are never replayed and a published candidate is never pushed twice.
	for n := 1; n <= complete.store.saves; n++ {
		t.Run(string(rune('A'+n)), func(t *testing.T) {
			f := newRepairFixture(t)
			f.model.replies = append(repairReplies(), reply("finish_evaluation", assessmentJSON()))
			f.store.failAt = n
			_, _ = f.advance()
			before := f.git.pushes
			f.store.failAt = 0
			f.model.replies = workflowReplies() // Any legitimately new revision review.
			job, _ := f.advance()
			if f.git.pushes > 1 || before > 1 {
				t.Fatal("duplicate publication at boundary", n)
			}
			if job != nil && job.ConsumptionUncertain && f.git.pushes > before {
				t.Fatal("uncertain consumption admitted publication", n)
			}
		})
	}
}

func TestRepairLimitsAndFailures(t *testing.T) {
	for _, mode := range []string{"cycles", "compute", "review-reserve", "judge-reserve", "fork", "uncertain", "zero-policy", "intent", "local-check-failure"} {
		t.Run(mode, func(t *testing.T) {
			p := repairPolicy(t)
			run := &workflowRun{Session: managedSession{Session: Session{Report: Report{PR: repairPR()}}}}
			job := &workflowJob{Repository: "o/r", Runs: []*workflowRun{run}}
			reviewer, judge := policy(t), judgePolicy(t)
			switch mode {
			case "cycles":
				p.cycles = 1
				run.Repair = &repairAttempt{}
			case "compute":
				job.RepairNanos = int64(time.Minute)
			case "review-reserve":
				job.ReviewNanos = int64(time.Minute)
			case "judge-reserve":
				job.EvaluationNanos = int64(time.Duration(judge.route.config.TimeoutSeconds) * time.Second)
			case "fork":
				run.Session.Report.PR.HeadRepository = "other/r"
			case "uncertain":
				job.ConsumptionUncertain = true
			case "zero-policy":
				p = RepairPolicy{}
			case "intent":
				run.Session.Report.Proposals = []Proposal{{Topic: "pending"}}
			case "local-check-failure":
				run.Session.Report.CheckFailure = "tests fail"
			}
			err := repairAdmission(job, run, p, reviewer, judge)
			if (err == nil) != (mode == "local-check-failure") {
				t.Fatal(mode, err)
			}
		})
	}
	f := newRepairFixture(t)
	f.c.Repair.cycles = 1
	f.model.replies = repairReplies()
	if _, err := f.advance(); err != nil {
		t.Fatal(err)
	}
	f.model.replies = []Reply{reply("finish_review", changesJSON), reply("finish_evaluation", assessmentJSON())}
	job, err := f.advance()
	if err != nil {
		t.Fatal(err)
	}
	if len(job.Runs) != 2 || f.git.pushes != 1 || !strings.Contains(job.Runs[1].Checks["repair"].Result.Output.Text, "cycle allowance exhausted") {
		t.Fatal("cycles reset on new commit")
	}
}

func TestRepairUnsuccessfulStageIsVisible(t *testing.T) {
	for _, mode := range []string{"check-failure", "turn-limit", "capabilities", "protected", "stale-head", "bad-parent", "tree-change"} {
		t.Run(mode, func(t *testing.T) {
			f := newRepairFixture(t)
			f.c.Repair.route.config.MaxTurns = 3
			f.model.replies = append(repairReplies(), reply("finish_evaluation", assessmentJSON()))
			if mode == "check-failure" {
				f.failCheck = true
			}
			if mode == "turn-limit" {
				f.c.Repair.route.config.MaxTurns = 1
			}
			if mode == "capabilities" {
				f.model.capErr = errors.New("unsupported model")
			}
			original := f.c.Runner.Exec
			f.c.Runner.Exec = commandFunc(func(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
				key := name + " " + strings.Join(args, " ")
				if mode == "protected" && strings.HasPrefix(key, "git diff --cached --name-only") {
					return []byte("scripts/check.sh\x00"), nil
				}
				if mode == "bad-parent" && strings.HasPrefix(key, "git show --no-patch") {
					return []byte(candidateSHA + " " + base), nil
				}
				if mode == "stale-head" && key == "git write-tree" {
					f.pr.Base = strings.Repeat("f", 40)
				}
				return original.Run(ctx, dir, name, args...)
			})
			if mode == "tree-change" {
				originalModel := f.c.Runner.Model
				f.c.Runner.Model = modelIntercept{originalModel, func(msgs []Message) {
					if len(msgs) > 0 && strings.Contains(msgs[0].Content, "Repair demonstrated") && len(f.model.requests) == 3 {
						f.tree = strings.Repeat("f", 40)
					}
				}}
			}
			job, err := f.advance()
			if err != nil {
				t.Fatal(err)
			}
			if f.git.pushes != 0 {
				t.Fatal("unsafe repair published", mode)
			}
			if mode != "capabilities" && job.Runs[0].Checks["repair"].Result.Conclusion != "action_required" {
				t.Fatal("failure concealed")
			}
		})
	}
}

type modelIntercept struct {
	Model
	before func([]Message)
}

func (m modelIntercept) Turn(ctx context.Context, p Policy, msgs []Message) (Reply, error) {
	m.before(msgs)
	return m.Model.Turn(ctx, p, msgs)
}

func TestRepairCheckPublicationFailureDoesNotPush(t *testing.T) {
	f := newRepairFixture(t)
	f.model.replies = repairReplies()
	f.api.rejectTitle = "Running repair"
	if _, err := f.advance(); err == nil {
		t.Fatal("checkpoint failure hidden")
	}
	if f.git.pushes != 0 {
		t.Fatal("checkpoint failure allowed repair")
	}
	f = newRepairFixture(t)
	f.model.replies = repairReplies()
	f.api.rejectTitle = "Repair published"
	if _, err := f.advance(); !errors.Is(err, github.ErrCheckNotDispatched) {
		t.Fatal(err)
	}
	f.api.rejectTitle = ""
	f.model.replies = workflowReplies()
	if _, err := f.advance(); err != nil {
		t.Fatal(err)
	}
	if f.git.pushes != 1 {
		t.Fatal("publication retried repair")
	}
}

func TestRepairCorrectionRequiresReproposal(t *testing.T) {
	f := newRepairFixture(t)
	f.model.replies = []Reply{reply("finish_review", changesJSON), reply("request_intent_confirmation", askJSON)}
	job, err := f.advance()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.advance(); err != nil {
		t.Fatal(err)
	}
	proposal := job.Runs[0].Repair.Session.Report.Proposals[0]
	f.api.comments = append(f.api.comments, commentFixture(4, fmt.Sprintf("CORRECTED-%s-v1:: Neither; keep old behavior.", proposal.Topic), "human", "User"))
	revised := proposal.Request
	revised.Question = "Keep the existing behavior when input is empty?"
	args, _ := json.Marshal(revised)
	f.model.replies = []Reply{reply("finish_repair", `{"summary":"Must be refused until confirmed"}`), reply("request_intent_confirmation", string(args))}
	job, err = f.advance()
	if err != nil {
		t.Fatal(err)
	}
	if f.git.pushes != 0 || job.Runs[0].Phase != workflowRepairWaiting || len(job.Runs[0].Repair.Session.Report.Proposals) != 2 {
		t.Fatal("correction treated as approval")
	}
	if _, err = f.advance(); err != nil {
		t.Fatal(err)
	}
	f.api.comments = append(f.api.comments, commentFixture(8, fmt.Sprintf("CONFIRMED-%s-v2:: Yes", proposal.Topic), "human", "User"))
	f.model.replies = repairReplies()[1:]
	if _, err = f.advance(); err != nil {
		t.Fatal(err)
	}
	f.model.replies = workflowReplies()
	job, err = f.advance()
	if err != nil {
		t.Fatal(err)
	}
	if f.git.pushes != 1 || len(job.Runs[1].Session.Report.Proposals) != 2 {
		t.Fatal("intent lineage lost")
	}
}

func TestRepairWaitingSaveFailureAndReconciledReceipt(t *testing.T) {
	first := newRepairFixture(t)
	first.model.replies = []Reply{reply("finish_review", changesJSON), reply("request_intent_confirmation", askJSON)}
	if _, err := first.advance(); err != nil {
		t.Fatal(err)
	}
	f := newRepairFixture(t)
	f.model.replies = first.model.replies
	// Reinstall replies consumed by the completed fixture.
	f.model.replies = []Reply{reply("finish_review", changesJSON), reply("request_intent_confirmation", askJSON)}
	f.store.failAt = first.store.saves
	if _, err := f.advance(); err == nil {
		t.Fatal("waiting-state save failure hidden")
	}
	f = newRepairFixture(t)
	f.model.replies = repairReplies()
	f.git.pushErr = errors.New("lost receipt")
	if _, err := f.advance(); err == nil {
		t.Fatal("push uncertainty hidden")
	}
	f.store.failAt = f.store.saves + 1
	if _, err := f.advance(); err == nil {
		t.Fatal("reconciled receipt save failure hidden")
	}
	if f.git.pushes != 1 {
		t.Fatal("replayed push")
	}
}

func TestRepairWorkflowRetainsReviewAndEvaluationRetries(t *testing.T) {
	f := newRepairFixture(t)
	f.model.replies = workflowReplies()
	job, err := f.advance()
	if err != nil {
		t.Fatal(err)
	}
	if job.Version != 2 {
		t.Fatal("older binaries could discard repair-enabled state")
	}
	request := retryRequest{ID: "retry-judge", Actor: "human", CheckID: job.Runs[0].Checks["evaluation"].Result.ID}
	data, _ := json.Marshal(request)
	routed, rejected, err := routeWatchRetries(context.Background(), f.store, "o/r", []PR{f.pr}, []service.InboxItem{{ID: request.ID, Data: data}})
	if err != nil || len(rejected) != 0 || len(routed[1]) != 1 {
		t.Fatal("v2 retries not routed", err)
	}
	f.c.Retry = &request
	f.model.replies = []Reply{reply("finish_evaluation", assessmentJSON())}
	job, err = f.advance()
	if err != nil {
		t.Fatal(err)
	}
	if len(job.Runs) != 2 || job.Runs[1].ReviewSource == "" || f.git.pushes != 0 {
		t.Fatal("evaluation retry dispatched repairs")
	}
}
