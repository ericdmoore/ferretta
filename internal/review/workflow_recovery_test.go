package review

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ericdmoore/ferretta/internal/github"
)

func TestSupersededWorkRetainsUncertainty(t *testing.T) {
	for _, mode := range []string{"waiting", "running", "post-failure", "save-failure"} {
		t.Run(mode, func(t *testing.T) {
			m := &fakeModel{replies: []Reply{reply("request_intent_confirmation", askJSON)}}
			api := &proposalAPI{}
			c := CLI{Runner: *runner(m), Proposals: api}
			s := &memoryWorkflow{}
			if _, err := advance(t, c, s); err != nil {
				t.Fatal(err)
			}
			if mode == "running" {
				s.mutate(t, func(j *workflowJob) { j.Runs[0].Phase = workflowReviewRunning })
			}
			if mode == "post-failure" {
				api.postErr = errors.New("uncertain publication")
			}
			if mode == "save-failure" {
				s.failAt = s.saves + 4
			}
			newPR := pr()
			newPR.Head = strings.Repeat("e", 40)
			c.Runner.GitHub = prFunc(func(context.Context, string, int) (PR, error) { return newPR, nil })
			m.replies = workflowReplies()
			job, err := c.advanceWorkflow(context.Background(), s, "o/r", newPR, policy(t), []byte(policyJSON), judgePolicy(t), []byte(evaluationPolicyJSON()), []string{"human"})
			if mode == "waiting" {
				if err != nil || len(job.Runs) != 2 {
					t.Fatal(job, err)
				}
			} else if err == nil {
				t.Fatal("failure hidden", mode)
			}
			if mode == "running" && len(m.requests) != 1 {
				t.Fatal("uncertain prior spending admitted new work")
			}
		})
	}
}

func TestWorkflowPreDispatchAndHumanFailures(t *testing.T) {
	for _, mode := range []string{"changed-revision", "canceled", "human-poll", "marshal"} {
		t.Run(mode, func(t *testing.T) {
			m := &fakeModel{replies: []Reply{reply("request_intent_confirmation", askJSON)}}
			api := &proposalAPI{}
			c := CLI{Runner: *runner(m), Proposals: api}
			s := &memoryWorkflow{}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch mode {
			case "changed-revision":
				c.Runner.GitHub = prFunc(func(context.Context, string, int) (PR, error) {
					p := pr()
					p.Head = strings.Repeat("f", 40)
					return p, nil
				})
			case "canceled":
				cancel()
			case "human-poll":
				if _, err := advance(t, c, s); err != nil {
					t.Fatal(err)
				}
				api.readErr = errors.New("offline")
			case "marshal":
				c.Runner.Now = func() time.Time { return time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC) }
			}
			if _, err := c.advanceWorkflow(ctx, s, "o/r", pr(), policy(t), []byte(policyJSON), judgePolicy(t), []byte(evaluationPolicyJSON()), []string{"human"}); err == nil {
				t.Fatal("failure hidden", mode)
			}
		})
	}
}

type responsePublication struct {
	proposalAPI
	rewrite func(github.Comment) github.Comment
}

func (a *responsePublication) CreateComment(ctx context.Context, repo string, n int, body string) (github.Comment, error) {
	c, err := a.proposalAPI.CreateComment(ctx, repo, n, body)
	if a.rewrite != nil {
		c = a.rewrite(c)
	}
	return c, err
}

func TestPublicationRequiresVerifiedIdentity(t *testing.T) {
	for _, change := range []func(github.Comment) github.Comment{
		func(c github.Comment) github.Comment { c.ID = 0; return c },
		func(c github.Comment) github.Comment { c.User.Login = "other[bot]"; return c },
	} {
		api := &responsePublication{rewrite: change}
		c := CLI{Proposals: api}
		run := &workflowRun{ID: "run", Publications: map[string]*publication{}}
		if err := c.publishWorkflow(context.Background(), run, "start", "Starting", "o/r", 1, "ferretta[bot]", func() error { return nil }); err == nil || run.Publications["start"].Delivery != Uncertain {
			t.Fatal("unverified POST accepted")
		}
	}
	api := &proposalAPI{postErr: github.ErrCommentNotDispatched}
	c := CLI{Proposals: api}
	run := &workflowRun{ID: "run", Publications: map[string]*publication{}}
	saves := 0
	if err := c.publishWorkflow(context.Background(), run, "start", "Starting", "o/r", 1, "ferretta[bot]", func() error {
		saves++
		if saves == 3 {
			return errors.New("cannot persist retryable state")
		}
		return nil
	}); err == nil {
		t.Fatal("lost save failure")
	}
}

type cancelWriter struct{ cancel context.CancelFunc }

func (w cancelWriter) Write(b []byte) (int, error) { w.cancel(); return len(b), nil }

func TestWatchRecoveryAndScope(t *testing.T) {
	for _, mode := range []string{"wrong-target", "draft", "store-open", "store-write", "workflow-error", "stop-wait"} {
		t.Run(mode, func(t *testing.T) {
			t.Chdir(t.TempDir())
			_ = os.WriteFile("review.json", []byte(policyJSON), 0600)
			_ = os.WriteFile("judge.json", []byte(evaluationPolicyJSON()), 0600)
			state := filepath.Join(t.TempDir(), "state")
			args := []string{"--repo", "o/r", "--pr", "1", "--state", state, "--review-policy", "review.json", "--judge-policy", "judge.json", "--humans", "human", "--publication", "comments", "--once"}
			m := &fakeModel{replies: workflowReplies()}
			api := &watchAPI{pulls: []PR{pr()}}
			c := CLI{Runner: *runner(m), Proposals: api}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var out io.Writer = io.Discard
			switch mode {
			case "wrong-target":
				api.pulls[0].Number = 2
			case "draft":
				api.pulls[0].Draft = true
			case "store-open":
				_ = os.WriteFile(state, []byte("occupied"), 0600)
			case "store-write":
				api.afterPoll = func() {
					db, err := sql.Open("sqlite", filepath.Join(state, "state.sqlite"))
					if err != nil {
						t.Fatal(err)
					}
					defer db.Close()
					if _, err := db.Exec("DROP TABLE observations"); err != nil {
						t.Fatal(err)
					}
				}
			case "workflow-error":
				api.statusErr = errors.New("App unavailable")
			case "stop-wait":
				args = args[:len(args)-1]
				out = cancelWriter{cancel}
			}
			code := c.Watch(ctx, args, out, io.Discard)
			if mode == "wrong-target" || mode == "draft" {
				if code != 0 || len(m.requests) != 0 || api.posts != 0 {
					t.Fatal("unadmitted PR dispatched", code)
				}
			} else if mode == "stop-wait" {
				if code != 0 {
					t.Fatal(code)
				}
			} else if code != 1 {
				t.Fatal("failure hidden", mode, code)
			}
		})
	}
}
