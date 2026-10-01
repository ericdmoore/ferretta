package review

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"time"

	"github.com/ericdmoore/ferretta/internal/github"
)

type retryQueue interface {
	QueueRetry(context.Context, string, []byte) error
}

// GitHub is responsible for authenticating sender identity. We trust it only
// after checking the signature of the exact body, App installation and allowlist.
func retryWebhook(secret []byte, identity github.Identity, repo string, humans []string, queue retryQueue) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/github/webhook" {
			http.Error(w, "POST /github/webhook required", http.StatusNotFound)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			http.Error(w, "invalid or oversized body", http.StatusBadRequest)
			return
		}
		signature, err := hex.DecodeString(strings.TrimPrefix(r.Header.Get("X-Hub-Signature-256"), "sha256="))
		mac := hmac.New(sha256.New, secret)
		_, _ = mac.Write(body)
		if err != nil || !strings.HasPrefix(r.Header.Get("X-Hub-Signature-256"), "sha256=") || !hmac.Equal(signature, mac.Sum(nil)) {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
		var event struct {
			Action       string             `json:"action"`
			Check        github.CheckRun    `json:"check_run"`
			Installation struct{ ID int64 } `json:"installation"`
			Repository   struct {
				FullName string `json:"full_name"`
			} `json:"repository"`
			Sender struct{ Login, Type string } `json:"sender"`
		}
		if err := json.Unmarshal(body, &event); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if r.Header.Get("X-GitHub-Event") == "ping" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Header.Get("X-GitHub-Event") != "check_run" || event.Action != "rerequested" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		allowed := false
		for _, human := range humans {
			allowed = allowed || strings.EqualFold(human, event.Sender.Login)
		}
		if !allowed || event.Sender.Type != "User" || event.Installation.ID != identity.InstallationID || event.Check.App.ID != identity.AppID || !strings.EqualFold(event.Repository.FullName, repo) {
			http.Error(w, "retry sender, installation or repository not authorized", http.StatusForbidden)
			return
		}
		request := retryRequest{ID: r.Header.Get("X-GitHub-Delivery"), CheckID: event.Check.ID, Actor: event.Sender.Login}
		if err := request.validate(); err != nil {
			http.Error(w, "invalid retry identity", http.StatusBadRequest)
			return
		}
		data, _ := json.Marshal(request)
		if err := queue.QueueRetry(r.Context(), request.ID, data); err != nil {
			http.Error(w, "could not persist retry receipt", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})
}

// The service listens only on loopback. Operators provide an HTTPS reverse
// proxy/tunnel that forwards the untouched signed request body and headers.
func serveRetryWebhook(address, secretPath string, handler func([]byte) http.Handler) (func(), error) {
	endpoint, err := netip.ParseAddrPort(address)
	if err != nil || !endpoint.Addr().IsLoopback() {
		return nil, fmt.Errorf("webhook listener must be a loopback IP:port")
	}
	file, err := os.Open(secretPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	secret, err := readWebhookSecret(file)
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}
	server := &http.Server{Handler: handler(secret), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second}
	go func() { _ = server.Serve(listener) }()
	return func() { _ = server.Close() }, nil
}

func readWebhookSecret(file interface {
	io.Reader
	Stat() (os.FileInfo, error)
}) ([]byte, error) {
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, fmt.Errorf("webhook secret must be a private regular file (chmod 600)")
	}
	secret, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil {
		return nil, err
	}
	secret = []byte(strings.TrimSpace(string(secret)))
	if len(secret) < 32 || len(secret) > 4096 {
		return nil, fmt.Errorf("webhook secret must contain 32–4096 bytes")
	}
	return secret, nil
}
