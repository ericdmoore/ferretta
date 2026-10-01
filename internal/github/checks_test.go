package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func checkFixture() CheckInput {
	return CheckInput{Name: "Ferretta / review", Head: strings.Repeat("a", 40), ExternalID: "effect", Status: "in_progress", Output: CheckOutput{Title: "Reviewing", Summary: "Exact revision", Text: "Progress"}}
}

func TestCheckLifecycleAndCredentialScopes(t *testing.T) {
	grants := map[string]int{}
	var saved CheckRun
	saved.ID, saved.URL, saved.App.ID = 23, "https://github.com/owner/repo/runs/23", 7
	var methods []string
	a := makeApp(t, transportFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/repos/owner/repo/installation":
			return response(200, installationJSON), nil
		case "/app/installations/42/access_tokens":
			var request struct {
				Permissions  map[string]string
				Repositories []string
			}
			if json.NewDecoder(r.Body).Decode(&request) != nil || len(request.Repositories) != 1 || request.Repositories[0] != "repo" || len(request.Permissions) != 2 || request.Permissions["contents"] != "read" {
				t.Fatal("unscoped request")
			}
			scope := request.Permissions["checks"]
			if scope == "" {
				scope = "fetch"
				if request.Permissions["pull_requests"] != "read" {
					t.Fatal("fetch broadened")
				}
			}
			grants[scope]++
			data, _ := json.Marshal(map[string]any{"token": scope, "expires_at": appTime.Add(time.Hour), "permissions": request.Permissions})
			return response(201, string(data)), nil
		case "/repos/owner/repo/check-runs", "/repos/owner/repo/check-runs/23":
			methods = append(methods, r.Method)
			if r.Method == "GET" {
				if r.Header.Get("Authorization") != "Bearer read" {
					t.Fatal("check read used broad token")
				}
			} else {
				if r.Header.Get("Authorization") != "Bearer write" {
					t.Fatal("check write used wrong token")
				}
				var input CheckInput
				if json.NewDecoder(r.Body).Decode(&input) != nil {
					t.Fatal("invalid JSON")
				}
				if r.Method == "PATCH" {
					if input.Head != "" {
						t.Fatal("update attempted to change head")
					}
					input.Head = saved.Head
				}
				saved.CheckInput = input
			}
			data, _ := json.Marshal(saved)
			return response(200, string(data)), nil
		default:
			if !strings.HasSuffix(r.URL.Path, "/check-runs") || r.URL.Query().Get("filter") != "all" {
				t.Fatal(r.URL)
			}
			data, _ := json.Marshal(map[string]any{"check_runs": []CheckRun{saved}})
			return response(200, string(data)), nil
		}
	}), func() time.Time { return appTime })
	c := &Connection{Load: func() (*App, error) { return a, nil }}
	ctx := context.Background()
	if _, err := c.Token(ctx, "owner/repo"); err != nil {
		t.Fatal(err)
	}
	input := checkFixture()
	if result, err := c.WriteCheck(ctx, "owner/repo", 0, input); err != nil || result.CheckInput != input {
		t.Fatal(result, err)
	}
	input.Status, input.Conclusion = "completed", "success"
	if result, err := c.WriteCheck(ctx, "owner/repo", 23, input); err != nil || result.CheckInput != input {
		t.Fatal(result, err)
	}
	if result, err := c.CheckRun(ctx, "owner/repo", 23); err != nil || result.CheckInput != input {
		t.Fatal(result, err)
	}
	if result, err := c.CheckRuns(ctx, "owner/repo", input.Head); err != nil || len(result) != 1 {
		t.Fatal(result, err)
	}
	if _, err := c.Token(ctx, "owner/repo"); err != nil {
		t.Fatal(err)
	}
	if len(grants) != 3 || grants["read"] != 1 || grants["write"] != 1 || grants["fetch"] != 1 || strings.Join(methods, ",") != "POST,PATCH,GET" {
		t.Fatal(grants, methods)
	}
}

