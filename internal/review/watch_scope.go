package review

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ericdmoore/ferretta/internal/service"
)

// Scope authorizes automatic execution separately from who can confirm intent.
// A single PR remains an explicit operator selection; repository watching only
// admits allowlisted authors' branches in the watched repository. Checks execute
// as the service OS user, so observing a public PR alone is never authorization.
type watchScope struct {
	number  int
	authors []string
}

func newWatchScope(number int, all bool, authors string) (watchScope, error) {
	if number > 0 && !all && authors == "" {
		return watchScope{number: number}, nil
	}
	if number != 0 || !all || authors == "" {
		return watchScope{}, fmt.Errorf("choose --pr NUMBER or --all-prs --authors LOGIN[,LOGIN]")
	}
	names := strings.Split(authors, ",")
	seen := map[string]bool{}
	for _, name := range names {
		key := strings.ToLower(name)
		if name == "" || strings.TrimSpace(name) != name || seen[key] {
			return watchScope{}, fmt.Errorf("author allowlist requires distinct, nonempty logins without surrounding whitespace")
		}
		seen[key] = true
	}
	return watchScope{authors: names}, nil
}

func (s watchScope) exclusion(repo string, pr PR) string {
	if pr.State != "OPEN" || pr.Draft {
		return "PR is not open and ready"
	}
	if s.number > 0 {
		if pr.Number != s.number {
			return "outside the selected PR"
		}
		return ""
	}
	if !strings.EqualFold(pr.HeadRepository, repo) {
		return "fork or unknown source repository"
	}
	for _, author := range s.authors {
		if strings.EqualFold(author, pr.Author) {
			return ""
		}
	}
	return "author is not allowlisted"
}

// Recheck admission whenever the workflow refreshes the PR, including after a
// human wait. Listing data is a hint; it cannot authorize later execution.
type scopedPullRequests struct {
	source PullRequests
	scope  watchScope
}

func (s scopedPullRequests) PullRequest(ctx context.Context, repo string, number int) (PR, error) {
	pr, err := s.source.PullRequest(ctx, repo, number)
	if err != nil {
		return PR{}, err
	}
	if reason := s.scope.exclusion(repo, pr); reason != "" {
		return PR{}, fmt.Errorf("PR #%d is no longer admitted: %s", number, reason)
	}
	return pr, nil
}

// Route signed retry receipts using locally recorded Check ownership, never the
// order of the PR listing. Unknown/closed/out-of-scope targets are rejected by
// the caller. No receipt is allowed to start a new PR's workflow.
func routeWatchRetries(ctx context.Context, store workflowStore, repo string, pulls []PR, pending []service.InboxItem) (map[int][]retryRequest, []retryRequest, error) {
	routed := map[int][]retryRequest{}
	if len(pending) == 0 {
		return routed, nil, nil
	}
	owners := map[int64]int{}
	for _, pr := range pulls {
		data, err := store.Review(ctx, workflowKey(repo, pr.Number))
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		var job workflowJob
		if err := json.Unmarshal(data, &job); err != nil {
			return nil, nil, err
		}
		if (job.Version != 1 && job.Version != 2) || job.Repository != repo || job.PR != pr.Number {
			return nil, nil, fmt.Errorf("invalid saved retry ownership")
		}
		for _, run := range job.Runs {
			if run == nil {
				return nil, nil, fmt.Errorf("invalid saved retry run")
			}
			for _, check := range run.Checks {
				if check == nil {
					return nil, nil, fmt.Errorf("invalid saved retry Check")
				}
				id := check.Result.ID
				if id <= 0 {
					continue
				}
				if owner, exists := owners[id]; exists && owner != pr.Number {
					return nil, nil, fmt.Errorf("Check has multiple saved PR owners")
				}
				owners[id] = pr.Number
			}
		}
	}
	var rejected []retryRequest
	for _, item := range pending {
		var request retryRequest
		if err := json.Unmarshal(item.Data, &request); err != nil || request.ID != item.ID || request.validate() != nil {
			return nil, nil, fmt.Errorf("invalid persisted retry receipt")
		}
		if owner, ok := owners[request.CheckID]; ok {
			routed[owner] = append(routed[owner], request)
		} else {
			rejected = append(rejected, request)
		}
	}
	return routed, rejected, nil
}

func workflowKey(repo string, number int) string {
	return fmt.Sprintf("workflow-v1:%s:%d", strings.ToLower(repo), number)
}
