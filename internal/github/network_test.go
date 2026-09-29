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
