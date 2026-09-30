package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Connection lazily loads machine-owned credentials, so offline commands need
// no authentication. It retains tokens only in memory for the process lifetime.
type Connection struct {
	Load func() (*App, error)
	once sync.Once
	app  *App
	err  error
}

func (c *Connection) get() (*App, error) {
	c.once.Do(func() {
		if c.Load == nil {
			c.err = fmt.Errorf("GitHub App loader is required")
			return
		}
		c.app, c.err = c.Load()
		if c.app == nil && c.err == nil {
			c.err = fmt.Errorf("GitHub App loader returned no connection")
		}
	})
	return c.app, c.err
}
func (c *Connection) Token(ctx context.Context, repo string) (string, error) {
	a, e := c.get()
	if e != nil {
		return "", e
	}
	return a.Token(ctx, repo)
}
func (c *Connection) Status(ctx context.Context, repo string) (Identity, error) {
	a, e := c.get()
	if e != nil {
		return Identity{}, e
	}
	return a.Status(ctx, repo)
}
func (c *Connection) Comments(ctx context.Context, repo string, pr int) ([]Comment, error) {
	a, e := c.get()
	if e != nil {
		return nil, e
	}
	return a.Comments(ctx, repo, pr)
}
func (c *Connection) PullRequest(ctx context.Context, repo string, pr int) (PullRequest, error) {
	a, e := c.get()
	if e != nil {
		return PullRequest{}, e
	}
	return a.PullRequest(ctx, repo, pr)
}

type AppConfig struct {
	ClientID       string `json:"client_id"`
	InstallationID int64  `json:"installation_id"`
	PrivateKeyFile string `json:"private_key_file"`
}

func (c *Connection) OpenPullRequests(ctx context.Context, repo string) ([]PullRequest, error) {
	a, err := c.get()
	if err != nil {
		return nil, err
	}
	return a.OpenPullRequests(ctx, repo)
}

func DefaultConfigPath() (string, error) {
	if path := os.Getenv("FERRETTA_GITHUB_CONFIG"); path != "" {
		if !filepath.IsAbs(path) {
			return "", fmt.Errorf("FERRETTA_GITHUB_CONFIG must be an absolute machine configuration path")
		}
		return path, nil
	}
	root, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "ferretta", "github.json"), nil
}

func LoadDefaultApp() (*App, error) {
	path, err := DefaultConfigPath()
	if err != nil {
		return nil, err
	}
	return LoadApp(path, NewHTTPClient(), time.Now)
}

func NewHTTPClient() *http.Client {
	return &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return fmt.Errorf("GitHub credential redirects are not allowed")
	}}
}

func LoadApp(path string, client Doer, now func() time.Time) (*App, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read GitHub App configuration at %s: %w; see docs/github-app-auth.md", path, err)
	}
	var config AppConfig
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&config) != nil || decoder.Decode(new(any)) != io.EOF {
		return nil, fmt.Errorf("invalid GitHub App configuration")
	}
	return appFromConfig(config, client, now)
}

func appFromConfig(config AppConfig, client Doer, now func() time.Time) (*App, error) {
	if !filepath.IsAbs(config.PrivateKeyFile) {
		return nil, fmt.Errorf("private_key_file must be an absolute path")
	}
	key, err := os.Open(config.PrivateKeyFile)
	if err != nil {
		return nil, fmt.Errorf("cannot open GitHub App private key")
	}
	defer key.Close()
	info, err := key.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, fmt.Errorf("GitHub App private key must be a regular file accessible only to its owner (chmod 600)")
	}
	pem, err := io.ReadAll(io.LimitReader(key, 65537))
	if err != nil || len(pem) > 65536 {
		return nil, fmt.Errorf("cannot read bounded GitHub App private key")
	}
	return NewApp(config.ClientID, config.InstallationID, pem, client, now)
}
