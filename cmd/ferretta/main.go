package main

import (
	"context"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/ericdmoore/ferretta/internal/cli"
	"github.com/ericdmoore/ferretta/internal/github"
	"github.com/ericdmoore/ferretta/internal/review"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, input io.Reader, output, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	client := &github.Connection{Load: github.LoadDefaultApp}
	return cli.RunWithReviewer(ctx, args, input, output, stderr, client, review.NewCLI(client))
}
