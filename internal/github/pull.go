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

func (c Client) PullRequest(ctx context.Context, repo string, number int) (PullRequest, error) {
	if !ValidRepository(repo) || number <= 0 {
		return PullRequest{}, fmt.Errorf("provide owner/repository and a positive PR number")
	}
	var data struct {
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
	if err := c.get(ctx, "/repos/"+repo+"/pulls/"+strconv.Itoa(number), &data); err != nil {
		return PullRequest{}, err
	}
	if data.Number != number {
		return PullRequest{}, fmt.Errorf("GitHub returned a different PR")
	}
	return PullRequest{data.Number, data.Head.SHA, data.Base.SHA, data.URL, data.Title, data.Body, strings.ToUpper(data.State), data.Draft}, nil
}
