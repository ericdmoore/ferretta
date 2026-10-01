package review

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ericdmoore/ferretta/internal/github"
)

type fakeRetryQueue struct {
	items map[string][]byte
	err   error
}

func (q *fakeRetryQueue) QueueRetry(_ context.Context, id string, data []byte) error {
	if q.err != nil {
		return q.err
	}
	if q.items == nil {
		q.items = map[string][]byte{}
	}
	q.items[id] = data
	return nil
}

const webhookEvent = `{"action":"rerequested","installation":{"id":9},"repository":{"full_name":"o/r"},"sender":{"login":"human","type":"User"},"check_run":{"id":1,"app":{"id":7}}}`

var webhookSecret = []byte(strings.Repeat("s", 32))

type unreadableSecret struct{ *os.File }

func (unreadableSecret) Read([]byte) (int, error) { return 0, errors.New("storage read failure") }

func TestWebhookSecretStorageErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, webhookSecret, 0600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := readWebhookSecret(unreadableSecret{f}); err == nil {
		t.Fatal("read failure ignored")
	}
	f.Close()
	if _, err := readWebhookSecret(f); err == nil {
		t.Fatal("stat failure ignored")
	}
}

func signedRequest(body string) *http.Request {
	r := httptest.NewRequest("POST", "http://localhost/github/webhook", strings.NewReader(body))
	mac := hmac.New(sha256.New, webhookSecret)
	_, _ = mac.Write([]byte(body))
	r.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	r.Header.Set("X-GitHub-Event", "check_run")
	r.Header.Set("X-GitHub-Delivery", "delivery-1")
	return r
}
func TestRetryWebhookAuthentication(t *testing.T) {
	for _, mode := range []string{"valid", "redelivery", "method", "path", "oversized", "broken-body", "signature", "missing-prefix", "malformed-signature", "json", "ping", "other-event", "other-action", "sender", "bot", "app", "installation", "repo", "id", "check", "store-failure"} {
		t.Run(mode, func(t *testing.T) {
			body := webhookEvent
			switch mode {
			case "json":
				body = "{"
			case "oversized":
				body = strings.Repeat("x", (1<<20)+1)
			case "other-action":
				body = strings.Replace(body, "rerequested", "completed", 1)
			case "sender":
				body = strings.Replace(body, "human", "stranger", 1)
			case "bot":
				body = strings.Replace(body, "User", "Bot", 1)
			case "app":
				body = strings.Replace(body, `"id":7`, `"id":8`, 1)
			case "installation":
				body = strings.Replace(body, `"id":9`, `"id":10`, 1)
			case "repo":
				body = strings.Replace(body, "o/r", "other/repo", 1)
			case "check":
				body = strings.Replace(body, `"id":1,`, `"id":0,`, 1)
			}
			r := signedRequest(body)
			q := &fakeRetryQueue{}
			switch mode {
			case "method":
				r.Method = "GET"
			case "path":
				r.URL.Path = "/other"
			case "broken-body":
				r.Body = io.NopCloser(errorReader{})
			case "signature":
				r.Header.Set("X-Hub-Signature-256", "sha256="+strings.Repeat("0", 64))
			case "missing-prefix":
				r.Header.Set("X-Hub-Signature-256", strings.TrimPrefix(r.Header.Get("X-Hub-Signature-256"), "sha256="))
			case "malformed-signature":
				r.Header.Set("X-Hub-Signature-256", "sha256=zz")
			case "ping":
				r.Header.Set("X-GitHub-Event", "ping")
			case "other-event":
				r.Header.Set("X-GitHub-Event", "pull_request")
			case "id":
				r.Header.Set("X-GitHub-Delivery", "bad id")
			case "store-failure":
				q.err = errors.New("disk full")
			}
			h := retryWebhook(webhookSecret, github.Identity{AppID: 7, InstallationID: 9}, "o/r", []string{"HUMAN"}, q)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if mode == "valid" || mode == "redelivery" {
				if w.Code != 202 || len(q.items) != 1 {
					t.Fatal(w.Code, w.Body.String())
				}
				var req retryRequest
				if err := json.Unmarshal(q.items["delivery-1"], &req); err != nil || req.Actor != "human" || req.CheckID != 1 {
					t.Fatal(req, err)
				}
				if mode == "redelivery" {
					h.ServeHTTP(httptest.NewRecorder(), signedRequest(body))
					if len(q.items) != 1 {
						t.Fatal("duplicate")
					}
				}
			} else {
				if len(q.items) != 0 {
					t.Fatal("untrusted or irrelevant event admitted", mode)
				}
				if mode == "ping" || mode == "other-event" || mode == "other-action" {
					if w.Code != 204 {
						t.Fatal(w.Code)
					}
				} else if w.Code < 400 {
					t.Fatal("failure acknowledged", w.Code)
				}
			}
		})
	}
}

func TestRetryWebhookListener(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "secret")
	if err := os.WriteFile(secret, webhookSecret, 0600); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"invalid-address", "public-address", "missing-secret", "directory", "public-secret", "short-secret", "large-secret", "busy-port", "valid"} {
		t.Run(mode, func(t *testing.T) {
			address, path := "127.0.0.1:0", secret
			switch mode {
			case "invalid-address":
				address = "localhost:8080"
			case "public-address":
				address = "0.0.0.0:8080"
			case "missing-secret":
				path = filepath.Join(dir, "missing")
			case "directory":
				path = dir
			case "public-secret":
				path = filepath.Join(dir, "public")
				_ = os.WriteFile(path, webhookSecret, 0644)
				if err := os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
			case "short-secret":
				path = filepath.Join(dir, "short")
				_ = os.WriteFile(path, []byte("short"), 0600)
			case "large-secret":
				path = filepath.Join(dir, "large")
				_ = os.WriteFile(path, []byte(strings.Repeat("s", 4097)), 0600)
			case "busy-port":
				l, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				defer l.Close()
				address = l.Addr().String()
			}
			stop, err := serveRetryWebhook(address, path, func(got []byte) http.Handler {
				if string(got) != string(webhookSecret) {
					t.Fatal("secret mismatch")
				}
				return http.NotFoundHandler()
			})
			if mode == "valid" {
				if err != nil {
					t.Fatal(err)
				}
				stop()
			} else if err == nil {
				stop()
				t.Fatal("invalid listener accepted")
			}
		})
	}
}
