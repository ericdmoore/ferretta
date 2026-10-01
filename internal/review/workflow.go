package review

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ericdmoore/ferretta/internal/github"
	"github.com/ericdmoore/ferretta/internal/intent"
)

type workflowStore interface {
	Review(context.Context, string) ([]byte, error)
	SaveReview(context.Context, string, []byte) error
}

type workflowPhase string

const (
	workflowStart         workflowPhase = "starting"
	workflowReview        workflowPhase = "review_ready"
	workflowReviewRunning workflowPhase = "review_running"
	workflowWaiting       workflowPhase = "awaiting_intent"
	workflowVerdict       workflowPhase = "verdict_pending"
	workflowEvaluate      workflowPhase = "evaluation_ready"
	workflowEvaluating    workflowPhase = "evaluation_running"
	workflowScorecard     workflowPhase = "scorecard_pending"
	workflowDone          workflowPhase = "complete"
)

type publication struct {
	ID       string         `json:"id"`
	Body     string         `json:"body"`
	Delivery Delivery       `json:"delivery"`
	Comment  github.Comment `json:"comment"`
}

type workflowRun struct {
	ID                 string                  `json:"id"`
	Phase              workflowPhase           `json:"phase"`
	Session            managedSession          `json:"session"`
	Evaluation         *Evaluation             `json:"evaluation,omitempty"`
	EvaluationMessages []Message               `json:"evaluation_messages,omitempty"`
	Publications       map[string]*publication `json:"publications"`
	Checks             map[string]*checkEffect `json:"checks,omitempty"`
}

// Each PR owns cumulative allowances and all revision attempts. New commits do
// not reset resource use. Persisted phases are validated before dispatch.
type workflowJob struct {
	Version              int            `json:"version"`
	Repository           string         `json:"repository"`
	PR                   int            `json:"pr"`
	Bot                  string         `json:"bot"`
	Humans               []string       `json:"humans"`
	ReviewPolicy         string         `json:"review_policy_sha256"`
	JudgePolicy          string         `json:"judge_policy_sha256"`
	Publication          string         `json:"publication,omitempty"`
	AppID                int64          `json:"app_id,omitempty"`
	ReviewNanos          int64          `json:"review_active_ns"`
	EvaluationNanos      int64          `json:"evaluation_active_ns"`
	ConsumptionUncertain bool           `json:"consumption_uncertain"`
	Runs                 []*workflowRun `json:"runs"`
}

