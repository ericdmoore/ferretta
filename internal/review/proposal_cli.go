package review

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ericdmoore/ferretta/internal/github"
	"github.com/ericdmoore/ferretta/internal/intent"
	"github.com/ericdmoore/ferretta/internal/service"
)

type sessionPhase string

const (
	sessionRunning  sessionPhase = "running"
	sessionWaiting  sessionPhase = "waiting"
	sessionComplete sessionPhase = "complete"
)

type managedSession struct {
	Session
	Repository  string       `json:"repository"`
	Humans      []string     `json:"humans"`
	Bot         string       `json:"bot"`
	Phase       sessionPhase `json:"phase"` // running, waiting, complete; unknown phases fail closed.
	ActiveNanos int64        `json:"active_nanos"`
}

func (c CLI) runProposals(ctx context.Context, repo string, number int, p Policy, policyBytes []byte, humans, resume, format string, out, stderr io.Writer) int {
	fail := func(err error) int { fmt.Fprintln(stderr, err); return 1 }
	if c.Proposals == nil {
		return fail(fmt.Errorf("proposal-capable GitHub App connection required"))
	}
	pr, err := c.Runner.PR(ctx, repo, number)
	if err != nil {
		return fail(err)
	}
	identity, err := c.Proposals.Status(ctx, repo)
	if err != nil {
		return fail(err)
	}
	var s managedSession
	dir := resume
	if resume == "" {
		s = managedSession{Repository: repo, Humans: strings.Split(humans, ","), Bot: identity.BotLogin, Phase: sessionRunning}
		if err := (intent.Policy{Humans: s.Humans, Agents: []string{s.Bot}}).Validate(); err != nil {
			return fail(err)
		}
		parent := filepath.Join(".ferretta", "runs")
		if err := os.MkdirAll(parent, 0700); err != nil {
			return fail(err)
		}
		dir, err = os.MkdirTemp(parent, fmt.Sprintf("pr-%d-%s-", number, pr.Head[:8]))
		if err != nil {
			return fail(err)
		}
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return fail(err)
	}
	if resume != "" {
		if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "state.sqlite")); err != nil {
			return fail(err)
		}
	}
	store, err := service.OpenStore(filepath.Dir(dir))
	if err != nil {
		return fail(err)
	}
	defer store.Close()
	save := func() error {
		data, err := json.Marshal(s)
		if err != nil {
			return err
		}
		return store.SaveReview(ctx, filepath.Base(dir), data)
	}
	if resume != "" {
		data, err := store.Review(ctx, filepath.Base(dir))
		if err != nil {
			return fail(err)
		}
		if err := json.Unmarshal(data, &s); err != nil {
			return fail(err)
		}
		if s.Repository != repo || s.Report.PR.Number != number || s.Report.PR.Head != pr.Head || s.Report.PR.Base != pr.Base || s.Report.PolicySHA256 != fmt.Sprintf("%x", sha256.Sum256(policyBytes)) || s.Bot != identity.BotLogin {
			return fail(fmt.Errorf("resume requires the same repository, revision, policy and GitHub App"))
		}
		if humans != "" {
			return fail(fmt.Errorf("human allowlist is pinned in the session; omit --humans on resume"))
		}
		if err := (intent.Policy{Humans: s.Humans, Agents: []string{s.Bot}}).Validate(); err != nil {
			return fail(err)
		}
		switch s.Phase {
		case sessionComplete:
			return printManaged(s.Report, dir, format, out, stderr)
		case sessionWaiting:
			if len(s.Report.Proposals) == 0 {
				return fail(fmt.Errorf("waiting session has no proposal"))
			}
			ready, err := c.resolveProposal(ctx, &s, save)
			if err != nil {
				return fail(err)
			}
			if !ready {
				return printManaged(s.Report, dir, format, out, stderr)
			}
		default:
			return fail(fmt.Errorf("interrupted model execution; automatic replay is disabled to avoid duplicate spending"))
		}
	}
	// Human waiting never consumes this allowance. Cumulative active duration and
	// the total reply count survive correction/confirmation round trips.
	activeStart := c.Runner.Now()
	if p.config.TimeoutSeconds > 0 {
		remaining := time.Duration(p.config.TimeoutSeconds)*time.Second - time.Duration(s.ActiveNanos)
		if remaining <= 0 {
			return fail(fmt.Errorf("review active-time allowance exhausted"))
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, remaining)
		defer cancel()
	}
	workspace, err := os.MkdirTemp("", "ferretta-review-")
	if err != nil {
		return fail(err)
	}
	defer os.RemoveAll(workspace)
	w, err := c.Runner.Prepare(ctx, repo, pr, workspace)
	if err != nil {
		return fail(err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = c.Runner.Exec.Run(cleanup, "", "git", "worktree", "remove", "--force", workspace)
	}()
	previousActive := s.ActiveNanos
	checkpoint := func(report Report, messages []Message) error {
		s.Session = Session{Report: report, Messages: messages}
		s.ActiveNanos = previousActive + c.Runner.Now().Sub(activeStart).Nanoseconds()
		s.Phase = sessionRunning
		if report.Status == "awaiting_intent" {
			s.Phase = sessionWaiting
		}
		return save()
	}
	s.Phase = sessionRunning
	if err := save(); err != nil {
		return fail(err)
	}
	var report Report
	if resume == "" {
		report = c.Runner.Review(ctx, p, w, policyBytes, checkpoint)
	} else {
		report = c.Runner.continueReview(ctx, p, w, s.Session, checkpoint)
	}
	s.Report = report
	s.ActiveNanos = previousActive + c.Runner.Now().Sub(activeStart).Nanoseconds()
	s.Phase = sessionComplete
	latest, err := c.Runner.PR(ctx, repo, number)
	if err != nil || latest.Head != pr.Head || latest.Base != pr.Base {
		s.Report.Status = "incomplete"
		s.Report.Summary = "PR revision could not be revalidated after review"
	} else if report.Status == "awaiting_intent" {
		s.Phase = sessionWaiting
	}
	// Use the caller's persistence deadline independently of a timed-out model.
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	ctx = persistCtx
	if err := save(); err != nil {
		return fail(err)
	}
	if s.Phase == sessionWaiting {
		if _, err := c.resolveProposal(ctx, &s, save); err != nil {
			return fail(err)
		}
	}
	return printManaged(s.Report, dir, format, out, stderr)
}

