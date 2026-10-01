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

	"github.com/ericdmoore/ferretta/internal/github"
)

type checkAPI struct {
	proposalAPI
	checks                    map[int64]github.CheckRun
	writes                    []github.CheckInput
	writeErr, listErr, getErr error
	drop                      bool
	wrongReply                bool
	pulls                     []PR
	rejectTitle               string
}

func (a *checkAPI) Status(ctx context.Context, repo string) (github.Identity, error) {
	id, err := a.proposalAPI.Status(ctx, repo)
	id.AppID = 7
	return id, err
}
func (a *checkAPI) OpenPullRequests(context.Context, string) ([]PR, error) { return a.pulls, nil }
func (a *checkAPI) CheckRuns(_ context.Context, _, head string) ([]github.CheckRun, error) {
	var result []github.CheckRun
	for _, check := range a.checks {
		if check.Head == head {
			result = append(result, check)
		}
	}
	return result, a.listErr
}
func (a *checkAPI) CheckRun(_ context.Context, _ string, id int64) (github.CheckRun, error) {
	return a.checks[id], a.getErr
}
func (a *checkAPI) WriteCheck(_ context.Context, _ string, id int64, input github.CheckInput) (github.CheckRun, error) {
	a.writes = append(a.writes, input)
	if a.rejectTitle != "" && strings.Contains(input.Output.Title, a.rejectTitle) {
		return github.CheckRun{}, github.ErrCheckNotDispatched
	}
	if errors.Is(a.writeErr, github.ErrCheckNotDispatched) {
		return github.CheckRun{}, a.writeErr
	}
	if a.checks == nil {
		a.checks = map[int64]github.CheckRun{}
	}
	if id == 0 {
		id = int64(len(a.checks) + 1)
	}
	result := github.CheckRun{CheckInput: input, ID: id, URL: fmt.Sprintf("https://github.com/o/r/runs/%d", id)}
	result.App.ID = 7
	if !a.drop {
		a.checks[id] = result
	}
	if a.wrongReply {
		result.App.ID = 42
	}
	return result, a.writeErr
}
func checksCLI(m *fakeModel, a *checkAPI) CLI {
	return CLI{Runner: *runner(m), Proposals: a, Checks: a, PublicationMode: "checks"}
}

func TestChecksWorkflowLifecycle(t *testing.T) {
	m := &fakeModel{replies: workflowReplies()}
	api := &checkAPI{}
	c := checksCLI(m, api)
	store := &memoryWorkflow{}
	job, err := advance(t, c, store)
	if err != nil || job.Runs[0].Phase != workflowDone || api.posts != 0 || len(api.checks) != 2 || len(m.requests) != 3 {
		t.Fatal(job, err, api.posts, len(api.checks))
	}
	run := job.Runs[0]
	if run.Checks["review"].Result.Conclusion != "success" || run.Checks["evaluation"].Result.Conclusion != "neutral" {
		t.Fatal("verdict and grades conflated")
	}
	start := api.writes[0].Output.Text
	for _, expected := range []string{"requested effort", "requested thinking", "allowance", "Plan:"} {
		if !strings.Contains(start, expected) {
			t.Fatal("missing startup detail", expected)
		}
	}
	if !strings.Contains(run.Checks["evaluation"].Result.Output.Text, run.Checks["review"].Result.URL) {
		t.Fatal("missing verdict link")
	}
	writes := len(api.writes)
	if _, err := advance(t, c, store); err != nil || len(api.writes) != writes || len(m.requests) != 3 {
		t.Fatal("repeated workflow replayed", err)
	}
	status, _ := json.Marshal(workflowStatus(job))
	if !bytes.Contains(status, []byte(`"checks"`)) || bytes.Contains(status, []byte("private continuation")) {
		t.Fatal(string(status))
	}
	// Same revision and policy cannot change publication channels implicitly.
	c.PublicationMode = "comments"
	if _, err := advance(t, c, store); err == nil {
		t.Fatal("mode changed silently")
	}
}

