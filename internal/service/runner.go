package service

import (
	"context"
	"fmt"
	"time"

	"github.com/ericdmoore/ferretta/internal/github"
)

type Source interface {
	OpenPullRequests(context.Context, string) ([]github.PullRequest, error)
}
type Recorder interface {
	Record(context.Context, RecordSnapshot) error
}

type Runner struct {
	Source Source
	Store  Recorder
	Now    func() time.Time
	Wait   func(context.Context, time.Duration) error
	Report func(string, error)
}

// Run polls only. Read failures retry at the next interval. Persistence failure
// stops the owner; no external model/tool/write effects exist in this slice.
func (r Runner) Run(ctx context.Context, repos []string, interval time.Duration, once bool) error {
	if r.Source == nil || r.Store == nil || r.Now == nil || r.Wait == nil || r.Report == nil || len(repos) == 0 || interval < 5*time.Second || interval > 24*time.Hour {
		return fmt.Errorf("source, store, clock, wait, reporter, repositories and interval (5s–24h) required")
	}
	for _, repo := range repos {
		if !github.ValidRepository(repo) {
			return fmt.Errorf("invalid repository")
		}
	}
	for {
		var lastError error
		for _, repo := range repos {
			if ctx.Err() != nil {
				return nil
			}
			// Date the read before dispatch, so a delayed response cannot appear newer.
			at := r.Now()
			pollCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			pulls, err := r.Source.OpenPullRequests(pollCtx, repo)
			cancel()
			if ctx.Err() != nil {
				return nil
			}
			var command RecordSnapshot
			if err == nil {
				command, err = PlanSnapshot(repo, pulls, at)
			}
			if err != nil {
				lastError = err
				r.Report(repo, err)
				continue
			}
			if err := r.Store.Record(ctx, command); err != nil {
				return fmt.Errorf("persist PR intake: %w", err)
			}
		}
		if once {
			return lastError
		}
		if err := r.Wait(ctx, interval); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
	}
}

func Wait(ctx context.Context, interval time.Duration) error {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
