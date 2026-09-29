// Package github reads GitHub evidence. It never posts or modifies comments.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

type Client struct {
	HTTP    Doer
	BaseURL string
	Token   string
}

type Comment struct {
	ID        int64  `json:"id"`
	Body      string `json:"body"`
	URL       string `json:"html_url"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	User      struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"user"`
}

func (c Client) get(ctx context.Context, path string, result any) error {
	base := c.BaseURL
	if base == "" {
		base = "https://api.github.com"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "ferretta")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("GitHub request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub returned HTTP %d", resp.StatusCode)
	}
	// Bound individual API responses. Do not include response bodies in errors:
	// provider error text can contain credentials or private repository details.
	data, err := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
	if err != nil {
		return err
	}
	if len(data) > 8<<20 {
		return fmt.Errorf("GitHub response exceeds 8 MiB")
	}
	if err := json.Unmarshal(data, result); err != nil {
		return fmt.Errorf("decode GitHub response: %w", err)
	}
	return nil
}

func (c Client) Comments(ctx context.Context, repository string, pr int) ([]Comment, error) {
	parts := strings.Split(repository, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || pr <= 0 {
		return nil, fmt.Errorf("provide owner/repository and a positive PR number")
	}
	for _, part := range parts {
		if part == "." || part == ".." || strings.ContainsAny(part, "?#%\\ \t\r\n") {
			return nil, fmt.Errorf("invalid repository name")
		}
	}
	if c.HTTP == nil {
		return nil, fmt.Errorf("HTTP client is required")
	}
	root := "/repos/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1])
	var pull struct {
		Number int `json:"number"`
	}
	if err := c.get(ctx, root+"/pulls/"+strconv.Itoa(pr), &pull); err != nil {
		return nil, err
	}
	if pull.Number != pr {
		return nil, fmt.Errorf("GitHub returned a different PR")
	}
	comments := []Comment{}
	for page := 1; ; page++ {
		var batch []Comment
		path := root + "/issues/" + strconv.Itoa(pr) + "/comments?per_page=100&page=" + strconv.Itoa(page)
		if err := c.get(ctx, path, &batch); err != nil {
			return nil, err
		}
		comments = append(comments, batch...)
		if len(batch) < 100 {
			return comments, nil
		}
	}
}
