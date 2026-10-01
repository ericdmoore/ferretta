package review

import (
	"bytes"
	"context"
	"database/sql"
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

type memoryWorkflow struct {
	data          map[string][]byte
	readErr       error
	saves, failAt int
}

func (s *memoryWorkflow) Review(_ context.Context, key string) ([]byte, error) {
	if s.readErr != nil {
		return nil, s.readErr
	}
	data, ok := s.data[key]
	if !ok {
		return nil, sql.ErrNoRows
	}
	return data, nil
}
func (s *memoryWorkflow) SaveReview(_ context.Context, key string, data []byte) error {
	s.saves++
	if s.saves == s.failAt {
		return errors.New("disk interrupted")
	}
	if s.data == nil {
		s.data = map[string][]byte{}
	}
	s.data[key] = append([]byte(nil), data...)
	return nil
}
func (s *memoryWorkflow) mutate(t *testing.T, f func(*workflowJob)) {
	t.Helper()
	for key, data := range s.data {
		var job workflowJob
		if err := json.Unmarshal(data, &job); err != nil {
			t.Fatal(err)
		}
		f(&job)
		s.data[key], _ = json.Marshal(job)
	}
}
func workflowReplies() []Reply {
	return []Reply{reply("run_checks", `{}`), reply("finish_review", finishJSON("lgtm")), reply("finish_evaluation", assessmentJSON())}
}
func advance(t *testing.T, c CLI, s workflowStore) (*workflowJob, error) {
	t.Helper()
	return c.advanceWorkflow(context.Background(), s, "o/r", pr(), policy(t), []byte(policyJSON), judgePolicy(t), []byte(evaluationPolicyJSON()), []string{"human"})
}

func TestWorkflowMilestonesAndReplay(t *testing.T) {
	m := &fakeModel{replies: workflowReplies()}
	api := &proposalAPI{}
	c := CLI{Runner: *runner(m), Proposals: api}
	store := &memoryWorkflow{}
	job, err := advance(t, c, store)
	if err != nil || len(job.Runs) != 1 || job.Runs[0].Phase != workflowDone || api.posts != 3 || len(m.requests) != 3 {
		t.Fatal(job, err, api.posts, len(m.requests))
	}
	for i, title := range []string{"has started", "review verdict", "scorecard"} {
		if !strings.Contains(api.comments[i].Body, title) || !strings.Contains(api.comments[i].Body, head) {
			t.Fatal("wrong milestone", i, api.comments[i].Body)
		}
	}
	if !strings.Contains(api.comments[0].Body, "Assigned reviewer") || !strings.Contains(api.comments[0].Body, "requested effort") || !strings.Contains(api.comments[0].Body, "allowance") {
		t.Fatal("starting plan incomplete")
	}
	if _, err := advance(t, c, store); err != nil || api.posts != 3 || len(m.requests) != 3 {
		t.Fatal("replay repeated work", err)
	}
	data, _ := json.Marshal(workflowStatus(job))
	if bytes.Contains(data, []byte("private continuation")) {
		t.Fatal("status leaked transcript")
	}
	if !strings.Contains(EvaluationMarkdown(*job.Runs[0].Evaluation), "2/3") {
		t.Fatal("scorecard missing grades")
	}
	if len(job.Runs[0].EvaluationMessages) == 0 || !strings.Contains(api.comments[0].Body, hashBytes([]byte(policyJSON))) || !strings.Contains(api.comments[1].Body, "Recorded check: make check") {
		t.Fatal("missing judge checkpoint, policy identity or check evidence")
	}
	failed := reportFixture()
	failed.CheckFailure = "test failed"
	if !strings.Contains(verdictComment(failed), "Check failure: test failed") {
		t.Fatal("failure omitted")
	}
}

func TestWorkflowHumanWaitAndRevisionChanges(t *testing.T) {
	m := &fakeModel{replies: []Reply{reply("request_intent_confirmation", askJSON)}}
	api := &proposalAPI{}
	c := CLI{Runner: *runner(m), Proposals: api}
	store := &memoryWorkflow{}
	job, err := advance(t, c, store)
	if err != nil {
		t.Fatal(err)
	}
	if api.posts != 2 || job.Runs[0].Phase != workflowWaiting {
		t.Fatal(job, api.posts)
	}
	if _, err := advance(t, c, store); err != nil || len(m.requests) != 1 || api.posts != 2 {
		t.Fatal("waiting repeated work", err)
	}
	p := job.Runs[0].Session.Report.Proposals[0]
	api.comments = append(api.comments, commentFixture(3, fmt.Sprintf("CONFIRMED-%s-v1:: A", p.Topic), "human", "User"))
	m.replies = workflowReplies()
	job, err = advance(t, c, store)
	if err != nil || job.Runs[0].Phase != workflowDone || api.posts != 4 {
		t.Fatal(job, err, api.posts)
	}
	// A new revision preserves history and accumulated resource use.
	newPR := pr()
	newPR.Head = strings.Repeat("d", 40)
	c.Runner.GitHub = prFunc(func(context.Context, string, int) (PR, error) { return newPR, nil })
	m.replies = workflowReplies()
	job, err = c.advanceWorkflow(context.Background(), store, "o/r", newPR, policy(t), []byte(policyJSON), judgePolicy(t), []byte(evaluationPolicyJSON()), []string{"human"})
	if err != nil || len(job.Runs) != 2 || api.posts != 7 || job.Runs[0].Session.Report.PR.Head != head {
		t.Fatal(job, err, api.posts)
	}
	if _, err := advance(t, c, store); err != nil || api.posts != 7 {
		t.Fatal("force-push back repeated work", err)
	}
}

func TestWorkflowPublicationReconciliation(t *testing.T) {
	for _, mode := range []string{"ambiguous", "not_dispatched", "no_match", "wrong_reply", "read_failure", "edited", "oversized", "save_failure"} {
		t.Run(mode, func(t *testing.T) {
			api := &proposalAPI{}
			c := CLI{Proposals: api}
			run := &workflowRun{ID: "run", Publications: map[string]*publication{}}
			saves := 0
			save := func() error {
				saves++
				if mode == "save_failure" {
					return errors.New("storage")
				}
				return nil
			}
			body := "Starting"
			switch mode {
			case "ambiguous", "no_match", "edited":
				api.postErr = errors.New("response lost")
			case "not_dispatched":
				api.postErr = github.ErrCommentNotDispatched
			case "wrong_reply":
				api.afterPost = func() { api.comments[0].User.Login = "imposter" }
			case "read_failure":
				api.readErr = errors.New("offline")
			case "oversized":
				body = strings.Repeat("x", 60001)
			}
			err := c.publishWorkflow(context.Background(), run, "starting", body, "o/r", 1, "ferretta[bot]", save)
			if mode != "wrong_reply" && err == nil {
				t.Fatal("expected failure", mode)
			}
			if mode == "ambiguous" {
				api.postErr = nil
				if err := c.publishWorkflow(context.Background(), run, "starting", body, "o/r", 1, "ferretta[bot]", save); err != nil || api.posts != 1 {
					t.Fatal("reconciliation duplicated comment", err)
				}
			}
			if mode == "no_match" {
				api.comments = nil
				if err := c.publishWorkflow(context.Background(), run, "starting", body, "o/r", 1, "ferretta[bot]", save); err == nil || api.posts != 1 {
					t.Fatal("uncertain missing effect reposted")
				}
			}
			if mode == "edited" {
				api.comments[0].Body += "edited"
				if err := c.publishWorkflow(context.Background(), run, "starting", body, "o/r", 1, "ferretta[bot]", save); err == nil {
					t.Fatal("edited comment accepted")
				}
			}
			if mode == "not_dispatched" && run.Publications["starting"].Delivery != Draft {
				t.Fatal("known pre-dispatch failure not retryable")
			}
		})
	}
}

func TestWorkflowCrashBoundaries(t *testing.T) {
	// Inject a failed durable write at each boundary, then restart from persisted
	// bytes. This checks observable no-replay/no-duplicate invariants across all
	// stages rather than asserting the implementation's private call sequence.
	for failAt := 1; failAt <= 36; failAt++ {
		t.Run(fmt.Sprint(failAt), func(t *testing.T) {
			m := &fakeModel{replies: workflowReplies()}
			api := &proposalAPI{}
			c := CLI{Runner: *runner(m), Proposals: api}
			s := &memoryWorkflow{failAt: failAt}
			_, _ = advance(t, c, s)
			s.failAt = 0
			_, _ = advance(t, c, s)
			if len(m.requests) > 3 {
				t.Fatal("model calls replayed", len(m.requests))
			}
			seen := map[string]bool{}
			for _, comment := range api.comments {
				if seen[comment.Body] {
					t.Fatal("duplicate publication")
				}
				seen[comment.Body] = true
			}
		})
	}
}

func TestWorkflowInvalidAndInterruptedState(t *testing.T) {
	mutations := []func(*workflowJob){
		func(j *workflowJob) { j.Version = 99 }, func(j *workflowJob) { j.Runs[0].Publications = nil }, func(j *workflowJob) { j.Runs[0].Phase = "unknown" },
		func(j *workflowJob) { j.Runs[0].Phase = workflowWaiting; j.Runs[0].Session.Report.Proposals = nil },
		func(j *workflowJob) { j.Runs[0].Phase = workflowScorecard; j.Runs[0].Evaluation = nil },
	}
	for _, mutate := range mutations {
		m := &fakeModel{replies: workflowReplies()}
		c := CLI{Runner: *runner(m), Proposals: &proposalAPI{}}
		s := &memoryWorkflow{}
		if _, err := advance(t, c, s); err != nil {
			t.Fatal(err)
		}
		s.mutate(t, mutate)
		if _, err := advance(t, c, s); err == nil {
			t.Fatal("invalid state accepted")
		}
	}
	for _, phase := range []workflowPhase{workflowReviewRunning, workflowEvaluating} {
		m := &fakeModel{replies: workflowReplies()}
		api := &proposalAPI{}
		c := CLI{Runner: *runner(m), Proposals: api}
		s := &memoryWorkflow{}
		if _, err := advance(t, c, s); err != nil {
			t.Fatal(err)
		}
		s.mutate(t, func(j *workflowJob) {
			r := j.Runs[0]
			r.Phase = phase
			r.Publications = map[string]*publication{}
			r.Evaluation = nil
		})
		api.comments = nil
		job, err := advance(t, c, s)
		if err != nil || !job.ConsumptionUncertain || len(m.requests) != 3 || job.Runs[0].Evaluation.Assessment != nil {
			t.Fatal("interrupted work replayed", job, err)
		}
	}
	if _, err := advance(t, CLI{}, &memoryWorkflow{}); err == nil {
		t.Fatal("missing App accepted")
	}
	c := CLI{Runner: *runner(&fakeModel{}), Proposals: &proposalAPI{statusErr: errors.New("auth")}}
	if _, err := advance(t, c, &memoryWorkflow{}); err == nil {
		t.Fatal("auth failure ignored")
	}
	c.Proposals = &proposalAPI{bot: "human"}
	if _, err := c.advanceWorkflow(context.Background(), &memoryWorkflow{}, "o/r", pr(), policy(t), nil, judgePolicy(t), nil, []string{"human"}); err == nil {
		t.Fatal("human/bot overlap")
	}
	c.Proposals = &proposalAPI{}
	for _, s := range []*memoryWorkflow{{readErr: errors.New("read failed")}, {data: map[string][]byte{"workflow-v1:o/r:1": []byte("{")}}} {
		if _, err := advance(t, c, s); err == nil {
			t.Fatal("store failure ignored")
		}
	}
}

func TestWorkflowLimitsAndPreparationFailures(t *testing.T) {
	p := policy(t)
	for _, used := range []int64{int64(time.Minute), int64(time.Minute - time.Millisecond)} {
		if _, err := remainingPolicy(p, used); err == nil {
			t.Fatal("exhausted allowance accepted")
		}
	}
	p.config.TimeoutSeconds = 0
	if _, err := remainingPolicy(p, int64(time.Hour)); err != nil {
		t.Fatal("unlimited allowance rejected")
	}
	if !strings.Contains(startingComment(pr(), "run", p, judgePolicy(t), 0, 0), "unlimited") {
		t.Fatal("unlimited hidden")
	}
	for _, mode := range []string{"prepare", "deadline", "judge-deadline", "stale", "revalidation", "temp"} {
		t.Run(mode, func(t *testing.T) {
			m := &fakeModel{replies: workflowReplies()}
			c := CLI{Runner: *runner(m), Proposals: &proposalAPI{}}
			s := &memoryWorkflow{}
			if mode == "prepare" {
				c.Runner.Fetch = func(context.Context, string, string) error { return errors.New("fetch") }
			}
			if mode == "temp" {
				t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
			}
			if mode == "stale" || mode == "revalidation" {
				calls := 0
				c.Runner.GitHub = prFunc(func(context.Context, string, int) (PR, error) {
					calls++
					if mode == "stale" || calls > 1 {
						return PR{}, errors.New("unavailable")
					}
					return pr(), nil
				})
			}
			if mode == "deadline" || mode == "judge-deadline" {
				_, _ = advance(t, c, s)
				s.mutate(t, func(j *workflowJob) {
					j.Runs[0].Publications = map[string]*publication{}
					j.Runs[0].Phase = workflowReview
					if mode == "deadline" {
						j.ReviewNanos = int64(time.Minute)
					} else {
						j.Runs[0].Phase = workflowEvaluate
						j.EvaluationNanos = int64(time.Minute)
					}
				})
				c.Proposals.(*proposalAPI).comments = nil
			}
			job, err := advance(t, c, s)
			if mode == "stale" {
				if err == nil {
					t.Fatal("lost stale error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if mode != "judge-deadline" && job.Runs[0].Session.Report.Status == "lgtm" {
				t.Fatal("failure approved")
			}
		})
	}
}

type watchAPI struct {
	proposalAPI
	pulls     []github.PullRequest
	pollErr   error
	afterPoll func()
}

func (a *watchAPI) OpenPullRequests(context.Context, string) ([]github.PullRequest, error) {
	if a.afterPoll != nil {
		a.afterPoll()
	}
	return a.pulls, a.pollErr
}

func TestWatchCLI(t *testing.T) {
	t.Chdir(t.TempDir())
	_ = os.WriteFile("review.json", []byte(policyJSON), 0600)
	_ = os.WriteFile("judge.json", []byte(evaluationPolicyJSON()), 0600)
	state := filepath.Join(t.TempDir(), "state")
	args := []string{"--repo", "o/r", "--pr", "1", "--state", state, "--review-policy", "review.json", "--judge-policy", "judge.json", "--humans", "human", "--once"}
	m := &fakeModel{replies: workflowReplies()}
	api := &watchAPI{pulls: []PR{pr()}}
	c := CLI{Runner: *runner(m), Proposals: api}
	var out, errs bytes.Buffer
	if code := c.Watch(context.Background(), args, &out, &errs); code != 0 {
		t.Fatal(code, errs.String())
	}
	if !strings.Contains(out.String(), "complete") || api.posts != 3 {
		t.Fatal(out.String(), api.posts)
	}
	if code := c.Watch(context.Background(), args, io.Discard, io.Discard); code != 0 || len(m.requests) != 3 {
		t.Fatal("restart repeated calls", code)
	}
	for _, bad := range [][]string{nil, {"--unknown"}} {
		if c.Watch(context.Background(), bad, io.Discard, io.Discard) != 1 {
			t.Fatal(bad)
		}
	}
	if c.Watch(context.Background(), args, failingIO{}, io.Discard) != 1 {
		t.Fatal("write failure ignored")
	}
	api.pollErr = errors.New("offline")
	if c.Watch(context.Background(), args, io.Discard, io.Discard) != 1 {
		t.Fatal("poll failure ignored")
	}
	api.pollErr = nil
	api.pulls = []PR{{Number: 1}}
	if c.Watch(context.Background(), args, io.Discard, io.Discard) != 1 {
		t.Fatal("malformed listing")
	}
	api.pulls = []PR{}
	if c.Watch(context.Background(), args, io.Discard, io.Discard) != 0 {
		t.Fatal("missing target should wait")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if c.Watch(ctx, args, io.Discard, io.Discard) != 0 {
		t.Fatal("cancellation")
	}
	for _, path := range []string{"review.json", "judge.json"} {
		original, _ := os.ReadFile(path)
		_ = os.WriteFile(path, []byte(`{}`), 0600)
		if c.Watch(context.Background(), args, io.Discard, io.Discard) != 1 {
			t.Fatal("bad policy", path)
		}
		_ = os.Remove(path)
		if c.Watch(context.Background(), args, io.Discard, io.Discard) != 1 {
			t.Fatal("missing policy", path)
		}
		_ = os.WriteFile(path, original, 0600)
	}
}
