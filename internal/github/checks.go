package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// CheckOutput deliberately excludes annotations: append-only annotation effects
// require their own reconciliation contract before they can be safely retried.
type CheckOutput struct {
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Text    string `json:"text"`
}

type CheckInput struct {
	Name       string      `json:"name"`
	Head       string      `json:"head_sha,omitempty"`
	ExternalID string      `json:"external_id"`
	Status     string      `json:"status"`
	Conclusion string      `json:"conclusion,omitempty"`
	Output     CheckOutput `json:"output"`
}

func (c CheckInput) Validate() error {
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(c.Head) || c.Name == "" || len(c.Name) > 100 || c.ExternalID == "" || len(c.ExternalID) > 100 || strings.TrimSpace(c.Output.Title) == "" || len(c.Output.Title) > 255 || strings.TrimSpace(c.Output.Summary) == "" || len(c.Output.Summary) > 60000 || len(c.Output.Text) > 60000 {
		return fmt.Errorf("check requires exact head, bounded identity and output")
	}
	if (c.Status == "queued" || c.Status == "in_progress") && c.Conclusion == "" {
		return nil
	}
	if c.Status == "completed" && slices.Contains([]string{"success", "failure", "neutral", "action_required", "cancelled", "timed_out", "skipped"}, c.Conclusion) {
		return nil
	}
	return fmt.Errorf("invalid check status/conclusion")
}

type CheckRun struct {
	CheckInput
	ID  int64  `json:"id"`
	URL string `json:"html_url"`
	App struct {
		ID int64 `json:"id"`
	} `json:"app"`
}

// ErrCheckNotDispatched is proof that no POST/PATCH was attempted. Every other
// error may hide a successful effect; callers must reconcile before proceeding.
var ErrCheckNotDispatched = errors.New("check was not dispatched")

type Checks interface {
	CheckRuns(context.Context, string, string) ([]CheckRun, error)
	CheckRun(context.Context, string, int64) (CheckRun, error)
	WriteCheck(context.Context, string, int64, CheckInput) (CheckRun, error)
}

func (a *App) WriteCheck(ctx context.Context, repo string, id int64, input CheckInput) (CheckRun, error) {
	if err := input.Validate(); err != nil || !ValidRepository(repo) || id < 0 {
		return CheckRun{}, fmt.Errorf("%w: invalid check, repository or ID", ErrCheckNotDispatched)
	}
	token, err := a.scopedToken(ctx, repo, "checks", "write")
	if err != nil {
		return CheckRun{}, fmt.Errorf("%w: %v", ErrCheckNotDispatched, err)
	}
	method, path := "POST", fmt.Sprintf("/repos/%s/check-runs", repo)
	if id > 0 {
		method, path = "PATCH", fmt.Sprintf("%s/%d", path, id)
		input.Head = "" // Updates cannot change the commit binding.
	}
	data, _ := json.Marshal(input)
	var result CheckRun
	err = a.request(ctx, method, path, token, data, &result)
	return result, err
}

func (a *App) CheckRun(ctx context.Context, repo string, id int64) (CheckRun, error) {
	if !ValidRepository(repo) || id <= 0 {
		return CheckRun{}, fmt.Errorf("valid check repository and ID required")
	}
	token, err := a.scopedToken(ctx, repo, "checks", "read")
	if err != nil {
		return CheckRun{}, err
	}
	var result CheckRun
	err = a.request(ctx, "GET", fmt.Sprintf("/repos/%s/check-runs/%d", repo, id), token, nil, &result)
	return result, err
}

func (a *App) CheckRuns(ctx context.Context, repo, head string) ([]CheckRun, error) {
	if !ValidRepository(repo) || !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(head) {
		return nil, fmt.Errorf("valid repository and exact check head required")
	}
	token, err := a.scopedToken(ctx, repo, "checks", "read")
	if err != nil {
		return nil, err
	}
	var result []CheckRun
	for page := 1; page <= 100; page++ {
		var batch struct {
			Runs []CheckRun `json:"check_runs"`
		}
		path := fmt.Sprintf("/repos/%s/commits/%s/check-runs?filter=all&per_page=100&page=%d", repo, head, page)
		if err := a.request(ctx, "GET", path, token, nil, &batch); err != nil {
			return nil, err
		}
		if batch.Runs == nil {
			return nil, fmt.Errorf("missing check list; reconciliation incomplete")
		}
		result = append(result, batch.Runs...)
		if len(batch.Runs) < 100 {
			return result, nil
		}
	}
	return nil, fmt.Errorf("check pagination limit reached; reconciliation incomplete")
}

func (c *Connection) WriteCheck(ctx context.Context, repo string, id int64, input CheckInput) (CheckRun, error) {
	a, err := c.get()
	if err != nil {
		return CheckRun{}, fmt.Errorf("%w: %v", ErrCheckNotDispatched, err)
	}
	return a.WriteCheck(ctx, repo, id, input)
}

func (c *Connection) CheckRun(ctx context.Context, repo string, id int64) (CheckRun, error) {
	a, err := c.get()
	if err != nil {
		return CheckRun{}, err
	}
	return a.CheckRun(ctx, repo, id)
}

func (c *Connection) CheckRuns(ctx context.Context, repo, head string) ([]CheckRun, error) {
	a, err := c.get()
	if err != nil {
		return nil, err
	}
	return a.CheckRuns(ctx, repo, head)
}
