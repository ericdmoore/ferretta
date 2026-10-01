package review

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ericdmoore/ferretta/internal/github"
)

// One effect describes either a create (TargetID == 0) or a complete replacement
// of a known check's output. Uncertain effects are immutable until reconciled.
type checkEffect struct {
	Request  github.CheckInput `json:"request"`
	TargetID int64             `json:"target_id"`
	Delivery Delivery          `json:"delivery"`
	Result   github.CheckRun   `json:"result"`
	At       time.Time         `json:"at"`
}

func publicationMode(mode string) string {
	if mode == "" {
		return "comments"
	} // Persisted v1 jobs predate Checks.
	return mode
}

func reconcileCheck(effect *checkEffect, observed []github.CheckRun, appID int64) (github.CheckRun, bool, error) {
	if effect == nil || effect.Request.Validate() != nil || effect.TargetID < 0 || appID <= 0 || (effect.Delivery != Draft && effect.Delivery != Uncertain && effect.Delivery != Posted) {
		return github.CheckRun{}, false, fmt.Errorf("invalid persisted check effect")
	}
	var found github.CheckRun
	for _, candidate := range observed {
		if candidate.App.ID != appID || candidate.ExternalID != effect.Request.ExternalID {
			continue
		}
		if found.ID != 0 {
			return github.CheckRun{}, false, fmt.Errorf("duplicate check identity; operator reconciliation required")
		}
		if candidate.ID <= 0 || candidate.URL == "" || candidate.CheckInput != effect.Request || (effect.TargetID != 0 && candidate.ID != effect.TargetID) {
			return github.CheckRun{}, false, fmt.Errorf("check content or revision differs; operator reconciliation required")
		}
		found = candidate
	}
	if found.ID != 0 {
		return found, true, nil
	}
	if effect.Delivery != Draft {
		return github.CheckRun{}, false, fmt.Errorf("check publication uncertain; no automatic redispatch")
	}
	return github.CheckRun{}, false, nil
}

func (c CLI) reconcileCheckEffect(ctx context.Context, repo string, effect *checkEffect, appID int64, save func() error) error {
	var observed []github.CheckRun
	var err error
	if effect.TargetID > 0 {
		var result github.CheckRun
		result, err = c.Checks.CheckRun(ctx, repo, effect.TargetID)
		observed = []github.CheckRun{result}
	} else {
		observed, err = c.Checks.CheckRuns(ctx, repo, effect.Request.Head)
	}
	if err != nil {
		return err
	}
	result, found, err := reconcileCheck(effect, observed, appID)
	if err != nil {
		return err
	}
	if found {
		effect.Result, effect.Delivery = result, Posted
		return save()
	}
	return nil
}

func (c CLI) publishCheck(ctx context.Context, job *workflowJob, run *workflowRun, role, status, conclusion, title, text string, save func() error) error {
	if c.Checks == nil || job.AppID <= 0 {
		return fmt.Errorf("Checks adapter and App identity required")
	}
	input := github.CheckInput{Name: "Ferretta / " + role, Head: run.Session.Report.PR.Head,
		ExternalID: hashBytes([]byte(run.ID + "/checks/" + role)), Status: status, Conclusion: conclusion,
		Output: github.CheckOutput{Title: title, Summary: fmt.Sprintf("PR #%d · head `%s` · base `%s`\n\nReviewer policy `%s`; judge policy `%s`.\n\n%s", job.PR, run.Session.Report.PR.Head, run.Session.Report.PR.Base, job.ReviewPolicy, job.JudgePolicy, title), Text: text}}
	if run.Retry != nil {
		input.Output.Summary += fmt.Sprintf("\n\nRetry `%s`, requested by `%s`, of Check `%d`. Previous attempt: `%s`. Allowances remain cumulative.", run.Retry.ID, run.Retry.Actor, run.Retry.CheckID, run.RetryParent)
	}
	if err := input.Validate(); err != nil {
		return err
	}
	if run.Checks == nil {
		run.Checks = map[string]*checkEffect{}
	}
	previous := run.Checks[role]
	if previous != nil && previous.Delivery == Uncertain {
		if err := c.reconcileCheckEffect(ctx, job.Repository, previous, job.AppID, save); err != nil {
			return err
		}
	}
	if previous != nil && previous.Delivery == Posted && previous.Request == input {
		return nil
	}
	if previous == nil || previous.Delivery == Posted {
		id := int64(0)
		if previous != nil {
			id = previous.Result.ID
		}
		previous = &checkEffect{Request: input, TargetID: id, Delivery: Draft, At: c.Runner.Now()}
		run.Checks[role] = previous
		if err := save(); err != nil {
			return err
		}
	}
	// A persisted draft must be delivered as recorded before replacing it with
	// newer progress. Discovery before the first POST catches prior publications.
	if previous.TargetID == 0 {
		if err := c.reconcileCheckEffect(ctx, job.Repository, previous, job.AppID, save); err != nil {
			return err
		}
	}
	if previous.Delivery != Posted {
		previous.Delivery = Uncertain
		if err := save(); err != nil {
			return err
		}
		result, err := c.Checks.WriteCheck(ctx, job.Repository, previous.TargetID, previous.Request)
		if err != nil {
			if errors.Is(err, github.ErrCheckNotDispatched) {
				previous.Delivery = Draft
				if err := save(); err != nil {
					return err
				}
			}
			return err
		}
		confirmed, _, err := reconcileCheck(previous, []github.CheckRun{result}, job.AppID)
		if err != nil {
			return err
		}
		previous.Result, previous.Delivery = confirmed, Posted
		if err := save(); err != nil {
			return err
		}
	}
	if previous.Request != input {
		return c.publishCheck(ctx, job, run, role, status, conclusion, title, text, save)
	}
	return nil
}

