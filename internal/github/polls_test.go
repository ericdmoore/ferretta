package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestOpenPullRequestPagination(t *testing.T) {
	ctx := context.Background()
	batch := `[` + strings.TrimSuffix(strings.Repeat(`{"number":1},`, 100), ",") + `]`
	c := Client{Token: "secret", HTTP: transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer secret" || r.URL.Path != "/repos/o/r/pulls" || r.URL.Query().Get("state") != "open" || r.URL.Query().Get("sort") != "created" {
			t.Fatal("wrong request", r.URL)
		}
		if r.URL.Query().Get("page") == "1" {
			return response(200, batch), nil
		}
		return response(200, `[{"number":101,"head":{"sha":"head","repo":{"full_name":"o/r"}},"user":{"login":"human"},"base":{"sha":"base"},"state":"open","draft":true}]`), nil
	})}
	prs, err := c.OpenPullRequests(ctx, "o/r")
	if err != nil || len(prs) != 101 || prs[100].Head != "head" || prs[100].Base != "base" || prs[100].State != "OPEN" || !prs[100].Draft || prs[100].Author != "human" || prs[100].HeadRepository != "o/r" {
		t.Fatal(prs, err)
	}
	for _, repo := range []string{"bad", "a/b?c"} {
		if _, err := c.OpenPullRequests(ctx, repo); err == nil {
			t.Fatal("invalid repository")
		}
	}
	if _, err := (Client{}).OpenPullRequests(ctx, "o/r"); err == nil {
		t.Fatal("missing client")
	}
	c.HTTP = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("page") == "1" {
			return response(200, batch), nil
		}
		return response(500, `private response`), nil
	})
	if prs, err := c.OpenPullRequests(ctx, "o/r"); err == nil || prs != nil {
		t.Fatal("partial listing returned")
	}
	c.HTTP = transportFunc(func(*http.Request) (*http.Response, error) { return response(200, batch), nil })
	if prs, err := c.OpenPullRequests(ctx, "o/r"); err == nil || prs != nil {
		t.Fatal("unbounded pagination")
	}
}

func TestAppPollingConnection(t *testing.T) {
	a := makeApp(t, transportFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/repos/o/r/installation":
			return response(200, installationJSON), nil
		case "/app/installations/42/access_tokens":
			return response(201, tokenJSON(appTime)), nil
		case "/repos/o/r/pulls":
			if r.Header.Get("Authorization") != "Bearer installation-secret" {
				t.Fatal("wrong identity")
			}
			return response(200, `[{"number":1,"state":"open"}]`), nil
		}
		return nil, fmt.Errorf("unexpected request")
	}), func() time.Time { return appTime })
	c := &Connection{Load: func() (*App, error) { return a, nil }}
	prs, err := c.OpenPullRequests(context.Background(), "o/r")
	if err != nil || len(prs) != 1 {
		t.Fatal(err)
	}
	c = &Connection{Load: func() (*App, error) { return nil, errors.New("not provisioned") }}
	if _, err := c.OpenPullRequests(context.Background(), "o/r"); err == nil {
		t.Fatal("missing connection accepted")
	}
}
