package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestCommentCredentialsRemainSeparateFromFetch(t *testing.T) {
	exchanges, posts := 0, 0
	a := makeApp(t, transportFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/repos/owner/repo/installation":
			return response(200, installationJSON), nil
		case "/app/installations/42/access_tokens":
			exchanges++
			var b struct {
				Permissions  map[string]string
				Repositories []string
			}
			if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
				t.Fatal(err)
			}
			if len(b.Permissions) != 2 || b.Permissions["contents"] != "read" || len(b.Repositories) != 1 || b.Repositories[0] != "repo" {
				t.Fatal("unscoped token")
			}
			level := b.Permissions["pull_requests"]
			if level != "read" && level != "write" {
				t.Fatal(level)
			}
			token := strings.Replace(tokenJSON(appTime), `"pull_requests":"read"`, `"pull_requests":"`+level+`"`, 1)
			return response(201, token), nil
		case "/repos/owner/repo/issues/1/comments":
			posts++
			var b struct{ Body string }
			if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&b) != nil || b.Body != "proposal" {
				t.Fatal("wrong comment")
			}
			return response(201, `{"id":123,"body":"proposal"}`), nil
		default:
			t.Fatal(r.URL.Path)
			return nil, nil
		}
	}), func() time.Time { return appTime })
	c := &Connection{Load: func() (*App, error) { return a, nil }}
	if _, err := c.Token(context.Background(), "owner/repo"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		comment, err := c.CreateComment(context.Background(), "owner/repo", 1, "proposal")
		if err != nil || comment.ID != 123 {
			t.Fatal(comment, err)
		}
	}
	if _, err := c.Token(context.Background(), "owner/repo"); err != nil {
		t.Fatal(err)
	}
	if exchanges != 2 || posts != 2 {
		t.Fatal("token cache mixed permissions", exchanges, posts)
	}
}
func TestCommentFailureDoesNotRetry(t *testing.T) {
	if _, err := (&Connection{}).CreateComment(context.Background(), "owner/repo", 1, "x"); err == nil {
		t.Fatal("missing loader")
	}
	a := makeApp(t, transportFunc(func(r *http.Request) (*http.Response, error) { return nil, errors.New("offline") }), func() time.Time { return appTime })
	for _, tc := range []struct {
		repo string
		n    int
		body string
	}{{"invalid", 1, "x"}, {"o/r", 0, "x"}, {"o/r", 1, ""}, {"o/r", 1, strings.Repeat("x", 60001)}, {"o/r", 1, "x"}} {
		if _, err := a.CreateComment(context.Background(), tc.repo, tc.n, tc.body); err == nil {
			t.Fatal("accepted invalid/failed comment")
		}
	}
	for _, mode := range []string{"post_error", "wrong_permission"} {
		posts := 0
		a = makeApp(t, transportFunc(func(r *http.Request) (*http.Response, error) {
			if strings.HasSuffix(r.URL.Path, "/installation") {
				return response(200, installationJSON), nil
			}
			if strings.HasSuffix(r.URL.Path, "/access_tokens") {
				body := tokenJSON(appTime)
				if mode != "wrong_permission" {
					body = strings.Replace(body, `"pull_requests":"read"`, `"pull_requests":"write"`, 1)
				}
				return response(201, body), nil
			}
			posts++
			return nil, errors.New("unknown outcome")
		}), func() time.Time { return appTime })
		if _, err := a.CreateComment(context.Background(), "owner/repo", 1, "proposal"); err == nil {
			t.Fatal("write failure ignored")
		}
		if posts > 1 {
			t.Fatal("blind retry")
		}
	}
}