func (c CLI) advanceWorkflow(ctx context.Context, store workflowStore, repo string, pr PR, reviewPolicy Policy, reviewBytes []byte, judgePolicy EvaluationPolicy, judgeBytes []byte, humans []string) (*workflowJob, error) {
	if c.Proposals == nil {
		return nil, fmt.Errorf("GitHub App publication adapter required")
	}
	identity, err := c.Proposals.Status(ctx, repo)
	if err != nil {
		return nil, err
	}
	mode := publicationMode(c.PublicationMode)
	if mode != "comments" && mode != "checks" {
		return nil, fmt.Errorf("publication must be checks or comments")
	}
	if mode == "checks" && (c.Checks == nil || identity.AppID <= 0) {
		return nil, fmt.Errorf("Checks publication requires a Checks adapter and App identity")
	}
	if err := (intent.Policy{Humans: humans, Agents: []string{identity.BotLogin}}).Validate(); err != nil {
		return nil, err
	}
	key := fmt.Sprintf("workflow-v1:%s:%d", strings.ToLower(repo), pr.Number)
	job := &workflowJob{Version: 1, Repository: repo, PR: pr.Number, Bot: identity.BotLogin, Humans: humans, ReviewPolicy: hashBytes(reviewBytes), JudgePolicy: hashBytes(judgeBytes)}
	job.Publication, job.AppID = mode, identity.AppID
	data, err := store.Review(ctx, key)
	if err == nil {
		if err := json.Unmarshal(data, job); err != nil {
			return nil, err
		}
		// Decode legacy omitted fields as legacy values, not current defaults.
		var stored struct {
			Publication string `json:"publication"`
			AppID       int64  `json:"app_id"`
		}
		_ = json.Unmarshal(data, &stored)
		job.Publication, job.AppID = publicationMode(stored.Publication), stored.AppID
		if job.Publication != mode || (mode == "checks" && job.AppID != identity.AppID) {
			return nil, fmt.Errorf("publication mode or App identity changed; resume legacy jobs with --publication comments; operator reconciliation required")
		}
		if job.Version != 1 || job.Repository != repo || job.PR != pr.Number || job.Bot != identity.BotLogin || job.ReviewPolicy != hashBytes(reviewBytes) || job.JudgePolicy != hashBytes(judgeBytes) || strings.Join(job.Humans, ",") != strings.Join(humans, ",") || job.ReviewNanos < 0 || job.EvaluationNanos < 0 {
			return nil, fmt.Errorf("workflow identity, policies, human allowlist, or accounting changed; operator reconciliation required")
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	save := func() error {
		data, err := json.Marshal(job)
		if err != nil {
			return err
		}
		persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		return store.SaveReview(persist, key, data)
	}
	var run *workflowRun
	if len(job.Runs) > 0 {
		run = job.Runs[len(job.Runs)-1]
	}
	for _, previous := range job.Runs {
		for _, role := range []string{"review", "evaluation"} {
			effect := previous.Checks[role]
			if effect == nil {
				continue
			}
			if effect.Delivery == Posted {
				if _, _, err := reconcileCheck(effect, []github.CheckRun{effect.Result}, job.AppID); err != nil {
					return job, err
				}
			} else if effect.Delivery == Uncertain {
				if err := c.reconcileCheckEffect(ctx, repo, effect, job.AppID, save); err != nil {
					return job, err
				}
			} else if effect.Delivery != Draft || effect.Request.Validate() != nil {
				return job, fmt.Errorf("invalid persisted check state")
			}
		}
	}
	if run != nil && (run.Session.Report.PR.Head != pr.Head || run.Session.Report.PR.Base != pr.Base) {
		// A prior in-flight operation must be reconciled before more work, even
		// when the PR has advanced. Completed history remains available.
		if run.Phase == workflowReviewRunning || run.Phase == workflowEvaluating {
			job.ConsumptionUncertain = true
		}
		if run.Phase != workflowDone {
			if err := c.publishMilestone(ctx, job, run, "superseded", fmt.Sprintf("Ferretta run `%s` applies to commit `%s` and has been superseded by `%s`. It does not approve the new revision.", run.ID, run.Session.Report.PR.Head, pr.Head), save); err != nil {
				return job, err
			}
			run.Phase = workflowDone
			if err := save(); err != nil {
				return job, err
			}
		}
		run = nil
	}
	if run == nil {
		// A force-push back to an already observed revision is a delivery of
		// existing work, not permission to spend again under the same policy.
		for _, previous := range job.Runs {
			if previous.Session.Report.PR.Head == pr.Head && previous.Session.Report.PR.Base == pr.Base {
				return job, nil
			}
		}
		if job.ConsumptionUncertain {
			return job, fmt.Errorf("prior model consumption is uncertain; no new dispatch")
		}
		run = &workflowRun{ID: hashBytes([]byte(key + pr.Head + pr.Base + job.ReviewPolicy + job.JudgePolicy)), Phase: workflowStart, Publications: map[string]*publication{}, Session: managedSession{Repository: repo, Humans: humans, Bot: job.Bot, Session: Session{Report: Report{PR: pr, Status: "incomplete", PolicySHA256: job.ReviewPolicy}}}}
		job.Runs = append(job.Runs, run)
		if err := save(); err != nil {
			return job, err
		}
	}
	if run.Publications == nil || run.ID == "" {
		return job, fmt.Errorf("invalid persisted workflow")
	}
	for {
		if err := ctx.Err(); err != nil {
			return job, err
		}
		switch run.Phase {
		case workflowStart:
			body := startingComment(pr, run.ID, reviewPolicy, judgePolicy, job.ReviewNanos, job.EvaluationNanos)
			body += fmt.Sprintf("\nTrusted policy SHA-256: reviewer `%s`; judge `%s`.\n", job.ReviewPolicy, job.JudgePolicy)
			if err := c.publishMilestone(ctx, job, run, "starting", body, save); err != nil {
				return job, err
			}
			run.Phase = workflowReview
		case workflowReviewRunning:
			job.ConsumptionUncertain = true
			run.Session.Report.Status = "incomplete"
			run.Session.Report.Summary = "Review interrupted with uncertain model consumption. Automatic replay is disabled."
			run.Session.Report.FinishedAt = c.Runner.Now()
			run.Phase = workflowVerdict
		case workflowReview, workflowWaiting:
			latest, err := c.Runner.PR(ctx, repo, pr.Number)
			if err != nil {
				return job, err
			}
			if latest.Head != pr.Head || latest.Base != pr.Base {
				return job, fmt.Errorf("PR changed before review dispatch; waiting for next poll")
			}
			if run.Phase == workflowWaiting {
				if len(run.Session.Report.Proposals) == 0 {
					return job, fmt.Errorf("waiting workflow has no proposal")
				}
				ready, err := c.resolveProposal(ctx, &run.Session, save)
				if err != nil {
					return job, err
				}
				if !ready {
					if err := c.publishMilestone(ctx, job, run, "waiting", "Awaiting authenticated human intent confirmation.\n\n"+run.Session.Report.Question, save); err != nil {
						return job, err
					}
					return job, nil
				}
			}
			remaining, err := remainingPolicy(reviewPolicy, job.ReviewNanos)
			if err != nil {
				run.Session.Report.Status = "incomplete"
				run.Session.Report.Summary = err.Error()
				run.Session.Report.FinishedAt = c.Runner.Now()
				run.Phase = workflowVerdict
				break
			}
			started := c.Runner.Now()
			before := job.ReviewNanos
			run.Phase = workflowReviewRunning
			if err := save(); err != nil {
				return job, err
			}
			if err := c.executeWorkflowReview(ctx, run, remaining, reviewBytes, func(report Report, messages []Message) error {
				run.Session.Session = Session{Report: report, Messages: messages}
				job.ReviewNanos = before + int64(elapsed(started, c.Runner.Now()))
				if err := save(); err != nil {
					return err
				}
				return c.publishProgress(ctx, job, run, "review", startingComment(pr, run.ID, reviewPolicy, judgePolicy, job.ReviewNanos, job.EvaluationNanos), report.Usage, save)
			}); err != nil {
				run.Session.Report.Status = "incomplete"
				run.Session.Report.Summary = err.Error()
				run.Session.Report.FinishedAt = c.Runner.Now()
			}
			job.ReviewNanos = before + int64(elapsed(started, c.Runner.Now()))
			latest, err = c.Runner.PR(ctx, repo, pr.Number)
			if err != nil || latest.Head != pr.Head || latest.Base != pr.Base {
				run.Session.Report.Status = "incomplete"
				run.Session.Report.Summary = "PR revision could not be revalidated after review"
			}
			run.Phase = workflowVerdict
			if run.Session.Report.Status == "awaiting_intent" {
				run.Phase = workflowWaiting
			}
		case workflowVerdict:
			if err := c.publishMilestone(ctx, job, run, "verdict", verdictComment(run.Session.Report), save); err != nil {
				return job, err
			}
			run.Phase = workflowEvaluate
		case workflowEvaluating:
			job.ConsumptionUncertain = true
			if run.Evaluation == nil {
				run.Evaluation = &Evaluation{ID: run.ID + "/evaluation", Head: pr.Head, Base: pr.Base, ReviewStatus: run.Session.Report.Status, Rubric: rubricVersion}
			}
			run.Evaluation.Assessment = nil
			run.Evaluation.Failure = "Evaluation interrupted; provider consumption may be unknown. Automatic replay is disabled."
			run.Evaluation.FinishedAt = c.Runner.Now()
			run.Phase = workflowScorecard
		case workflowEvaluate:
			if mode == "checks" {
				if err := c.publishCheck(ctx, job, run, "evaluation", "in_progress", "", "Evaluating review output", startingComment(pr, run.ID, reviewPolicy, judgePolicy, job.ReviewNanos, job.EvaluationNanos), save); err != nil {
					return job, err
				}
			}
			snapshot, err := SnapshotReview(run.Session.Report)
			remaining, limitErr := remainingPolicy(judgePolicy.route, job.EvaluationNanos)
			if err != nil || limitErr != nil || job.ConsumptionUncertain {
				run.Evaluation = &Evaluation{ID: run.ID + "/evaluation", Head: pr.Head, Base: pr.Base, ReviewStatus: run.Session.Report.Status, Rubric: rubricVersion, RequestedModel: judgePolicy.route.config.Model, Provider: judgePolicy.route.config.Provider, Failure: fmt.Sprintf("Evaluation not admitted: snapshot=%v; allowance=%v; uncertain consumption=%t", err, limitErr, job.ConsumptionUncertain)}
				run.Phase = workflowScorecard
				break
			}
			run.Phase = workflowEvaluating
			if err := save(); err != nil {
				return job, err
			}
			started := c.Runner.Now()
			before := job.EvaluationNanos
			eval := c.Runner.Evaluate(ctx, EvaluationPolicy{route: remaining}, snapshot, judgeBytes, run.ID+"/evaluation", ".", func(e Evaluation, messages []Message) error {
				run.Evaluation = &e
				run.EvaluationMessages = messages
				job.EvaluationNanos = before + int64(elapsed(started, c.Runner.Now()))
				if err := save(); err != nil {
					return err
				}
				return c.publishProgress(ctx, job, run, "evaluation", startingComment(pr, run.ID, reviewPolicy, judgePolicy, job.ReviewNanos, job.EvaluationNanos), e.Usage, save)
			})
			run.Evaluation = &eval
			job.EvaluationNanos = before + int64(elapsed(started, c.Runner.Now()))
			run.Phase = workflowScorecard
		case workflowScorecard:
			if run.Evaluation == nil {
				return job, fmt.Errorf("scorecard stage has no evaluation outcome")
			}
			body := EvaluationMarkdown(*run.Evaluation)
			if p := run.Publications["verdict"]; p != nil {
				body += "\nReviewed output: " + p.Comment.URL + "\n"
			}
			if p := run.Checks["review"]; p != nil {
				body += "\nReviewed output: " + p.Result.URL + "\n"
			}
			if run.Evaluation.RequestedModel == run.Session.Report.RequestedModel {
				body += "\nReviewer and judge use the same model in separate sessions; this is not an independent model opinion.\n"
			}
			if err := c.publishMilestone(ctx, job, run, "scorecard", body, save); err != nil {
				return job, err
			}
			run.Phase = workflowDone
		case workflowDone:
			return job, nil
		default:
			return job, fmt.Errorf("unsupported workflow phase %q", run.Phase)
		}
		if err := save(); err != nil {
			return job, err
		}
	}
}

func remainingPolicy(p Policy, used int64) (Policy, error) {
	if p.config.TimeoutSeconds > 0 {
		remaining := int64(p.config.TimeoutSeconds)*int64(time.Second) - used
		if remaining <= 0 {
			return Policy{}, fmt.Errorf("active-time allowance exhausted")
		}
		// Round down so repeated resumptions cannot replenish allowance.
		p.config.TimeoutSeconds = int(remaining / int64(time.Second))
		if p.config.TimeoutSeconds == 0 {
			return Policy{}, fmt.Errorf("less than one second of active allowance remains")
		}
	}
	return p, nil
}

func (c CLI) executeWorkflowReview(ctx context.Context, run *workflowRun, p Policy, policyBytes []byte, checkpoint func(Report, []Message) error) error {
	ctx, cancel := reviewContext(ctx, p)
	defer cancel()
	workspace, err := os.MkdirTemp("", "ferretta-watch-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workspace)
	w, err := c.Runner.Prepare(ctx, run.Session.Repository, run.Session.Report.PR, workspace)
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = c.Runner.Exec.Run(cleanup, "", "git", "worktree", "remove", "--force", workspace)
	}()
	if len(run.Session.Messages) == 0 {
		run.Session.Report = c.Runner.Review(ctx, p, w, policyBytes, checkpoint)
	} else {
		run.Session.Report = c.Runner.continueReview(ctx, p, w, run.Session.Session, checkpoint)
	}
	return nil
}

func (c CLI) publishWorkflow(ctx context.Context, run *workflowRun, kind, body, repo string, number int, bot string, save func() error) error {
	p := run.Publications[kind]
	if p == nil {
		id := hashBytes([]byte(run.ID + "/" + kind))
		p = &publication{ID: id, Body: "<!-- ferretta-effect:" + id + " -->\n" + body, Delivery: Draft}
		if len(p.Body) > 60000 {
			return fmt.Errorf("milestone comment exceeds 60000 bytes")
		}
		run.Publications[kind] = p
		if err := save(); err != nil {
			return err
		}
	}
	if p.Delivery == Posted {
		return nil
	}
	comments, err := c.Proposals.Comments(ctx, repo, number)
	if err != nil {
		return err
	}
	reconciled, err := ReconcileProposal(Proposal{EffectID: p.ID, Body: p.Body, Delivery: p.Delivery, Comment: p.Comment}, comments, bot)
	if err != nil {
		return err
	}
	p.Delivery, p.Comment = reconciled.Delivery, reconciled.Comment
	if p.Delivery == Posted {
		return save()
	}
	if p.Delivery != Draft {
		return fmt.Errorf("milestone publication uncertain; reconcile without reposting")
	}
	p.Delivery = Uncertain
	if err := save(); err != nil {
		return err
	}
	comment, err := c.Proposals.CreateComment(ctx, repo, number, p.Body)
	if err != nil {
		if errors.Is(err, github.ErrCommentNotDispatched) {
			p.Delivery = Draft
			if err := save(); err != nil {
				return err
			}
		}
		return err
	}
	reconciled, err = ReconcileProposal(Proposal{EffectID: p.ID, Body: p.Body, Delivery: p.Delivery}, []github.Comment{comment}, bot)
	if err != nil {
		return err
	}
	if reconciled.Delivery != Posted {
		return fmt.Errorf("GitHub did not establish milestone publication")
	}
	p.Delivery, p.Comment = Posted, reconciled.Comment
	return save()
}

func startingComment(pr PR, id string, review Policy, judge EvaluationPolicy, reviewUsed, judgeUsed int64) string {
	limits := func(p Policy, used int64) string {
		turns, deadline := "unlimited", "unlimited"
		if p.config.MaxTurns > 0 {
			turns = fmt.Sprint(p.config.MaxTurns)
		}
		if p.config.TimeoutSeconds > 0 {
			deadline = fmt.Sprintf("%ds total; %.2fs previously consumed", p.config.TimeoutSeconds, float64(used)/1e9)
		}
		return fmt.Sprintf("%q via %s; requested effort: %q; requested thinking: %q; effective effort: not reported.\n- Turns: %s; context: %d tokens; generation per turn: %d tokens.\n- Active-stage allowance (including setup overhead, excluding human waits): %s.\n", p.config.Model, p.config.Provider, p.config.Effort, p.config.Thinking, turns, p.config.ContextTokens, p.config.MaxTokens, deadline)
	}
	return fmt.Sprintf("## Ferretta has started reviewing this PR\n\nCommit: `%s`; base: `%s`; run: `%s`.\n\n### Assigned reviewer\n%s\n### Scorecard judge\n%s\nPaid routes: disabled (local-only). Repair cycles: disabled. PR-age deadline: not configured.\n\nPlan: inspect the submitted commits, investigate material issues, run trusted checks, and publish a verdict. Consequential intent questions pause for an allowlisted human. Then a separate judge session publishes worker and oversight grades. Allocation follows core policy; reaching a limit does not mean approval.\n", pr.Head, pr.Base, id, limits(review, reviewUsed), limits(judge.route, judgeUsed))
}

func verdictComment(report Report) string {
	var text bytes.Buffer
	_ = PrintReport(&text, report, "text")
	for _, result := range report.Checks {
		command, _, _ := strings.Cut(result, "\n")
		fmt.Fprintf(&text, "\nRecorded check: %s\n", command)
	}
	if report.CheckFailure != "" {
		fmt.Fprintf(&text, "\nCheck failure: %s\n", report.CheckFailure)
	} else {
		fmt.Fprintln(&text, "\nCheck failure: none recorded (see verdict and executed-check count).")
	}
	return "## Ferretta review verdict\n\n" + quoted(text.String()) + "\n\nThis result applies only to the named revision. Scorecard evaluation follows separately.\n" + usageMarkdown("Review", report.Usage)
}
