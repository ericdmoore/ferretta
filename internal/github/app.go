package github

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// App holds installation credentials privately; only scoped, read-only tokens
// leave this adapter. It never uses personal credentials or retries dispatches.
type App struct {
	clientID       string
	installationID int64
	key            *rsa.PrivateKey
	http           Doer
	now            func() time.Time
	mu             sync.Mutex
	tokens         map[string]installationToken
}
type installationToken struct {
	Token       string            `json:"token"`
	ExpiresAt   time.Time         `json:"expires_at"`
	Permissions map[string]string `json:"permissions"`
}
type Identity struct {
	AppID          int64  `json:"app_id"`
	InstallationID int64  `json:"installation_id"`
	BotLogin       string `json:"bot_login"`
	Repository     string `json:"repository"`
}

func NewApp(clientID string, installationID int64, keyPEM []byte, client Doer, now func() time.Time) (*App, error) {
	if !regexp.MustCompile(`^[A-Za-z0-9_]+$`).MatchString(clientID) || installationID <= 0 || client == nil || now == nil {
		return nil, fmt.Errorf("app client ID, positive installation ID, HTTP client and clock are required")
	}
	block, rest := pem.Decode(keyPEM)
	if block == nil || len(bytes.TrimSpace(rest)) != 0 {
		return nil, fmt.Errorf("expected one RSA private key in PEM format")
	}
	var key *rsa.PrivateKey
	switch block.Type {
	case "RSA PRIVATE KEY":
		key, _ = x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		parsed, _ := x509.ParsePKCS8PrivateKey(block.Bytes)
		key, _ = parsed.(*rsa.PrivateKey)
	}
	if key == nil || key.N.BitLen() < 2048 || key.Validate() != nil {
		return nil, fmt.Errorf("expected a valid RSA private key of at least 2048 bits")
	}
	return &App{clientID: clientID, installationID: installationID, key: key, http: client, now: now, tokens: make(map[string]installationToken)}, nil
}

func (a *App) jwt() (string, error) {
	if a == nil || a.key == nil {
		return "", fmt.Errorf("GitHub App is not configured")
	}
	now := a.now()
	payload, _ := json.Marshal(struct {
		Iss string `json:"iss"`
		Iat int64  `json:"iat"`
		Exp int64  `json:"exp"`
	}{a.clientID, now.Add(-time.Minute).Unix(), now.Add(9 * time.Minute).Unix()})
	encode := base64.RawURLEncoding.EncodeToString
	unsigned := encode([]byte(`{"alg":"RS256","typ":"JWT"}`)) + "." + encode(payload)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(nil, a.key, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("could not sign GitHub App authentication")
	}
	return unsigned + "." + encode(signature), nil
}

func (a *App) request(ctx context.Context, method, path, token string, body []byte, result any) error {
	req, _ := http.NewRequestWithContext(ctx, method, "https://api.github.com"+path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "ferretta")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := a.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("GitHub App request failed; no automatic retry")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("GitHub App returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return fmt.Errorf("could not read bounded GitHub App response")
	}
	if json.Unmarshal(data, result) != nil {
		return fmt.Errorf("invalid GitHub App response")
	}
	return nil
}

func ValidRepository(repo string) bool {
	return regexp.MustCompile(`^[A-Za-z0-9_-]+/[A-Za-z0-9_.-]+$`).MatchString(repo) && !strings.HasSuffix(repo, "/.") && !strings.HasSuffix(repo, "/..")
}

func (a *App) installation(ctx context.Context, repo, jwt string) (Identity, error) {
	var info struct {
		ID          int64      `json:"id"`
		AppID       int64      `json:"app_id"`
		AppSlug     string     `json:"app_slug"`
		SuspendedAt *time.Time `json:"suspended_at"`
	}
	if err := a.request(ctx, "GET", "/repos/"+repo+"/installation", jwt, nil, &info); err != nil {
		return Identity{}, err
	}
	if info.ID != a.installationID || info.AppID <= 0 || info.AppSlug == "" || info.SuspendedAt != nil {
		return Identity{}, fmt.Errorf("repository does not have the configured active app installation")
	}
	return Identity{AppID: info.AppID, InstallationID: info.ID, BotLogin: info.AppSlug + "[bot]", Repository: repo}, nil
}

// Token caches per-repository tokens and renews before expiry. Clock is injected;
// no polling, sleeps, or retry policy is hidden in this adapter.
func (a *App) Token(ctx context.Context, repo string) (string, error) {
	if !ValidRepository(repo) {
		return "", fmt.Errorf("provide a valid owner/repository")
	}
	jwt, err := a.jwt()
	if err != nil {
		return "", err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	cacheKey := strings.ToLower(repo)
	if cached := a.tokens[cacheKey]; cached.ExpiresAt.After(a.now().Add(time.Minute)) {
		return cached.Token, nil
	}
	if _, err := a.installation(ctx, repo, jwt); err != nil {
		return "", err
	}
	body, _ := json.Marshal(struct {
		Repositories []string          `json:"repositories"`
		Permissions  map[string]string `json:"permissions"`
	}{[]string{strings.Split(repo, "/")[1]}, map[string]string{"contents": "read", "pull_requests": "read"}})
	var token installationToken
	if err := a.request(ctx, "POST", fmt.Sprintf("/app/installations/%d/access_tokens", a.installationID), jwt, body, &token); err != nil {
		return "", err
	}
	if strings.TrimSpace(token.Token) == "" || strings.ContainsAny(token.Token, "\r\n") || !token.ExpiresAt.After(a.now().Add(time.Minute)) || token.Permissions["contents"] != "read" || token.Permissions["pull_requests"] != "read" {
		return "", fmt.Errorf("invalid installation token or missing read permissions")
	}
	for name, level := range token.Permissions {
		if level != "read" || (name != "contents" && name != "pull_requests" && name != "metadata") {
			return "", fmt.Errorf("installation token exceeded requested permissions")
		}
	}
	a.tokens[cacheKey] = token
	return token.Token, nil
}

func (a *App) Status(ctx context.Context, repo string) (Identity, error) {
	if !ValidRepository(repo) {
		return Identity{}, fmt.Errorf("provide a valid owner/repository")
	}
	jwt, err := a.jwt()
	if err != nil {
		return Identity{}, err
	}
	identity, err := a.installation(ctx, repo, jwt)
	if err != nil {
		return Identity{}, err
	}
	// A successful installation lookup alone does not establish token permissions.
	token, err := a.Token(ctx, repo)
	if err != nil {
		return Identity{}, err
	}
	var repository struct {
		FullName string `json:"full_name"`
	}
	if err := a.request(ctx, "GET", "/repos/"+repo, token, nil, &repository); err != nil {
		return Identity{}, err
	}
	if !strings.EqualFold(repository.FullName, repo) {
		return Identity{}, fmt.Errorf("GitHub returned a different repository")
	}
	return identity, nil
}

func (a *App) client(repo string) Client {
	return Client{HTTP: a.http, TokenSource: func(ctx context.Context) (string, error) { return a.Token(ctx, repo) }}
}
func (a *App) Comments(ctx context.Context, repo string, pr int) ([]Comment, error) {
	return a.client(repo).Comments(ctx, repo, pr)
}
func (a *App) PullRequest(ctx context.Context, repo string, pr int) (PullRequest, error) {
	return a.client(repo).PullRequest(ctx, repo, pr)
}

func (a *App) OpenPullRequests(ctx context.Context, repo string) ([]PullRequest, error) {
	return a.client(repo).OpenPullRequests(ctx, repo)
}
