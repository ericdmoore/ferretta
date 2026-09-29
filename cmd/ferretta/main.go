package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/ericdmoore/ferretta/internal/cli"
	"github.com/ericdmoore/ferretta/internal/github"
	"github.com/ericdmoore/ferretta/internal/review"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	client := github.Client{HTTP: &http.Client{Timeout: 15 * time.Second}, Token: os.Getenv("GITHUB_TOKEN")}
	os.Exit(cli.RunWithReviewer(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, client, review.NewCLI()))
}
