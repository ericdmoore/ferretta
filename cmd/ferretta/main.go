package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/ericdmoore/ferretta/internal/cli"
	"github.com/ericdmoore/ferretta/internal/github"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	client := github.Client{HTTP: &http.Client{Timeout: 15 * time.Second}, Token: os.Getenv("GITHUB_TOKEN")}
	os.Exit(cli.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, client))
}
