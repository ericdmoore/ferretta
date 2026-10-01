package service

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ericdmoore/ferretta/internal/github"
)

type CLI struct{ Source Source }

func (c CLI) Run(ctx context.Context, args []string, output, stderr io.Writer) int {
	fail := func(err error) int { fmt.Fprintln(stderr, err); return 1 }
	if len(args) == 0 || (args[0] != "run" && args[0] != "status") {
		return fail(fmt.Errorf("usage: ferretta service run --repo owner/repo [--state /absolute/path] [--interval 1m] [--once] | ferretta service status [--state /absolute/path]"))
	}
	flags := flag.NewFlagSet("service "+args[0], flag.ContinueOnError)
	flags.SetOutput(stderr)
	state := flags.String("state", "", "absolute private state directory (default: user config directory/ferretta/state)")
	interval := time.Minute
	once := false
	var repos []string
	if args[0] == "run" {
		flags.DurationVar(&interval, "interval", time.Minute, "delay between polls (5s–24h)")
		flags.BoolVar(&once, "once", false, "poll once and exit without inference")
		flags.Func("repo", "repository to watch; repeat for multiple repositories", func(repo string) error {
			if !github.ValidRepository(repo) {
				return fmt.Errorf("provide owner/repository")
			}
			repo = strings.ToLower(repo)
			for _, existing := range repos {
				if existing == repo {
					return fmt.Errorf("duplicate repository")
				}
			}
			repos = append(repos, repo)
			return nil
		})
	}
	if err := flags.Parse(args[1:]); err != nil {
		return 1
	}
	if flags.NArg() != 0 {
		return fail(fmt.Errorf("positional arguments are not accepted"))
	}
	if *state == "" {
		root, err := os.UserConfigDir()
		if err != nil {
			return fail(err)
		}
		*state = filepath.Join(root, "ferretta", "state")
	}
	if args[0] == "status" {
		status, err := ReadStatus(ctx, *state)
		if err != nil {
			return fail(err)
		}
		if err := json.NewEncoder(output).Encode(status); err != nil {
			return fail(err)
		}
		return 0
	}
	if c.Source == nil || len(repos) == 0 || interval < 5*time.Second || interval > 24*time.Hour {
		return fail(fmt.Errorf("GitHub App connection, --repo and interval between 5s and 24h required"))
	}
	store, err := OpenStore(*state)
	if err != nil {
		return fail(err)
	}
	defer store.Close()
	fmt.Fprintln(stderr, "Ferretta service: intake-only; no model dispatch. State:", *state)
	runner := Runner{Source: c.Source, Store: store, Now: time.Now, Wait: Wait, Report: func(repo string, err error) {
		fmt.Fprintln(stderr, "Poll failed for", repo+":", err)
	}}
	if err := runner.Run(ctx, repos, interval, once); err != nil {
		return fail(err)
	}
	return 0
}
