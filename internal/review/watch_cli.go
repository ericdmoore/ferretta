package review

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ericdmoore/ferretta/internal/github"
	"github.com/ericdmoore/ferretta/internal/service"
)

// Watch is explicit opt-in dispatch for one PR. Existing service run remains
// intake-only. A missing/draft target waits without inference or publication.
func (c CLI) Watch(ctx context.Context, args []string, output, stderr io.Writer) int {
	fail := func(err error) int { fmt.Fprintln(stderr, err); return 1 }
	flags := flag.NewFlagSet("service watch", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repo := flags.String("repo", "", "GitHub owner/repository")
	number := flags.Int("pr", 0, "only PR allowed to execute; may not exist yet")
	state := flags.String("state", "", "absolute private installation state directory")
	reviewPath := flags.String("review-policy", "", "trusted reviewer JSON policy")
	judgePath := flags.String("judge-policy", "", "trusted read-only judge JSON policy")
	humans := flags.String("humans", "", "comma-separated allowlisted human logins")
	interval := flags.Duration("interval", time.Minute, "delay between polls (5s–24h)")
	once := flags.Bool("once", false, "poll and advance permitted workflow stages once")
	publication := flags.String("publication", "checks", "checks or comments; pinned for the PR workflow")
	if err := flags.Parse(args); err != nil {
		return 1
	}
	if (*publication != "checks" && *publication != "comments") || (*publication == "checks" && c.Checks == nil) {
		return fail(fmt.Errorf("--publication requires checks (with a Checks adapter) or comments"))
	}
	c.PublicationMode, c.Progress = *publication, stderr
	source, ok := c.Proposals.(service.Source)
	if flags.NArg() != 0 || !github.ValidRepository(*repo) || *number <= 0 || *state == "" || *reviewPath == "" || *judgePath == "" || *humans == "" || *interval < 5*time.Second || *interval > 24*time.Hour || !ok {
		return fail(fmt.Errorf("watch requires --repo, --pr, --state, --review-policy, --judge-policy, --humans, a valid interval and an App connection"))
	}
	reviewBytes, err := os.ReadFile(*reviewPath)
	if err != nil {
		return fail(err)
	}
	p, err := ParsePolicy(reviewBytes)
	if err != nil {
		return fail(err)
	}
	judgeBytes, err := os.ReadFile(*judgePath)
	if err != nil {
		return fail(err)
	}
	judge, err := ParseEvaluationPolicy(judgeBytes)
	if err != nil {
		return fail(err)
	}
	store, err := service.OpenStore(*state)
	if err != nil {
		return fail(err)
	}
	defer store.Close()
	fmt.Fprintf(stderr, "Ferretta watch: automatic review, proposals, verdict and scorecard enabled for %s PR #%d only; publication: %s.\n", *repo, *number, *publication)
	for {
		if ctx.Err() != nil {
			return 0
		}
		pollCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		pulls, err := source.OpenPullRequests(pollCtx, *repo)
		cancel()
		if err != nil {
			fmt.Fprintln(stderr, "Poll failed:", err)
			if *once {
				return 1
			}
		} else {
			command, err := service.PlanSnapshot(*repo, pulls, c.Runner.Now())
			if err != nil {
				return fail(err)
			}
			if err := store.Record(ctx, command); err != nil {
				return fail(err)
			}
			for _, pr := range pulls {
				if pr.Number != *number || pr.Draft {
					continue
				}
				job, err := c.advanceWorkflow(ctx, store, *repo, pr, p, reviewBytes, judge, judgeBytes, strings.Split(*humans, ","))
				if err != nil {
					return fail(err)
				}
				if err := json.NewEncoder(output).Encode(workflowStatus(job)); err != nil {
					return fail(err)
				}
			}
		}
		if *once {
			return 0
		}
		if err := service.Wait(ctx, *interval); err != nil {
			return 0
		}
	}
}

// Status intentionally excludes private continuations, local paths and evidence
// bodies. The persisted job retains full recovery state under mode 0600.
func workflowStatus(job *workflowJob) any {
	type revision struct {
		Head     string            `json:"head"`
		Phase    workflowPhase     `json:"phase"`
		Comments map[string]string `json:"comments"`
		Checks   map[string]string `json:"checks,omitempty"`
	}
	runs := []revision{}
	for _, run := range job.Runs {
		r := revision{Head: run.Session.Report.PR.Head, Phase: run.Phase, Comments: map[string]string{}, Checks: map[string]string{}}
		for kind, p := range run.Publications {
			if p.Delivery == Posted {
				r.Comments[kind] = p.Comment.URL
			}
		}
		for role, effect := range run.Checks {
			if effect.Delivery == Posted {
				r.Checks[role] = effect.Result.URL
			}
		}
		runs = append(runs, r)
	}
	return struct {
		Repository        string     `json:"repository"`
		PR                int        `json:"pr"`
		Runs              []revision `json:"runs"`
		ReviewSeconds     float64    `json:"review_active_seconds"`
		EvaluationSeconds float64    `json:"evaluation_active_seconds"`
		Uncertain         bool       `json:"consumption_uncertain"`
	}{job.Repository, job.PR, runs, float64(job.ReviewNanos) / 1e9, float64(job.EvaluationNanos) / 1e9, job.ConsumptionUncertain}
}
