package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConfigureVerifiedAppAndPreserveExisting(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "app.pem")
	if err := os.WriteFile(key, appKey(t), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := AppConfig{ClientID: "Iv1_test", InstallationID: 42, PrivateKeyFile: key}
	path := filepath.Join(dir, "machine", "github.json")
	calls := 0
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		switch r.URL.Path {
		case "/repos/o/r/installation":
			return response(200, installationJSON), nil
		case "/app/installations/42/access_tokens":
			return response(201, tokenJSON(appTime)), nil
		case "/repos/o/r":
			return response(200, `{"full_name":"o/r"}`), nil
		}
		return nil, fmt.Errorf("unexpected API call")
	})
	ctx := context.Background()
	now := func() time.Time { return appTime }
	identity, err := Configure(ctx, "o/r", path, cfg, transport, now)
	if err != nil || identity.BotLogin != "ferretta-test[bot]" {
		t.Fatal(identity, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var stored AppConfig
	if err := json.Unmarshal(data, &stored); err != nil || stored != cfg {
		t.Fatal(string(data), err)
	}
	if strings.Contains(string(data), "PRIVATE KEY") || strings.Contains(string(data), "installation-secret") {
		t.Fatal("secret persisted")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("unsafe config permissions", err)
	}
	before := calls
	if _, err := Configure(ctx, "o/r", path, cfg, transport, now); err == nil || calls != before {
		t.Fatal("existing config overwritten or unnecessary token exchange")
	}
	for _, bad := range []struct{ repo, path string }{{"bad", path}, {"o/r", "relative"}} {
		if _, err := Configure(ctx, bad.repo, bad.path, cfg, transport, now); err == nil {
			t.Fatal("invalid target")
		}
	}
	if _, err := Configure(ctx, "o/r", filepath.Join(dir, "bad-key.json"), AppConfig{}, transport, now); err == nil {
		t.Fatal("invalid key config")
	}
	for _, mode := range []string{"denied", "cancel", "race", "directory"} {
		t.Run(mode, func(t *testing.T) {
			target := filepath.Join(t.TempDir(), "machine", "github.json")
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			client := transportFunc(func(r *http.Request) (*http.Response, error) {
				if mode == "denied" {
					return response(403, "private details"), nil
				}
				if r.URL.Path == "/repos/o/r" {
					switch mode {
					case "cancel":
						cancel()
					case "race":
						if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(target, []byte("other writer"), 0600); err != nil {
							t.Fatal(err)
						}
					case "directory":
						if err := os.WriteFile(filepath.Dir(target), []byte("file"), 0600); err != nil {
							t.Fatal(err)
						}
					}
				}
				return transport.Do(r)
			})
			if _, err := Configure(ctx, "o/r", target, cfg, client, now); err == nil {
				t.Fatal("failure hidden")
			}
			if mode == "race" {
				data, err := os.ReadFile(target)
				if err != nil || string(data) != "other writer" {
					t.Fatal("concurrent configuration overwritten")
				}
			}
		})
	}
}

func TestConnectionConfigurePath(t *testing.T) {
	t.Setenv("FERRETTA_GITHUB_CONFIG", "relative")
	c := &Connection{}
	if _, err := c.Configure(context.Background(), "o/r", AppConfig{}); err == nil {
		t.Fatal("relative config allowed")
	}
	t.Setenv("FERRETTA_GITHUB_CONFIG", filepath.Join(t.TempDir(), "github.json"))
	if _, err := c.Configure(context.Background(), "o/r", AppConfig{}); err == nil {
		t.Fatal("invalid key allowed")
	}
}
