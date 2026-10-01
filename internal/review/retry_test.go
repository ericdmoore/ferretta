package review

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ericdmoore/ferretta/internal/intent"
)

func TestRetryReviewAndEvaluation(t *testing.T) {
	for _, role := range []string{"review", "evaluation"} {
		t.Run(role, func(t *testing.T) {
			m := &fakeModel{replies: workflowReplies()}
			api := &checkAPI{}
			c, store := checksCLI(m, api), &memoryWorkflow{}
			now := time.Unix(100, 0)
			c.Runner.Now = func() time.Time { now = now.Add(time.Millisecond); return now }
			job, err := advance(t, c, store)
			if err != nil {
				t.Fatal(err)
			}
			original, _ := json.Marshal(job.Runs[0])
			beforeReview, beforeEval := job.ReviewNanos, job.EvaluationNanos
			c.Retry = &retryRequest{ID: "delivery-1", CheckID: job.Runs[0].Checks[role].Result.ID, Actor: "human"}
			m.replies = workflowReplies()
			wantCalls, wantChecks := 6, 4
			if role == "evaluation" {
				m.replies = m.replies[2:]
				wantCalls, wantChecks = 4, 3
			}
			job, err = advance(t, c, store)
			if err != nil || len(job.Runs) != 2 || len(m.requests) != wantCalls || len(api.checks) != wantChecks {
				t.Fatal(job, err, len(m.requests), len(api.checks))
			}
			unchanged, _ := json.Marshal(job.Runs[0])
			if string(original) != string(unchanged) || job.ReviewNanos < beforeReview || job.EvaluationNanos < beforeEval {
				t.Fatal("history or accounting reset")
			}
			if role == "evaluation" && job.ReviewNanos != beforeReview {
				t.Fatal("evaluation retry spent reviewer allowance")
			}
			if job.Runs[1].RetryParent != job.Runs[0].ID || job.Runs[1].ID == job.Runs[0].ID {
				t.Fatal("missing attempt provenance")
			}
			if !strings.Contains(job.Runs[1].Checks["evaluation"].Result.Output.Summary, "delivery-1") {
				t.Fatal("retry not visible")
			}
			if _, err := advance(t, c, store); err != nil || len(m.requests) != wantCalls || len(api.checks) != wantChecks {
				t.Fatal("duplicate delivery spent again", err)
			}
			c.Retry.Actor = "changed"
			if _, err := advance(t, c, store); err == nil {
				t.Fatal("delivery identity reused")
			}
		})
	}
}

func TestRetryAdmission(t *testing.T) {
	for _, mode := range []string{"invalid", "empty", "legacy", "uncertain", "running", "new-head", "unknown-check", "review-budget", "judge-budget", "pending-intent", "corrected-intent", "duplicate-check"} {
		t.Run(mode, func(t *testing.T) {
			m := &fakeModel{replies: workflowReplies()}
			c, store := checksCLI(m, &checkAPI{}), &memoryWorkflow{}
			job, err := advance(t, c, store)
			if err != nil {
				t.Fatal(err)
			}
			request := retryRequest{ID: "delivery", CheckID: job.Runs[0].Checks["review"].Result.ID, Actor: "human"}
			current := pr()
			switch mode {
			case "invalid":
				request.ID = "bad id"
			case "empty":
				job.Runs = nil
			case "legacy":
				job.Publication = "comments"
			case "uncertain":
				job.ConsumptionUncertain = true
			case "running":
				job.Runs[0].Phase = workflowReviewRunning
			case "new-head":
				current.Head = strings.Repeat("d", 40)
			case "unknown-check":
				request.CheckID = 500
			case "review-budget":
				job.ReviewNanos = int64(time.Hour)
			case "judge-budget":
				job.EvaluationNanos = int64(time.Hour)
			case "pending-intent":
				job.Runs[0].Session.Report.Proposals = []Proposal{{Topic: "pending"}}
			case "corrected-intent":
				job.Runs[0].Session.Report.Proposals = []Proposal{{Topic: "pending", Decision: &intent.Evidence{Marker: intent.Marker{Vocabulary: intent.Corrected}}}}
			case "duplicate-check":
				job.Runs[0].Retry = &retryRequest{ID: "other", CheckID: request.CheckID, Actor: "human"}
			}
			if _, err := planRetry(job, request, current, policy(t), judgePolicy(t)); err == nil {
				t.Fatal("unsafe retry admitted")
			}
		})
	}
}