func TestChecksWorkflowIntentAndSupersession(t *testing.T) {
	m := &fakeModel{replies: []Reply{reply("request_intent_confirmation", askJSON)}}
	api := &checkAPI{}
	c := checksCLI(m, api)
	store := &memoryWorkflow{}
	job, err := advance(t, c, store)
	if err != nil || api.posts != 1 || job.Runs[0].Phase != workflowWaiting || job.Runs[0].Checks["review"].Result.Status != "in_progress" {
		t.Fatal(job, err, api.posts)
	}
	before := len(api.writes)
	if _, err := advance(t, c, store); err != nil || len(m.requests) != 1 || len(api.writes) != before {
		t.Fatal("waiting spent again", err)
	}
	p := job.Runs[0].Session.Report.Proposals[0]
	api.comments = append(api.comments, commentFixture(2, fmt.Sprintf("CONFIRMED-%s-v1:: A", p.Topic), "human", "User"))
	m.replies = workflowReplies()
	job, err = advance(t, c, store)
	if err != nil || job.Runs[0].Phase != workflowDone || api.posts != 1 {
		t.Fatal(job, err)
	}
	// An incomplete old run is closed out on its original SHA before new work.
	store.mutate(t, func(j *workflowJob) { j.Runs[0].Phase = workflowWaiting })
	newPR := pr()
	newPR.Head = strings.Repeat("d", 40)
	c.Runner.GitHub = prFunc(func(context.Context, string, int) (PR, error) { return newPR, nil })
	m.replies = workflowReplies()
	job, err = c.advanceWorkflow(context.Background(), store, "o/r", newPR, policy(t), []byte(policyJSON), judgePolicy(t), []byte(evaluationPolicyJSON()), []string{"human"})
	if err != nil || len(job.Runs) != 2 || len(api.checks) != 4 {
		t.Fatal(job, err)
	}
	for _, effect := range job.Runs[0].Checks {
		if effect.Result.Conclusion != "cancelled" || effect.Result.Head != head {
			t.Fatal("superseded check binding lost")
		}
	}
}

func TestChecksWorkflowCrashBoundaries(t *testing.T) {
	for failAt := 1; failAt <= 65; failAt++ {
		t.Run(fmt.Sprint(failAt), func(t *testing.T) {
			m := &fakeModel{replies: workflowReplies()}
			api := &checkAPI{}
			c := checksCLI(m, api)
			store := &memoryWorkflow{failAt: failAt}
			_, _ = advance(t, c, store)
			store.failAt = 0
			_, _ = advance(t, c, store)
			if len(m.requests) > 3 {
				t.Fatal("model work replayed")
			}
			seen := map[string]bool{}
			for _, check := range api.checks {
				if seen[check.ExternalID] {
					t.Fatal("duplicate check created")
				}
				seen[check.ExternalID] = true
			}
		})
	}
}

func TestCheckEffectReconciliation(t *testing.T) {
	for _, mode := range []string{"create", "update", "lost_without_effect", "not_dispatched", "edited", "wrong_reply", "list_failure", "get_failure", "draft_recovery"} {
		t.Run(mode, func(t *testing.T) {
			api := &checkAPI{}
			c := checksCLI(&fakeModel{}, api)
			job := &workflowJob{Repository: "o/r", PR: 1, AppID: 7}
			run := &workflowRun{ID: "run", Session: managedSession{Session: Session{Report: Report{PR: pr()}}}}
			save := func() error { return nil }
			publish := func(text string) error {
				return c.publishCheck(context.Background(), job, run, "review", "in_progress", "", "Reviewing", text, save)
			}
			if mode == "update" || mode == "get_failure" {
				if err := publish("initial"); err != nil {
					t.Fatal(err)
				}
			}
			api.writeErr = errors.New("response lost")
			if mode == "not_dispatched" {
				api.writeErr = github.ErrCheckNotDispatched
			}
			if mode == "lost_without_effect" {
				api.drop = true
			}
			if mode == "list_failure" {
				api.listErr = errors.New("offline")
			}
			if mode == "wrong_reply" {
				api.writeErr = nil
				api.wrongReply = true
			}
			if mode == "draft_recovery" {
				api.writeErr = github.ErrCheckNotDispatched
			}
			if err := publish("next"); err == nil {
				t.Fatal("expected failure")
			}
			writes := len(api.writes)
			api.writeErr = nil
			api.wrongReply = false
			if mode == "edited" {
				v := api.checks[1]
				v.Output.Text = "edited"
				api.checks[1] = v
			}
			if mode == "get_failure" {
				api.getErr = errors.New("offline")
			}
			text := "next"
			if mode == "draft_recovery" {
				text = "newer"
			}
			err := publish(text)
			blocked := mode == "lost_without_effect" || mode == "edited" || mode == "list_failure" || mode == "get_failure"
			if (err != nil) != blocked {
				t.Fatal(mode, err)
			}
			if mode != "not_dispatched" && mode != "draft_recovery" && len(api.writes) != writes {
				t.Fatal("uncertain effect replayed")
			}
		})
	}
}

func TestCheckReconciliationRejectsMismatches(t *testing.T) {
	input := github.CheckInput{Name: "Ferretta / review", Head: head, ExternalID: "effect", Status: "in_progress", Output: github.CheckOutput{Title: "t", Summary: "s"}}
	valid := github.CheckRun{CheckInput: input, ID: 1, URL: "https://github.com/o/r/runs/1"}
	valid.App.ID = 7
	for _, mode := range []string{"nil", "input", "delivery", "app", "target", "duplicate", "wrong_head", "wrong_id", "empty_url", "missing", "foreign"} {
		effect := &checkEffect{Request: input, Delivery: Uncertain}
		observed := []github.CheckRun{valid}
		appID := int64(7)
		switch mode {
		case "nil":
			effect = nil
		case "input":
			effect.Request.Head = "bad"
		case "delivery":
			effect.Delivery = "bad"
		case "app":
			appID = 0
		case "target":
			effect.TargetID = -1
		case "duplicate":
			observed = append(observed, valid)
		case "wrong_head":
			observed[0].Head = strings.Repeat("b", 40)
		case "wrong_id":
			effect.TargetID = 2
		case "empty_url":
			observed[0].URL = ""
		case "missing":
			observed = nil
		case "foreign":
			observed[0].App.ID = 8
		}
		if _, _, err := reconcileCheck(effect, observed, appID); err == nil {
			t.Fatal(mode)
		}
	}
}

