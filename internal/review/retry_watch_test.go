package review

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ericdmoore/ferretta/internal/github"
	"github.com/ericdmoore/ferretta/internal/service"
)

func TestWatchExplicitAndQueuedRetry(t *testing.T) {
	for _, mode := range []string{"operator", "queue", "rejected", "bad-json", "wrong-id", "read-failure", "save-failure", "ack-failure", "reject-ack-failure", "budget-rejected", "operator-queue"} {
		t.Run(mode, func(t *testing.T) {
			t.Chdir(t.TempDir())
			_ = os.WriteFile("review.json", []byte(policyJSON), 0600)
			_ = os.WriteFile("judge.json", []byte(evaluationPolicyJSON()), 0600)
			state := filepath.Join(t.TempDir(), "state")
			s, err := service.OpenStore(state)
			if err != nil {
				t.Fatal(err)
			}
			m := &fakeModel{replies: workflowReplies()}
			api := &checkAPI{pulls: []PR{pr()}}
			c := checksCLI(m, api)
			job, err := advance(t, c, s)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "budget-rejected" {
				job.ReviewNanos = 60 * 1e9
				data, _ := json.Marshal(job)
				if err := s.SaveReview(context.Background(), workflowKey("o/r", 1), data); err != nil {
					t.Fatal(err)
				}
			}
			id := job.Runs[0].Checks["review"].Result.ID
			req := retryRequest{ID: "delivery", CheckID: id, Actor: "human"}
			if mode == "rejected" || mode == "reject-ack-failure" {
				req.CheckID = 1000
			}
			if mode == "wrong-id" {
				req.ID = "other"
			}
			data, _ := json.Marshal(req)
			if mode == "bad-json" {
				data = []byte("{")
			}
			if mode != "operator" {
				if err := s.QueueRetry(context.Background(), "delivery", data); err != nil {
					t.Fatal(err)
				}
			}
			s.Close()
			db, err := sql.Open("sqlite", filepath.Join(state, "state.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "read-failure":
				_, err = db.Exec("DROP TABLE retry_inbox")
			case "save-failure":
				_, err = db.Exec("CREATE TRIGGER fail_save BEFORE UPDATE ON review_session BEGIN SELECT RAISE(ABORT,'disk failure'); END")
			case "ack-failure", "reject-ack-failure":
				_, err = db.Exec("CREATE TRIGGER fail_ack BEFORE UPDATE ON retry_inbox BEGIN SELECT RAISE(ABORT,'disk failure'); END")
			}
			db.Close()
			if err != nil {
				t.Fatal(err)
			}
			args := []string{"--repo", "o/r", "--pr", "1", "--state", state, "--review-policy", "review.json", "--judge-policy", "judge.json", "--humans", "human", "--once"}
			if mode == "operator" || mode == "operator-queue" {
				args = append(args, "--retry-check", strconv.FormatInt(id, 10), "--retry-id", "delivery")
			}
			m.replies = workflowReplies()
			var errs bytes.Buffer
			code := c.Watch(context.Background(), args, io.Discard, &errs)
			want := 1
			if mode == "operator" || mode == "operator-queue" || mode == "queue" || mode == "rejected" || mode == "budget-rejected" {
				want = 0
			}
			if code != want {
				t.Fatal(mode, code, errs.String())
			}
			if mode == "operator" || mode == "operator-queue" || mode == "queue" || mode == "ack-failure" {
				if len(m.requests) != 6 {
					t.Fatal("retry did not run once", len(m.requests))
				}
			} else if len(m.requests) != 3 {
				t.Fatal("rejected request spent", len(m.requests))
			}
			if mode == "operator-queue" {
				saved, err := service.OpenStore(state)
				if err != nil {
					t.Fatal(err)
				}
				pending, err := saved.PendingRetries(context.Background())
				saved.Close()
				if err != nil || len(pending) != 1 {
					t.Fatal("operator retry drained webhook inbox", pending, err)
				}
			}
			if mode == "operator" || mode == "operator-queue" || mode == "queue" {
				if code := c.Watch(context.Background(), args, io.Discard, &errs); code != 0 || len(m.requests) != 6 {
					t.Fatal("redelivery repeated model work", code, errs.String())
				}
			}
		})
	}
}

type retryWatchAPI struct {
	*checkAPI
	identity github.Identity
	err      error
}

func (a *retryWatchAPI) Status(context.Context, string) (github.Identity, error) {
	return a.identity, a.err
}

func TestWatchWebhookConfiguration(t *testing.T) {
	for _, mode := range []string{"valid", "auth-error", "identity", "humans", "secret", "once", "missing-secret-flag", "retry-flags"} {
		t.Run(mode, func(t *testing.T) {
			t.Chdir(t.TempDir())
			_ = os.WriteFile("review.json", []byte(policyJSON), 0600)
			_ = os.WriteFile("judge.json", []byte(evaluationPolicyJSON()), 0600)
			_ = os.WriteFile("secret", webhookSecret, 0600)
			api := &retryWatchAPI{checkAPI: &checkAPI{}, identity: github.Identity{AppID: 7, InstallationID: 9, BotLogin: "ferretta[bot]"}}
			c := checksCLI(&fakeModel{}, api.checkAPI)
			c.Proposals = api
			args := []string{"--repo", "o/r", "--pr", "1", "--state", filepath.Join(t.TempDir(), "state"), "--review-policy", "review.json", "--judge-policy", "judge.json", "--humans", "human", "--webhook-listen", "127.0.0.1:0", "--webhook-secret-file", "secret"}
			switch mode {
			case "auth-error":
				api.err = errors.New("offline")
			case "identity":
				api.identity.InstallationID = 0
			case "humans":
				args = append(args, "--humans", "human,human")
			case "secret":
				args = append(args, "--webhook-secret-file", "missing")
			case "once":
				args = append(args, "--once")
			case "missing-secret-flag":
				args = args[:len(args)-2]
			case "retry-flags":
				args = append(args, "--retry-check", "1")
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			var errs bytes.Buffer
			code := c.Watch(ctx, args, io.Discard, &errs)
			if mode == "valid" {
				if code != 0 || !strings.Contains(errs.String(), "Signed retry webhook listening") {
					t.Fatal(code, errs.String())
				}
			} else if code != 1 {
				t.Fatal("invalid setup accepted", mode, code)
			}
		})
	}
}
