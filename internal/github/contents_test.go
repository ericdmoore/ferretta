package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestContentsCredentialIsSeparateAndNarrow(t *testing.T) {
	a := makeApp(t, transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == "GET" {
			return response(200, installationJSON), nil
		}
		var payload struct {
			Permissions  map[string]string
			Repositories []string
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if len(payload.Permissions) != 1 || payload.Permissions["contents"] != "write" || len(payload.Repositories) != 1 || payload.Repositories[0] != "repo" {
			t.Fatal("broad token", payload)
		}
		data, _ := json.Marshal(map[string]any{"token": "write-token", "expires_at": appTime.Add(time.Hour), "permissions": payload.Permissions})
		return response(201, string(data)), nil
	}), func() time.Time { return appTime })
	c := &Connection{Load: func() (*App, error) { return a, nil }}
	if token, err := c.ContentsToken(context.Background(), "owner/repo"); err != nil || token != "write-token" {
		t.Fatal(token, err)
	}
	if len(a.tokens) != 1 || a.tokens["owner/repo:pull_requests:read"].Token != "" {
		t.Fatal("write grant reused for read")
	}
	broken := &Connection{Load: func() (*App, error) { return nil, errors.New("missing credentials") }}
	if _, err := broken.ContentsToken(context.Background(), "owner/repo"); err == nil {
		t.Fatal("failure hidden")
	}
	var response pullResponse
	if err := json.Unmarshal([]byte(`{"number":1,"head":{"sha":"abc","ref":"feature","repo":{"full_name":"o/r"}}}`), &response); err != nil {
		t.Fatal(err)
	}
	if response.pullRequest().HeadRef != "feature" {
		t.Fatal("lost branch identity")
	}
}
