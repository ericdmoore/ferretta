package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrCommentNotDispatched identifies failures before the comment POST.
var ErrCommentNotDispatched = errors.New("comment was not dispatched")

// CreateComment performs exactly one dispatch. Its caller must durably record
// intent before calling and reconcile ambiguous outcomes through Comments.
func (a *App) CreateComment(ctx context.Context, repo string, pr int, body string) (Comment, error) {
	if !ValidRepository(repo) || pr <= 0 || strings.TrimSpace(body) == "" || len(body) > 60000 {
		return Comment{}, fmt.Errorf("%w: valid repository, PR and bounded comment body required", ErrCommentNotDispatched)
	}
	token, err := a.token(ctx, repo, "write")
	if err != nil {
		return Comment{}, fmt.Errorf("%w: %v", ErrCommentNotDispatched, err)
	}
	data, _ := json.Marshal(struct {
		Body string `json:"body"`
	}{body})
	var result Comment
	err = a.request(ctx, "POST", fmt.Sprintf("/repos/%s/issues/%d/comments", repo, pr), token, data, &result)
	return result, err
}
func (c *Connection) CreateComment(ctx context.Context, repo string, pr int, body string) (Comment, error) {
	a, err := c.get()
	if err != nil {
		return Comment{}, fmt.Errorf("%w: %v", ErrCommentNotDispatched, err)
	}
	return a.CreateComment(ctx, repo, pr, body)
}
