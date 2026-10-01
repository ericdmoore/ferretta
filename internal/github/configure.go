package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func (c *Connection) Configure(ctx context.Context, repo string, config AppConfig) (Identity, error) {
	path, err := DefaultConfigPath()
	if err != nil {
		return Identity{}, err
	}
	return Configure(ctx, repo, path, config, NewHTTPClient(), time.Now)
}

// Configure verifies App identity and repository access before storing references.
// It never overwrites existing configuration or stores the private key/token.
func Configure(ctx context.Context, repo, path string, config AppConfig, http Doer, now func() time.Time) (Identity, error) {
	if !ValidRepository(repo) || !filepath.IsAbs(path) {
		return Identity{}, fmt.Errorf("valid repository and absolute machine config path required")
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		return Identity{}, fmt.Errorf("configuration already exists or cannot be inspected; inspect %s before changing it", path)
	}
	app, err := appFromConfig(config, http, now)
	if err != nil {
		return Identity{}, err
	}
	identity, err := app.Status(ctx, repo)
	if err != nil {
		return Identity{}, err
	}
	if err := ctx.Err(); err != nil {
		return Identity{}, err
	}
	data, _ := json.MarshalIndent(config, "", "  ")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return Identity{}, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return Identity{}, err
	}
	_, writeErr := f.Write(append(data, '\n'))
	if err := errors.Join(writeErr, f.Close()); err != nil {
		return Identity{}, fmt.Errorf("configuration write incomplete; inspect %s before retrying: %w", path, err)
	}
	return identity, nil
}