// Public progress contains only harness-owned operation names and accounting.
// It never includes prompts, thinking, tool arguments/results or credential data.
func progressText(events []UsageEvent) string {
	var out strings.Builder
	start := max(0, len(events)-12)
	for _, event := range events[start:] {
		operation := event.Operation
		switch operation {
		case "review", "judge", "read_file", "read_diff", "list_files", "search", "grep", "run_checks", "read_evidence", "finish_review", "finish_evaluation", "request_intent_confirmation":
		default:
			operation = "unrecognized tool"
		}
		state := "completed"
		if event.Outcome == "pending" {
			state = "awaiting response"
		} else if event.Outcome != "completed" {
			state = "failed or incomplete"
		}
		fmt.Fprintf(&out, "- %s: %s (%.2fs recorded execution)\n", operation, state, event.Duration.Seconds())
	}
	return out.String()
}

func (c CLI) publishProgress(ctx context.Context, job *workflowJob, run *workflowRun, role, plan string, events []UsageEvent, save func() error) error {
	if publicationMode(job.Publication) != "checks" {
		return nil
	}
	now := c.Runner.Now()
	if previous := run.Checks[role]; previous != nil && previous.Delivery == Posted && previous.Request.Status == "in_progress" && previous.Request.Output.Title == "Running "+role && elapsed(previous.At, now) < 10*time.Second {
		return nil
	}
	text := plan + "\n\n### Recent activity\nLast checkpoint: " + now.UTC().Format(time.RFC3339) + "\n\n" + progressText(events) + "\nUpdates occur at model/tool checkpoints. A long model call may have no new checkpoint; this is not a heartbeat or a streaming transcript.\n"
	if c.Progress != nil {
		fmt.Fprintf(c.Progress, "Ferretta %s checkpoint %s\n%s", role, now.UTC().Format(time.RFC3339), progressText(events))
	}
	return c.publishCheck(ctx, job, run, role, "in_progress", "", "Running "+role, text, save)
}

func reviewConclusion(status string) string {
	switch status {
	case "lgtm":
		return "success"
	case "changes_required":
		return "failure"
	default:
		return "action_required"
	}
}

func (c CLI) publishMilestone(ctx context.Context, job *workflowJob, run *workflowRun, kind, body string, save func() error) error {
	if publicationMode(job.Publication) == "comments" {
		if kind == "waiting" {
			return nil
		} // The proposal already carries the question.
		return c.publishWorkflow(ctx, run, kind, body, job.Repository, job.PR, job.Bot, save)
	}
	switch kind {
	case "starting":
		if err := c.publishCheck(ctx, job, run, "review", "in_progress", "", "Preparing review", body, save); err != nil {
			return err
		}
		return c.publishCheck(ctx, job, run, "evaluation", "queued", "", "Waiting for review output", body, save)
	case "waiting":
		return c.publishCheck(ctx, job, run, "review", "in_progress", "", "Awaiting human intent confirmation; compute paused", body, save)
	case "verdict":
		return c.publishCheck(ctx, job, run, "review", "completed", reviewConclusion(run.Session.Report.Status), "Review: "+run.Session.Report.Status, body, save)
	case "scorecard":
		conclusion, title := "neutral", "Assessment complete; grades are advisory"
		if run.Evaluation.Assessment == nil {
			conclusion, title = "action_required", "Evaluation incomplete; review verdict preserved"
		}
		return c.publishCheck(ctx, job, run, "evaluation", "completed", conclusion, title, body, save)
	default: // Superseded revision: both old checks become terminal.
		for _, role := range []string{"review", "evaluation"} {
			if role == "review" && run.ReviewSource != "" {
				continue // Evaluation retries never mutate the source review Check.
			}
			if err := c.publishCheck(ctx, job, run, role, "completed", "cancelled", "Superseded revision", body, save); err != nil {
				return err
			}
		}
		return nil
	}
}