func TestRetryRetainsSameRevisionIntent(t *testing.T) {
	m := &fakeModel{replies: workflowReplies()}
	c, store := checksCLI(m, &checkAPI{}), &memoryWorkflow{}
	job, err := advance(t, c, store)
	if err != nil {
		t.Fatal(err)
	}
	store.mutate(t, func(j *workflowJob) {
		j.Runs[0].Session.Report.Proposals = []Proposal{
			{Topic: "Scope", Version: 1, Decision: &intent.Evidence{Marker: intent.Marker{Vocabulary: intent.Corrected, Body: "use SQLite"}}},
			{Topic: "Scope", Version: 2, Decision: &intent.Evidence{CommentID: 4, Author: "human", URL: "https://github.com/o/r/pull/1#issuecomment-4", SHA256: "source-hash", Marker: intent.Marker{Vocabulary: intent.Confirmed, Body: "SQLite is required"}}},
		}
	})
	c.Retry = &retryRequest{ID: "same-revision", CheckID: job.Runs[0].Checks["review"].Result.ID, Actor: "human"}
	m.replies = workflowReplies()
	job, err = advance(t, c, store)
	if err != nil || len(job.Runs[1].Session.Report.Proposals) != 2 {
		t.Fatal(job, err)
	}
	data, _ := json.Marshal(m.requests[3])
	if !strings.Contains(string(data), "SQLite is required") || !strings.Contains(string(data), "source-hash") {
		t.Fatal("intent evidence lost")
	}
}

func TestRetryCrashDoesNotDuplicateWork(t *testing.T) {
	for failAt := 1; failAt <= 65; failAt++ {
		m := &fakeModel{replies: workflowReplies()}
		api := &checkAPI{}
		c, store := checksCLI(m, api), &memoryWorkflow{}
		job, err := advance(t, c, store)
		if err != nil {
			t.Fatal(err)
		}
		c.Retry = &retryRequest{ID: "crash-delivery", CheckID: job.Runs[0].Checks["review"].Result.ID, Actor: "human"}
		m.replies = workflowReplies()
		store.failAt = store.saves + failAt
		_, _ = advance(t, c, store)
		store.failAt = 0
		_, _ = advance(t, c, store)
		if len(m.requests) > 6 || len(api.checks) > 4 {
			t.Fatal("replayed after interrupted save", failAt)
		}
	}
}

func TestEvaluationRetrySupersessionPreservesReview(t *testing.T) {
	m := &fakeModel{replies: workflowReplies()}
	api := &checkAPI{}
	c, store := checksCLI(m, api), &memoryWorkflow{}
	job, err := advance(t, c, store)
	if err != nil {
		t.Fatal(err)
	}
	c.Retry = &retryRequest{ID: "evaluation-retry", CheckID: job.Runs[0].Checks["evaluation"].Result.ID, Actor: "human"}
	m.replies = workflowReplies()[2:]
	job, err = advance(t, c, store)
	if err != nil {
		t.Fatal(err)
	}
	oldReview := job.Runs[0].Checks["review"].Result
	store.mutate(t, func(j *workflowJob) { j.Runs[1].Phase = workflowEvaluate })
	c.Retry = nil
	next := pr()
	next.Head = strings.Repeat("d", 40)
	c.Runner.GitHub = prFunc(func(context.Context, string, int) (PR, error) { return next, nil })
	m.replies = workflowReplies()
	_, err = c.advanceWorkflow(context.Background(), store, "o/r", next, policy(t), []byte(policyJSON), judgePolicy(t), []byte(evaluationPolicyJSON()), []string{"human"})
	if err != nil || api.checks[oldReview.ID] != oldReview {
		t.Fatal("source review mutated", err)
	}
}
