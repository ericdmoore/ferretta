package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) Do(r *http.Request) (*http.Response, error) { return f(r) }
func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}
}

func TestCommentsPagination(t *testing.T) {
	calls := 0
	c := Client{Token: "test-token", HTTP: transportFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.Method != "GET" || req.Header.Get("Authorization") != "Bearer test-token" || req.Header.Get("Accept") != "application/vnd.github+json" {
			t.Fatal("incorrect request")
		}
		if req.URL.Host != "api.github.com" {
			t.Fatal("unexpected host")
		}
		switch calls {
		case 1:
			if req.URL.Path != "/repos/owner/repo/pulls/42" {
				t.Fatal(req.URL)
			}
			return response(200, `{"number":42}`), nil
		case 2:
			if req.URL.Query().Get("page") != "1" || req.URL.Query().Get("per_page") != "100" {
				t.Fatal(req.URL)
			}
			batch := make([]Comment, 100)
			for i := range batch {
				batch[i].ID = int64(i + 1)
			}
			data, _ := json.Marshal(batch)
			return response(200, string(data)), nil
		case 3:
			if req.URL.Query().Get("page") != "2" {
				t.Fatal(req.URL)
			}
			return response(200, `[{"id":101,"body":"CONFIRMED-Go-v1::","user":{"login":"human","type":"User"}}]`), nil
		default:
			t.Fatal("unexpected extra request")
			return nil, nil
		}
	})}
	comments, err := c.Comments(context.Background(), "owner/repo", 42)
	if err != nil || len(comments) != 101 || comments[100].User.Login != "human" {
		t.Fatalf("comments = %v, error = %v", comments, err)
	}
}

func TestClientFailures(t *testing.T) {
	for _, body := range []string{`{"number":2}`, `{bad`, strings.Repeat("x", (8<<20)+1)} {
		c := Client{HTTP: transportFunc(func(*http.Request) (*http.Response, error) { return response(200, body), nil })}
		if _, err := c.Comments(context.Background(), "o/r", 1); err == nil {
			t.Fatal("accepted invalid response")
		}
	}
	for _, status := range []int{401, 403, 404, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			c := Client{HTTP: transportFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return response(status, "secret-provider-details"), nil
			})}
			_, err := c.Comments(context.Background(), "o/r", 1)
			if err == nil || strings.Contains(err.Error(), "secret-provider-details") || calls != 1 {
				t.Fatalf("unsafe error/retry: %v, calls %d", err, calls)
			}
		})
	}
	c := Client{HTTP: transportFunc(func(req *http.Request) (*http.Response, error) { return nil, req.Context().Err() })}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Comments(ctx, "o/r", 1); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	c = Client{HTTP: transportFunc(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "/pulls/") {
			return response(200, `{"number":1}`), nil
		}
		return response(503, "down"), nil
	})}
	if _, err := c.Comments(context.Background(), "o/r", 1); err == nil {
		t.Fatal("accepted partial fetch")
	}
	c.BaseURL = ":invalid"
	if _, err := c.Comments(context.Background(), "o/r", 1); err == nil {
		t.Fatal("accepted invalid URL")
	}
	for _, repo := range []string{"", "owner", "o/r/x", "/r", "o/..", "o/r?x", "o/r x"} {
		if _, err := c.Comments(context.Background(), repo, 1); err == nil {
			t.Fatal("accepted invalid repo")
		}
	}
	if _, err := (Client{}).Comments(context.Background(), "o/r", 1); err == nil {
		t.Fatal("accepted nil client")
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }
func (failingReader) Close() error             { return nil }
func TestResponseReadFailure(t *testing.T) {
	c := Client{HTTP: transportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: failingReader{}}, nil
	})}
	if _, err := c.Comments(context.Background(), "o/r", 1); err == nil {
		t.Fatal("lost read error")
	}
}
