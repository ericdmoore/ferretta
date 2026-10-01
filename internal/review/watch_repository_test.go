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

	"github.com/ericdmoore/ferretta/internal/service"
)

func trustedPR(number int) PR {
	p := pr()
	p.Number, p.Author, p.HeadRepository = number, "human", "o/r"
	p.URL = fmt.Sprintf("https://github.com/o/r/pull/%d", number)
	return p
}

func repositoryWatch(t *testing.T, model *fakeModel, api *checkAPI) (CLI, []string) {
	t.Helper()
	t.Chdir(t.TempDir())
	if err := os.WriteFile("review.json", []byte(policyJSON), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("judge.json", []byte(evaluationPolicyJSON()), 0600); err != nil {
		t.Fatal(err)
	}
	c := checksCLI(model, api)
	c.Runner.GitHub = prFunc(func(_ context.Context, _ string, number int) (PR, error) {
		for _, p := range api.pulls {
			if p.Number == number {
				return p, nil
			}
		}
		return PR{}, errors.New("PR not found")
	})
	args := []string{"--repo", "o/r", "--all-prs", "--authors", "human", "--state", filepath.Join(t.TempDir(), "state"), "--review-policy", "review.json", "--judge-policy", "judge.json", "--humans", "human", "--once"}
	return c, args
}

func TestRepositoryWatchAdmissionAndRestart(t *testing.T) {
	api := &checkAPI{}
	m := &fakeModel{}
	c, args := repositoryWatch(t, m, api)
	poll := func() {
		t.Helper()
		var errs bytes.Buffer
		if code := c.Watch(context.Background(), args, io.Discard, &errs); code != 0 {
			t.Fatal(code, errs.String())
		}
	}
	poll() // Start before any PR exists.
	draft, fork, stranger, unknown := trustedPR(1), trustedPR(2), trustedPR(3), trustedPR(4)
	draft.Draft, fork.HeadRepository, stranger.Author, unknown.HeadRepository = true, "fork/r", "stranger", ""
	api.pulls = []PR{draft, fork, stranger, unknown}
	poll()
	if len(m.requests) != 0 || len(api.writes) != 0 {
		t.Fatal("unadmitted PR executed")
	}
	api.pulls[0].Draft = false
	m.replies = workflowReplies()
	poll()
	if len(m.requests) != 3 || len(api.checks) != 2 {
		t.Fatal("ready PR not admitted", len(m.requests), len(api.checks))
	}
	poll() // A restarted watcher uses exactly the same durable job.
	if len(m.requests) != 3 {
		t.Fatal("restart spent again")
	}
	api.pulls = append(api.pulls, trustedPR(5))
	m.replies = workflowReplies()
	poll()
	if len(m.requests) != 6 || len(api.checks) != 4 {
		t.Fatal("later PR not discovered")
	}
	api.pulls[0].Head = strings.Repeat("d", 40)
	m.replies = workflowReplies()
	poll()
	if len(m.requests) != 9 || len(api.checks) != 6 {
		t.Fatal("new commit not reviewed")
	}
	poll()
	if len(m.requests) != 9 {
		t.Fatal("new revision replayed")
	}
}

func TestRepositoryWatchIsolationAndRevalidation(t *testing.T) {
	for _, mode := range []string{"lookup-error", "changed-author", "fork-before-dispatch", "human-wait", "continuous-error"} {
		t.Run(mode, func(t *testing.T) {
			api := &checkAPI{pulls: []PR{trustedPR(1), trustedPR(2)}}
			m := &fakeModel{replies: workflowReplies()}
			c, args := repositoryWatch(t, m, api)
			calls := 0
			if mode == "human-wait" {
				m.replies = append([]Reply{reply("request_intent_confirmation", askJSON)}, workflowReplies()...)
			} else {
				c.Runner.GitHub = prFunc(func(_ context.Context, _ string, number int) (PR, error) {
					p := trustedPR(number)
					if number == 1 {
						calls++
						switch mode {
						case "lookup-error", "continuous-error":
							return PR{}, errors.New("offline")
						case "changed-author":
							p.Author = "stranger"
						case "fork-before-dispatch":
							if calls > 1 {
								p.HeadRepository = "fork/r"
							}
						}
					}
					return p, nil
				})
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var out, errs bytes.Buffer
			var writer io.Writer = &out
			if mode == "continuous-error" {
				args = args[:len(args)-1]
				writer = cancelWriter{cancel}
			}
			code := c.Watch(ctx, args, writer, &errs)
			want := 1
			if mode == "human-wait" || mode == "continuous-error" {
				want = 0
			}
			if code != want {
				t.Fatal(code, errs.String())
			}
			wantCalls := 3
			if mode == "human-wait" {
				wantCalls++
			}
			if len(m.requests) != wantCalls {
				t.Fatal("one PR blocked another or unadmitted code ran", len(m.requests), errs.String())
			}
			if mode != "continuous-error" && !strings.Contains(out.String(), `"pr":2`) {
				t.Fatal("second PR not completed", out.String())
			}
		})
	}
}

func TestRepositoryRetryRoutesToItsOwnPR(t *testing.T) {
	api := &checkAPI{pulls: []PR{trustedPR(1), trustedPR(2)}}
	m := &fakeModel{replies: append(workflowReplies(), workflowReplies()...)}
	c, args := repositoryWatch(t, m, api)
	if code := c.Watch(context.Background(), args, io.Discard, io.Discard); code != 0 {
		t.Fatal(code)
	}
	state := args[6]
	s, err := service.OpenStore(state)
	if err != nil {
		t.Fatal(err)
	}
	data, err := s.Review(context.Background(), workflowKey("o/r", 2))
	if err != nil {
		t.Fatal(err)
	}
	var job workflowJob
	if err := json.Unmarshal(data, &job); err != nil {
		t.Fatal(err)
	}
	req := retryRequest{ID: "second-pr", CheckID: job.Runs[0].Checks["evaluation"].Result.ID, Actor: "human"}
	data, _ = json.Marshal(req)
	if err := s.QueueRetry(context.Background(), req.ID, data); err != nil {
		t.Fatal(err)
	}
	s.Close()
	m.replies = []Reply{reply("finish_evaluation", assessmentJSON())}
	var errs bytes.Buffer
	if code := c.Watch(context.Background(), args, io.Discard, &errs); code != 0 {
		t.Fatal(code, errs.String())
	}
	if len(m.requests) != 7 || len(api.checks) != 5 || !strings.Contains(errs.String(), "second-pr: accepted") {
		t.Fatal("retry ran on wrong PR", len(m.requests), errs.String())
	}
	if code := c.Watch(context.Background(), args, io.Discard, io.Discard); code != 0 || len(m.requests) != 7 {
		t.Fatal("retry repeated", code)
	}
}

func TestWatchScopeValidation(t *testing.T) {
	for _, tt := range []struct {
		number  int
		all     bool
		authors string
		valid   bool
	}{
		{1, false, "", true}, {0, true, "human,agent[bot]", true},
		{0, false, "", false}, {-1, true, "human", false}, {1, true, "human", false},
		{1, false, "human", false}, {0, true, "", false}, {0, true, "human,HUMAN", false}, {0, true, "human,", false}, {0, true, " human", false},
	} {
		_, err := newWatchScope(tt.number, tt.all, tt.authors)
		if (err == nil) != tt.valid {
			t.Fatal(tt, err)
		}
	}
	scope, _ := newWatchScope(0, true, "HUMAN")
	p := trustedPR(1)
	if scope.exclusion("O/R", p) != "" {
		t.Fatal("case-sensitive scope")
	}
	p.State = "CLOSED"
	if scope.exclusion("o/r", p) == "" {
		t.Fatal("closed PR admitted")
	}
}

func TestRetryRoutingRejectsUnknownAndInvalidOwnership(t *testing.T) {
	request := retryRequest{ID: "delivery", CheckID: 42, Actor: "human"}
	data, _ := json.Marshal(request)
	pending := []service.InboxItem{{ID: request.ID, Data: data}}
	for _, mode := range []string{"missing-job", "read-error", "bad-json", "wrong-identity", "nil-run", "nil-check", "draft-check", "conflicting-owner", "shared-history"} {
		t.Run(mode, func(t *testing.T) {
			s := &memoryWorkflow{data: map[string][]byte{}}
			pulls := []PR{trustedPR(1), trustedPR(2)}
			for _, pr := range pulls {
				check := &checkEffect{}
				check.Result.ID = 42
				job := workflowJob{Version: 1, Repository: "o/r", PR: pr.Number, Runs: []*workflowRun{{Checks: map[string]*checkEffect{"review": check}}}}
				switch mode {
				case "missing-job":
					continue
				case "read-error":
					s.readErr = errors.New("storage unavailable")
				case "wrong-identity":
					job.PR = 999
				case "nil-run":
					job.Runs[0] = nil
				case "nil-check":
					job.Runs[0].Checks["review"] = nil
				case "draft-check":
					check.Result.ID = 0
				case "shared-history":
					if pr.Number == 2 {
						continue
					}
					job.Runs = append(job.Runs, job.Runs[0]) // Evaluation retries reuse the review Check.
				}
				data, _ := json.Marshal(job)
				if mode == "bad-json" {
					data = []byte("{")
				}
				s.data[workflowKey("o/r", pr.Number)] = data
			}
			routed, rejected, err := routeWatchRetries(context.Background(), s, "o/r", pulls, pending)
			switch mode {
			case "missing-job", "draft-check":
				if err != nil || len(rejected) != 1 || len(routed) != 0 {
					t.Fatal("unknown Check admitted", routed, rejected, err)
				}
			case "shared-history":
				if err != nil || len(routed[1]) != 1 || len(rejected) != 0 {
					t.Fatal("same PR history rejected", routed, rejected, err)
				}
			default:
				if err == nil {
					t.Fatal("invalid ownership accepted")
				}
			}
		})
	}
}

func TestRepositoryWatchInvalidFlags(t *testing.T) {
	c, args := repositoryWatch(t, &fakeModel{}, &checkAPI{})
	for _, extra := range [][]string{{"--pr", "1"}, {"--authors", "human,HUMAN"}, {"--retry-check", "1", "--retry-id", "retry"}, {"--repo", "bad"}} {
		if code := c.Watch(context.Background(), append(append([]string{}, args...), extra...), io.Discard, io.Discard); code != 1 {
			t.Fatal("invalid flags accepted", extra)
		}
	}
}