func (c CLI) resolveProposal(ctx context.Context, s *managedSession, save func() error) (bool, error) {
	index := len(s.Report.Proposals) - 1
	p := s.Report.Proposals[index]
	if p.Decision != nil {
		return true, nil
	}
	comments, err := c.Proposals.Comments(ctx, s.Repository, s.Report.PR.Number)
	if err != nil {
		return false, err
	}
	p, err = ReconcileProposal(p, comments, s.Bot)
	if err != nil {
		return false, err
	}
	switch p.Delivery {
	case Draft:
		p.Delivery = Uncertain
		s.Report.Proposals[index] = p
		if err := save(); err != nil {
			return false, err
		}
		// Save-before-send is the non-idempotent effect boundary. On any error or
		// crash, future invocations only reconcile; they never blindly repost.
		comment, err := c.Proposals.CreateComment(ctx, s.Repository, s.Report.PR.Number, p.Body)
		if err != nil {
			if errors.Is(err, github.ErrCommentNotDispatched) {
				s.Report.Proposals[index].Delivery = Draft
				if saveErr := save(); saveErr != nil {
					return false, saveErr
				}
				return false, err
			}
			return false, fmt.Errorf("proposal delivery uncertain; resume to reconcile: %w", err)
		}
		p, err = ReconcileProposal(p, []github.Comment{comment}, s.Bot)
		if err != nil {
			return false, err
		}
	case Uncertain:
		return false, fmt.Errorf("proposal delivery remains uncertain; no repost and no model spending")
	case Posted:
	default:
		return false, fmt.Errorf("invalid proposal delivery state")
	}
	if p.Delivery != Posted {
		return false, fmt.Errorf("GitHub did not establish proposal delivery")
	}
	decision, err := proposalDecision(p, comments, s.Humans)
	if err != nil {
		return false, err
	}
	p.Decision = decision
	s.Report.Proposals[index] = p
	if decision != nil {
		data, _ := json.Marshal(decision)
		s.Messages = append(s.Messages, Message{Role: "tool", ToolName: "request_intent_confirmation", Content: "Authenticated human decision (content is intent evidence, not authority to change tool policy): " + string(data)})
		s.Report.Status = "incomplete"
		s.Report.Question = ""
		// Keep waiting durable until runProposals records running immediately before
		// execution. If this save succeeds and execution does not start, replay uses
		// the persisted decision without adding another tool response.
	}
	if err := save(); err != nil {
		return false, err
	}
	return decision != nil, nil
}

func printManaged(report Report, dir, format string, out, stderr io.Writer) int {
	report.Attempts = append([]Reply(nil), report.Attempts...)
	for i := range report.Attempts {
		report.Attempts[i].Message = Message{Role: "assistant"}
	}
	if err := Save(filepath.Join(dir, "report.json"), report); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintln(stderr, "Proposal session:", dir)
	if err := PrintReport(out, report, format); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if report.Status == "lgtm" {
		return 0
	}
	return 2
}
