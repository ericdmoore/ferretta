//go:build network

package github

import (
	"context"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"
)

// Run only through make test-network with an explicitly selected fixture PR.
// The fixture must contain a comment; this checks real provider field mapping.
func TestNetworkComments(t *testing.T) {
	repo := os.Getenv("FERRETTA_TEST_REPO")
	pr, err := strconv.Atoi(os.Getenv("FERRETTA_TEST_PR"))
	if repo == "" || err != nil || pr <= 0 {
		t.Fatal("set FERRETTA_TEST_REPO and FERRETTA_TEST_PR to a PR with comments")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := Client{HTTP: &http.Client{Timeout: 10 * time.Second}, Token: os.Getenv("GITHUB_TOKEN")}
	comments, err := c.Comments(ctx, repo, pr)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) == 0 {
		t.Fatal("fixture PR must have at least one comment")
	}
	for _, comment := range comments {
		if comment.ID <= 0 || comment.URL == "" || comment.User.Login == "" || comment.User.Type == "" || comment.CreatedAt == "" || comment.UpdatedAt == "" {
			t.Fatalf("provider evidence fields missing on comment %d", comment.ID)
		}
	}
}

// This separate suite requires an explicitly provisioned app; it never posts.
func TestNetworkApp(t *testing.T) {
	repo := os.Getenv("FERRETTA_TEST_REPO")
	number, err := strconv.Atoi(os.Getenv("FERRETTA_TEST_PR"))
	if !ValidRepository(repo) || err != nil || number <= 0 {
		t.Fatal("set FERRETTA_TEST_REPO and FERRETTA_TEST_PR to a PR with comments")
	}
	a, err := LoadDefaultApp()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	identity, err := a.Status(ctx, repo)
	if err != nil || identity.BotLogin == "" {
		t.Fatal("verify installation", err)
	}
	pr, err := a.PullRequest(ctx, repo, number)
	if err != nil || pr.Head == "" || pr.Base == "" || pr.URL == "" {
		t.Fatal("read PR", err)
	}
	open, err := a.OpenPullRequests(ctx, repo)
	if err != nil {
		t.Fatal("list open PRs", err)
	}
	for _, candidate := range open {
		if candidate.Number <= 0 || candidate.Head == "" || candidate.Base == "" || candidate.State != "OPEN" {
			t.Fatal("missing open PR evidence")
		}
	}
	comments, err := a.Comments(ctx, repo, number)
	if err != nil || len(comments) == 0 {
		t.Fatal("fixture requires comments", err)
	}
	for _, c := range comments {
		if c.ID <= 0 || c.User.Login == "" || c.User.Type == "" {
			t.Fatal("missing authenticated comment identity")
		}
	}
}