func TestChecksProgressIsBoundedAndPublic(t *testing.T) {
	api := &checkAPI{}
	c := checksCLI(&fakeModel{}, api)
	now := time.Unix(100, 0)
	c.Runner.Now = func() time.Time { return now }
	var log bytes.Buffer
	c.Progress = &log
	job := &workflowJob{Repository: "o/r", PR: 1, AppID: 7, Publication: "checks"}
	run := &workflowRun{ID: "run", Session: managedSession{Session: Session{Report: Report{PR: pr()}}}}
	save := func() error { return nil }
	events := []UsageEvent{{Kind: "model", Operation: "review", Outcome: "pending"}, {Kind: "tool", Operation: "secret tool arguments", Outcome: "failed"}}
	if err := c.publishProgress(context.Background(), job, run, "review", "Plan", events, save); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(log.String(), "secret") || !strings.Contains(log.String(), "awaiting response") {
		t.Fatal(log.String())
	}
	before := len(api.writes)
	if err := c.publishProgress(context.Background(), job, run, "review", "Plan", events, save); err != nil || len(api.writes) != before {
		t.Fatal("progress not coalesced", err)
	}
	now = now.Add(11 * time.Second)
	events = append(events, UsageEvent{Operation: "read_file", Outcome: "completed"})
	if err := c.publishProgress(context.Background(), job, run, "review", "Plan", events, save); err != nil || len(api.writes) != before+1 {
		t.Fatal(err)
	}
	long := make([]UsageEvent, 20)
	for i := range long {
		long[i] = UsageEvent{Operation: "review", Outcome: "completed"}
	}
	if strings.Count(progressText(long), "- review") != 12 {
		t.Fatal("unbounded activity")
	}
	job.Publication = "comments"
	if err := c.publishProgress(context.Background(), job, run, "review", "Plan", events, save); err != nil {
		t.Fatal(err)
	}
	if reviewConclusion("changes_required") != "failure" || reviewConclusion("incomplete") != "action_required" {
		t.Fatal("unsafe verdict mapping")
	}
}

func TestChecksWatchDefaultAndLegacyState(t *testing.T) {
	t.Chdir(t.TempDir())
	_ = os.WriteFile("review.json", []byte(policyJSON), 0600)
	_ = os.WriteFile("judge.json", []byte(evaluationPolicyJSON()), 0600)
	api := &checkAPI{pulls: []PR{pr()}}
	m := &fakeModel{replies: workflowReplies()}
	c := checksCLI(m, api)
	args := []string{"--repo", "o/r", "--pr", "1", "--state", filepath.Join(t.TempDir(), "state"), "--review-policy", "review.json", "--judge-policy", "judge.json", "--humans", "human", "--once"}
	var errs bytes.Buffer
	if c.Watch(context.Background(), nil, io.Discard, io.Discard) != 1 {
		t.Fatal("invalid watch arguments accepted")
	}
	if code := c.Watch(context.Background(), args, io.Discard, &errs); code != 0 || api.posts != 0 || len(api.checks) != 2 {
		t.Fatal(code, errs.String())
	}
	if c.Watch(context.Background(), append(args, "--publication", "unknown"), io.Discard, io.Discard) != 1 {
		t.Fatal("unknown mode")
	}
	c.Checks = nil
	if c.Watch(context.Background(), args, io.Discard, io.Discard) != 1 {
		t.Fatal("missing checks adapter")
	}
	// Saved v1 workflows with no mode remain comments, including after restart.
	m = &fakeModel{replies: workflowReplies()}
	legacy := CLI{Runner: *runner(m), Proposals: &proposalAPI{}}
	store := &memoryWorkflow{}
	if _, err := advance(t, legacy, store); err != nil {
		t.Fatal(err)
	}
	store.mutate(t, func(j *workflowJob) { j.Publication = ""; j.AppID = 0 })
	if _, err := advance(t, legacy, store); err != nil {
		t.Fatal("legacy mode lost", err)
	}
	legacy.PublicationMode = "bad"
	if _, err := advance(t, legacy, store); err == nil {
		t.Fatal("invalid mode")
	}
	legacy.PublicationMode = "checks"
	if _, err := advance(t, legacy, store); err == nil {
		t.Fatal("missing adapter")
	}
}
