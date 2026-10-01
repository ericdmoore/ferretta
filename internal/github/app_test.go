package github

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var testKeyOnce sync.Once
var testKey *rsa.PrivateKey

func appKey(t *testing.T) []byte {
	t.Helper()
	testKeyOnce.Do(func() {
		var err error
		testKey, err = rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			panic(err)
		}
	})
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(testKey)})
}

var appTime = time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)

const installationJSON = `{"id":42,"app_id":7,"app_slug":"ferretta-test","suspended_at":null}`

func tokenJSON(now time.Time) string {
	return fmt.Sprintf(`{"token":"installation-secret","expires_at":%q,"permissions":{"contents":"read","pull_requests":"read","metadata":"read"}}`, now.Add(time.Hour).Format(time.RFC3339))
}
func makeApp(t *testing.T, client Doer, now func() time.Time) *App {
	t.Helper()
	a, e := NewApp("Iv1_test", 42, appKey(t), client, now)
	if e != nil {
		t.Fatal(e)
	}
	return a
}
func verifyJWT(t *testing.T, token string, now time.Time) {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatal("invalid JWT")
	}
	decode := base64.RawURLEncoding.DecodeString
	header, _ := decode(parts[0])
	var h map[string]string
	if json.Unmarshal(header, &h) != nil || h["alg"] != "RS256" {
		t.Fatal("wrong algorithm")
	}
	body, _ := decode(parts[1])
	var claims struct {
		Iss      string
		Iat, Exp int64
	}
	if json.Unmarshal(body, &claims) != nil || claims.Iss != "Iv1_test" || claims.Iat != now.Add(-time.Minute).Unix() || claims.Exp != now.Add(9*time.Minute).Unix() {
		t.Fatal("wrong JWT claims")
	}
	signature, _ := decode(parts[2])
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if rsa.VerifyPKCS1v15(&testKey.PublicKey, crypto.SHA256, digest[:], signature) != nil {
		t.Fatal("invalid signature")
	}
}
func TestAppScopeRefreshAndIdentity(t *testing.T) {
	now := appTime
	exchanges := 0
	a := makeApp(t, transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "api.github.com" {
			t.Fatal("wrong authority")
		}
		if r.Header.Get("X-GitHub-Api-Version") == "" {
			t.Fatal("missing version")
		}
		auth := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		switch r.URL.Path {
		case "/repos/owner/repo/installation", "/repos/Owner/Repo/installation":
			verifyJWT(t, auth, now)
			return response(200, installationJSON), nil
		case "/app/installations/42/access_tokens":
			verifyJWT(t, auth, now)
			exchanges++
			var body struct {
				Repositories []string
				Permissions  map[string]string
			}
			if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&body) != nil || len(body.Repositories) != 1 || body.Repositories[0] != "repo" || len(body.Permissions) != 2 || body.Permissions["contents"] != "read" || body.Permissions["pull_requests"] != "read" {
				t.Fatal("token scope broadened")
			}
			return response(201, tokenJSON(now)), nil
		case "/repos/owner/repo":
			if auth != "installation-secret" {
				t.Fatal("wrong API credential")
			}
			return response(200, `{"full_name":"owner/repo"}`), nil
		case "/repos/owner/repo/pulls/1":
			if auth != "installation-secret" {
				t.Fatal("wrong PR credential")
			}
			return response(200, `{"number":1,"head":{"sha":"abc","repo":{"full_name":"owner/repo"}},"user":{"login":"human"},"base":{"sha":"def"},"state":"open","html_url":"https://github.com/owner/repo/pull/1","title":"Review","body":"Example","draft":false}`), nil
		default:
			return response(200, `[]`), nil
		}
	}), func() time.Time { return now })
	c := &Connection{Load: func() (*App, error) { return a, nil }}
	id, err := c.Status(context.Background(), "owner/repo")
	if err != nil || id.BotLogin != "ferretta-test[bot]" || id.AppID != 7 || id.InstallationID != 42 {
		t.Fatal(id, err)
	}
	if token, err := c.Token(context.Background(), "Owner/Repo"); err != nil || token != "installation-secret" {
		t.Fatal("case-insensitive cache", err)
	}
	if pr, err := c.PullRequest(context.Background(), "owner/repo", 1); err != nil || pr.Head != "abc" || pr.Base != "def" || pr.State != "OPEN" || pr.Title != "Review" || pr.Author != "human" || pr.HeadRepository != "owner/repo" {
		t.Fatal(pr, err)
	}
	if comments, err := c.Comments(context.Background(), "owner/repo", 1); err != nil || len(comments) != 0 {
		t.Fatal(comments, err)
	}
	if exchanges != 1 {
		t.Fatal("unnecessary token exchange", exchanges)
	}
	now = now.Add(59 * time.Minute)
	if _, err := a.Token(context.Background(), "owner/repo"); err != nil || exchanges != 2 {
		t.Fatal("missing renewal", err, exchanges)
	}
}
func TestParallelTokenAdmission(t *testing.T) {
	calls := 0
	a := makeApp(t, transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == "POST" {
			calls++
			return response(201, tokenJSON(appTime)), nil
		}
		return response(200, installationJSON), nil
	}), func() time.Time { return appTime })
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			if _, e := a.Token(context.Background(), "owner/repo"); e != nil {
				t.Error(e)
			}
		})
	}
	wg.Wait()
	if calls != 1 {
		t.Fatal("duplicate concurrent exchange", calls)
	}
}
func TestAppFailureBoundaries(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"network", "cancel", "status", "body-read", "oversize", "json", "wrong-install", "suspended", "missing-app", "token-error", "token-empty", "token-expired", "token-permissions", "token-extra", "token-newline", "repo-error", "repo-wrong"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			ctx := ctx
			if mode == "cancel" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			a := makeApp(t, transportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if mode == "network" {
					return nil, errors.New("secret-key-details")
				}
				if mode == "status" {
					return response(401, "secret-key-details"), nil
				}
				if mode == "body-read" {
					return &http.Response{StatusCode: 200, Body: failingReader{}}, nil
				}
				if mode == "oversize" {
					return response(200, strings.Repeat("x", (1<<20)+1)), nil
				}
				if mode == "json" {
					return response(200, "secret-key-details"), nil
				}
				if strings.HasSuffix(r.URL.Path, "/installation") {
					switch mode {
					case "wrong-install":
						return response(200, strings.Replace(installationJSON, `"id":42`, `"id":99`, 1)), nil
					case "suspended":
						return response(200, strings.Replace(installationJSON, "null", `"2026-01-01T00:00:00Z"`, 1)), nil
					case "missing-app":
						return response(200, `{"id":42}`), nil
					}
					return response(200, installationJSON), nil
				}
				if r.Method == "POST" {
					body := tokenJSON(appTime)
					switch mode {
					case "token-error":
						return response(503, "secret-key-details"), nil
					case "token-empty":
						body = `{}`
					case "token-expired":
						body = tokenJSON(appTime.Add(-time.Hour))
					case "token-permissions":
						body = strings.ReplaceAll(body, `"read"`, `"write"`)
					case "token-extra":
						body = strings.Replace(body, `"metadata":"read"`, `"administration":"read"`, 1)
					case "token-newline":
						body = strings.Replace(body, "installation-secret", `bad\nheader`, 1)
					}
					return response(201, body), nil
				}
				if mode == "repo-error" {
					return response(403, "secret-key-details"), nil
				}
				return response(200, `{"full_name":"other/repo"}`), nil
			}), func() time.Time { return appTime })
			_, err := a.Status(ctx, "owner/repo")
			if err == nil || strings.Contains(err.Error(), "secret-key-details") {
				t.Fatal("unsafe success/error", err)
			}
			if mode == "network" && calls != 1 {
				t.Fatal("automatic retry")
			}
		})
	}
	var zero App
	if _, e := zero.Token(ctx, "o/r"); e == nil {
		t.Fatal("zero app accepted")
	}
	if _, e := zero.Status(ctx, "o/r"); e == nil {
		t.Fatal("zero app accepted")
	}
	for _, repo := range []string{"../r", "owner/..", "o/r?x", "o/r/x", "o/.", "o/"} {
		if _, e := zero.Token(ctx, repo); e == nil {
			t.Fatal("invalid repo accepted")
		}
		if _, e := zero.Status(ctx, repo); e == nil {
			t.Fatal("invalid repo accepted")
		}
	}
	a := makeApp(t, transportFunc(func(r *http.Request) (*http.Response, error) { return nil, r.Context().Err() }), func() time.Time { return appTime })
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if e := a.request(canceled, "GET", "/app", "jwt", nil, new(any)); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if _, e := a.Token(canceled, "o/r"); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}
func TestPrivateKeyValidation(t *testing.T) {
	good := appKey(t)
	client := NewHTTPClient()
	now := func() time.Time { return appTime }
	pkcs8, _ := x509.MarshalPKCS8PrivateKey(testKey)
	if _, e := NewApp("123", 1, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8}), client, now); e != nil {
		t.Fatal(e)
	}
	other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	otherDER, _ := x509.MarshalPKCS8PrivateKey(other)
	for _, bad := range [][]byte{nil, []byte("secret-invalid-pem"), append(append([]byte{}, good...), good...), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("x")}), pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: []byte("x")}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: otherDER})} {
		if _, e := NewApp("123", 1, bad, client, now); e == nil || strings.Contains(e.Error(), "secret-invalid-pem") {
			t.Fatal(e)
		}
	}
	for _, id := range []string{"", "bad\nvalue"} {
		if _, e := NewApp(id, 1, good, client, now); e == nil {
			t.Fatal("invalid ID")
		}
	}
	if _, e := NewApp("123", 0, good, client, now); e == nil {
		t.Fatal("invalid installation")
	}
	if _, e := NewApp("123", 1, good, nil, now); e == nil {
		t.Fatal("nil client")
	}
	if _, e := NewApp("123", 1, good, client, nil); e == nil {
		t.Fatal("nil clock")
	}
	if e := client.CheckRedirect(nil, nil); e == nil {
		t.Fatal("redirect allowed")
	}
}
func TestMachineConfiguration(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key.pem")
	path := filepath.Join(dir, "github.json")
	if e := os.WriteFile(keyPath, appKey(t), 0600); e != nil {
		t.Fatal(e)
	}
	config, _ := json.Marshal(AppConfig{"123", 42, keyPath})
	write := func(data []byte) {
		t.Helper()
		if e := os.WriteFile(path, data, 0600); e != nil {
			t.Fatal(e)
		}
	}
	write(config)
	t.Setenv("FERRETTA_GITHUB_CONFIG", path)
	if got, e := DefaultConfigPath(); e != nil || got != path {
		t.Fatal(got, e)
	}
	if _, e := LoadDefaultApp(); e != nil {
		t.Fatal(e)
	}
	t.Setenv("FERRETTA_GITHUB_CONFIG", "relative.json")
	if _, e := LoadDefaultApp(); e == nil {
		t.Fatal("relative config accepted")
	}
	t.Setenv("FERRETTA_GITHUB_CONFIG", "")
	if got, e := DefaultConfigPath(); e != nil || !filepath.IsAbs(got) {
		t.Fatal(got, e)
	}
	for _, data := range [][]byte{[]byte(`{`), []byte(`{"unknown":true}`), append(append([]byte{}, config...), []byte(` {}`)...), []byte(`{"private_key_file":"relative.pem"}`), []byte(`{"private_key_file":"/missing/ferretta.pem"}`)} {
		write(data)
		if _, e := LoadApp(path, NewHTTPClient(), time.Now); e == nil {
			t.Fatal("invalid config accepted")
		}
	}
	write(config)
	t.Setenv("FERRETTA_GITHUB_CONFIG", path)
	if e := os.Chmod(keyPath, 0644); e != nil {
		t.Fatal(e)
	}
	if _, e := LoadDefaultApp(); e == nil {
		t.Fatal("public key file accepted")
	}
	os.Chmod(keyPath, 0600)
	if e := os.WriteFile(keyPath, []byte(strings.Repeat("x", 65537)), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := LoadDefaultApp(); e == nil {
		t.Fatal("oversized key accepted")
	}
	os.Remove(keyPath)
	if e := os.Mkdir(keyPath, 0700); e != nil {
		t.Fatal(e)
	}
	if _, e := LoadDefaultApp(); e == nil {
		t.Fatal("directory key accepted")
	}
	if _, e := LoadApp(filepath.Join(dir, "missing"), NewHTTPClient(), time.Now); e == nil {
		t.Fatal("missing configuration accepted")
	}
}
func TestConnectionLoadErrors(t *testing.T) {
	for _, c := range []*Connection{{}, {Load: func() (*App, error) { return nil, nil }}, {Load: func() (*App, error) { return nil, errors.New("load failed") }}} {
		ctx := context.Background()
		if _, e := c.Token(ctx, "o/r"); e == nil {
			t.Fatal("load failure lost")
		}
		if _, e := c.Status(ctx, "o/r"); e == nil {
			t.Fatal("load failure lost")
		}
		if _, e := c.Comments(ctx, "o/r", 1); e == nil {
			t.Fatal("load failure lost")
		}
		if _, e := c.PullRequest(ctx, "o/r", 1); e == nil {
			t.Fatal("load failure lost")
		}
	}
}
func TestPullRequestFailureMapping(t *testing.T) {
	c := Client{HTTP: transportFunc(func(*http.Request) (*http.Response, error) { return response(404, "private"), nil })}
	if _, e := c.PullRequest(context.Background(), "o/r", 1); e == nil {
		t.Fatal("missing provider error")
	}
	c.HTTP = transportFunc(func(*http.Request) (*http.Response, error) { return response(200, `{"number":2}`), nil })
	if _, e := c.PullRequest(context.Background(), "o/r", 1); e == nil {
		t.Fatal("wrong PR accepted")
	}
	if _, e := c.PullRequest(context.Background(), "bad", 1); e == nil {
		t.Fatal("bad repo accepted")
	}
	c.TokenSource = func(context.Context) (string, error) { return "", errors.New("no app credentials") }
	c.Token = "personal-token-must-not-be-used"
	if _, e := c.Comments(context.Background(), "o/r", 1); e == nil {
		t.Fatal("personal credential fallback")
	}
}

// Prove an HTTP redirect never receives the signed credential. No live sockets.
func TestAppRedirectRejected(t *testing.T) {
	client := NewHTTPClient()
	calls := 0
	client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://elsewhere.invalid/"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	a := makeApp(t, client, func() time.Time { return appTime })
	if _, e := a.Token(context.Background(), "o/r"); e == nil || calls != 1 {
		t.Fatal("redirect credential leak", e, calls)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestInstallationTokensStayBoundToRepository(t *testing.T) {
	now := appTime
	minted := map[string]int{}
	a := makeApp(t, transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == "GET" {
			return response(200, installationJSON), nil
		}
		var request struct{ Repositories []string }
		if e := json.NewDecoder(r.Body).Decode(&request); e != nil || len(request.Repositories) != 1 {
			t.Fatal("missing token restriction", e)
		}
		repo := request.Repositories[0]
		minted[repo]++
		return response(201, strings.Replace(tokenJSON(now), "installation-secret", repo+"-credential", 1)), nil
	}), func() time.Time { return now })
	for _, repo := range []string{"first", "second", "first"} {
		token, e := a.Token(context.Background(), "owner/"+repo)
		if e != nil || token != repo+"-credential" {
			t.Fatal("credential crossed repositories", e)
		}
	}
	if minted["first"] != 1 || minted["second"] != 1 {
		t.Fatal("wrong repository cache behavior")
	}
}
