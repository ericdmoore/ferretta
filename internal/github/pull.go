package github

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// PullRequest retains the existing report representation while adapting REST.
type PullRequest struct {
	Number int    `json:"number"`
	Head   string `json:"headRefOid"`
	Base   string `json:"baseRefOid"`
	URL    string `json:"url"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	State  string `json:"state"`
	Draft  bool   `json:"isDraft"`
}

type pullResponse struct {
	Number int `json:"number"`
	Head   struct {
		SHA string `json:"sha"`
	} `json:"head"`
	Base struct {
		SHA string `json:"sha"`
	} `json:"base"`
	URL   string `json:"html_url"`
	Title string `json:"title"`
	Body  string `json:"body"`
	State string `json:"state"`
	Draft bool   `json:"draft"`
}

func (p pullResponse) pullRequest() PullRequest {
	return PullRequest{p.Number, p.Head.SHA, p.Base.SHA, p.URL, p.Title, p.Body, strings.ToUpper(p.State), p.Draft}
}

func (c Client) PullRequest(ctx context.Context, repo string, number int) (PullRequest, error) {
	if !ValidRepository(repo) || number <= 0 {
		return PullRequest{}, fmt.Errorf("provide owner/repository and a positive PR number")
	}
	var data pullResponse
	if err := c.get(ctx, "/repos/"+repo+"/pulls/"+strconv.Itoa(number), &data); err != nil {
		return PullRequest{}, err
	}
	if data.Number != number {
		return PullRequest{}, fmt.Errorf("GitHub returned a different PR")
	}
	return data.pullRequest(), nil
}

// OpenPullRequests returns a complete paginated read or an error, never a
// partial success. GitHub pagination is not an atomic snapshot; consumers must
// revalidate a revision before dispatching work or accepting a verdict.
func (c Client) OpenPullRequests(ctx context.Context, repo string) ([]PullRequest, error) {
	if !ValidRepository(repo) || c.HTTP == nil {
		return nil, fmt.Errorf("valid repository and HTTP client are required")
	}
	result := []PullRequest{}
	for page := 1; page <= 100; page++ {
		var batch []pullResponse
		path := "/repos/" + repo + "/pulls?state=open&sort=created&direction=asc&per_page=100&page=" + strconv.Itoa(page)
		if err := c.get(ctx, path, &batch); err != nil {
			return nil, err
		}
		for _, item := range batch {
			result = append(result, item.pullRequest())
		}
		if len(batch) < 100 {
			return result, nil
		}
	}
	return nil, fmt.Errorf("open PR listing exceeds 100 pages")
}
