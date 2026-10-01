package review

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ericdmoore/ferretta/internal/github"
)

func TestChecksFailuresPreserveWorkAndVerdicts(t *testing.T) {
	for _, mode := range []string{"missing_adapter", "oversized", "reset_storage", "waiting", "superseded", "lost_create", "interrupted_judge", "corrupt_receipt", "corrupt_delivery"} {
		t.Run(mode, func(t *testing.T) {
			api := &checkAPI{}
			m := &fakeModel{replies: workflowReplies()}
			c := checksCLI(m, api)
			s := &memoryWorkflow{}
			if mode == "missing_adapter" || mode == "oversized" || mode == "reset_storage" {
				job := &workflowJob{Repository: "o/r", PR: 1, AppID: 7}
				run := &workflowRun{ID: "r", Session: managedSession{Session: Session{Report: Report{PR: pr()}}}}
				body := "Starting"
				if mode == "oversized" {
					body = strings.Repeat("x", 60001)
				}
				if mode == "missing_adapter" {
					c.Checks = nil
				}
				if mode == "reset_storage" {
					api.writeErr = github.ErrCheckNotDispatched
				}
				saves := 0
				save := func() error {
					saves++
					if mode == "reset_storage" && saves == 3 {
						return errors.New("disk")
					}
					return nil
				}
				if err := c.publishCheck(context.Background(), job, run, "review", "in_progress", "", "Starting", body, save); err == nil {
					t.Fatal("publication failure hidden")
				}
				return
			}
			if mode == "waiting" {
				api.rejectTitle = "Awaiting"
				m.replies = []Reply{reply("request_intent_confirmation", askJSON)}
			}
			if mode == "lost_create" {
				api.writeErr = errors.New("unknown outcome")
				api.drop = true
			}
			job, err := advance(t, c, s)
			if mode == "waiting" {
				if err == nil || api.posts != 1 || len(m.requests) != 1 {
					t.Fatal("waiting failure lost", err)
				}
				return
			}
			if mode == "lost_create" {
				api.writeErr = nil
				if _, err = advance(t, c, s); err == nil || len(api.writes) != 1 || len(m.requests) != 0 {
					t.Fatal("lost create repeated work", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "superseded":
				s.mutate(t, func(j *workflowJob) { j.Runs[0].Phase = workflowWaiting })
				api.rejectTitle = "Superseded"
				changed := pr()
				changed.Head = strings.Repeat("d", 40)
				_, err = c.advanceWorkflow(context.Background(), s, "o/r", changed, policy(t), []byte(policyJSON), judgePolicy(t), []byte(evaluationPolicyJSON()), []string{"human"})
				if err == nil || len(m.requests) != 3 {
					t.Fatal("supersession failure ignored")
				}
			case "corrupt_receipt", "corrupt_delivery":
				s.mutate(t, func(j *workflowJob) {
					e := j.Runs[0].Checks["review"]
					if mode == "corrupt_receipt" {
						e.Result.App.ID = 8
					} else {
						e.Delivery = "bad"
					}
				})
				if _, err = advance(t, c, s); err == nil || len(m.requests) != 3 {
					t.Fatal("invalid saved check accepted")
				}
			case "interrupted_judge":
				s.mutate(t, func(j *workflowJob) { j.Runs[0].Phase = workflowEvaluating })
				job, err = advance(t, c, s)
				if err != nil || len(m.requests) != 3 || !job.ConsumptionUncertain || job.Runs[0].Checks["review"].Result.Conclusion != "success" || job.Runs[0].Checks["evaluation"].Result.Conclusion != "action_required" {
					t.Fatal("interrupted judge changed verdict or replayed", err)
				}
			}
		})
	}
}
