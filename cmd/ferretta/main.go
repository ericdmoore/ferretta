package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/ericdmoore/ferretta/internal/cli"
	"github.com/ericdmoore/ferretta/internal/github"
	"github.com/ericdmoore/ferretta/internal/review"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	client := &github.Connection{Load: github.LoadDefaultApp}
	os.Exit(cli.RunWithReviewer(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, client, review.NewCLI(client)))
}