func TestCheckValidationFailuresAndUncertainWrites(t *testing.T) {
	ctx := context.Background()
	for _, mutate := range []func(*CheckInput){func(c *CheckInput) { c.Head = "bad" }, func(c *CheckInput) { c.Name = "" }, func(c *CheckInput) { c.Output.Summary = "" }, func(c *CheckInput) { c.Output.Text = strings.Repeat("x", 60001) }, func(c *CheckInput) { c.Status = "completed" }, func(c *CheckInput) { c.Conclusion = "success" }, func(c *CheckInput) { c.Status = "waiting" }} {
		input := checkFixture()
		mutate(&input)
		if input.Validate() == nil {
			t.Fatal("invalid state accepted", input.Status)
		}
	}
	for _, status := range []string{"queued", "in_progress"} {
		input := checkFixture()
		input.Status = status
		if input.Validate() != nil {
			t.Fatal(status)
		}
	}
	c := &Connection{}
	if _, err := c.WriteCheck(ctx, "o/r", 0, checkFixture()); !errors.Is(err, ErrCheckNotDispatched) {
		t.Fatal(err)
	}
	if _, err := c.CheckRun(ctx, "o/r", 1); err == nil {
		t.Fatal("missing connection")
	}
	if _, err := c.CheckRuns(ctx, "o/r", strings.Repeat("a", 40)); err == nil {
		t.Fatal("missing connection")
	}
	for _, mode := range []string{"auth", "scope", "write", "read", "invalid"} {
		dispatches := 0
		a := makeApp(t, transportFunc(func(r *http.Request) (*http.Response, error) {
			if strings.HasSuffix(r.URL.Path, "/installation") {
				if mode == "auth" {
					return nil, errors.New("offline")
				}
				return response(200, installationJSON), nil
			}
			if strings.HasSuffix(r.URL.Path, "/access_tokens") {
				var request struct{ Permissions map[string]string }
				_ = json.NewDecoder(r.Body).Decode(&request)
				if mode == "scope" {
					request.Permissions["checks"] = "read"
				}
				data, _ := json.Marshal(map[string]any{"token": "check-token", "expires_at": appTime.Add(time.Hour), "permissions": request.Permissions})
				return response(201, string(data)), nil
			}
			dispatches++
			return nil, errors.New("response lost")
		}), func() time.Time { return appTime })
		input := checkFixture()
		repo := "o/r"
		id := int64(1)
		if mode == "invalid" {
			repo = "bad"
			id = -1
		}
		_, err := a.WriteCheck(ctx, repo, id, input)
		if err == nil || errors.Is(err, ErrCheckNotDispatched) != (mode == "auth" || mode == "scope" || mode == "invalid") {
			t.Fatal(mode, err)
		}
		if dispatches > 1 {
			t.Fatal("write retried")
		}
		if _, err := a.CheckRun(ctx, repo, id); err == nil {
			t.Fatal("read failure")
		}
		if _, err := a.CheckRuns(ctx, repo, input.Head); err == nil {
			t.Fatal("list failure")
		}
	}
}

func TestCheckPagination(t *testing.T) {
	for _, pages := range []int{0, 2, 101} {
		calls := 0
		a := makeApp(t, transportFunc(func(r *http.Request) (*http.Response, error) {
			if strings.HasSuffix(r.URL.Path, "/installation") {
				return response(200, installationJSON), nil
			}
			if strings.HasSuffix(r.URL.Path, "/access_tokens") {
				return response(201, strings.Replace(tokenJSON(appTime), "pull_requests", "checks", 1)), nil
			}
			calls++
			if pages == 0 {
				return response(200, `{}`), nil
			}
			if r.URL.Query().Get("page") != fmt.Sprint(calls) {
				t.Fatal("wrong page")
			}
			count := 100
			if calls == pages {
				count = 1
			}
			data, _ := json.Marshal(map[string]any{"check_runs": make([]CheckRun, count)})
			return response(200, string(data)), nil
		}), func() time.Time { return appTime })
		result, err := a.CheckRuns(context.Background(), "o/r", strings.Repeat("a", 40))
		if pages == 0 && (err == nil || calls != 1) {
			t.Fatal("malformed listing treated as empty", err)
		}
		if pages == 2 && (err != nil || len(result) != 101) {
			t.Fatal(len(result), err)
		}
		if pages == 101 && (err == nil || calls != 100) {
			t.Fatal("unbounded listing", calls, err)
		}
	}
}
