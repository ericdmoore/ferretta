package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/ericdmoore/ferretta/internal/github"
)

type onboardingStub struct{ called string }

func (s *onboardingStub) Eval(context.Context, []string, io.Writer, io.Writer) int {
	s.called = "eval"
	return 0
}

func (s *onboardingStub) Watch(context.Context, []string, io.Writer, io.Writer) int {
	s.called = "watch"
	return 0
}

func TestWorkflowDispatch(t *testing.T) {
	s := &onboardingStub{}
	if RunWithReviewer(context.Background(), []string{"service", "watch"}, nil, io.Discard, io.Discard, nil, s) != 0 || s.called != "watch" {
		t.Fatal("watch not routed")
	}
	if Run(context.Background(), []string{"service", "watch"}, nil, io.Discard, io.Discard, nil) != 1 {
		t.Fatal("missing adapter")
	}
}

func (s *onboardingStub) Run(context.Context, []string, io.Writer, io.Writer) int {
	s.called = "review"
	return 0
}
func (s *onboardingStub) Init(context.Context, []string, io.Reader, io.Writer, io.Writer) int {
	s.called = "init"
	return 0
}
func (s *onboardingStub) Doctor(context.Context, []string, io.Writer, io.Writer) int {
	s.called = "doctor"
	return 0
}
func (s *onboardingStub) Demo(context.Context, []string, io.Writer, io.Writer) int {
	s.called = "demo"
	return 0
}
func (s *onboardingStub) Probe(context.Context, []string, io.Writer, io.Writer) int {
	s.called = "model"
	return 0
}

func TestOnboardingDispatch(t *testing.T) {
	for _, args := range [][]string{{"eval"}, {"init"}, {"doctor"}, {"demo"}, {"model", "test"}} {
		s := &onboardingStub{}
		if RunWithReviewer(context.Background(), args, nil, io.Discard, io.Discard, nil, s) != 0 || s.called != args[0] {
			t.Fatal(args, s.called)
		}
		if Run(context.Background(), args, nil, io.Discard, io.Discard, nil) != 1 {
			t.Fatal("missing adapters")
		}
	}
	for _, args := range [][]string{{"model"}, {"model", "other"}} {
		if RunWithReviewer(context.Background(), args, nil, io.Discard, io.Discard, nil, &onboardingStub{}) != 1 {
			t.Fatal("invalid model command")
		}
	}
}

type configureStub struct {
	fakeClient
	err error
}

func (c *configureStub) Configure(_ context.Context, repo string, cfg github.AppConfig) (github.Identity, error) {
	if repo != "o/r" || cfg.ClientID != "app" || cfg.InstallationID != 42 || cfg.PrivateKeyFile != "/private/app.pem" {
		return github.Identity{}, errors.New("wrong connection request")
	}
	return github.Identity{BotLogin: "ferretta[bot]", Repository: repo}, c.err
}

func TestGuidedAppSetup(t *testing.T) {
	ctx := context.Background()
	var out bytes.Buffer
	if Run(ctx, []string{"auth", "github", "--setup"}, nil, &out, io.Discard, nil) != 0 || !strings.Contains(out.String(), "--configure") || !strings.Contains(out.String(), "https://github.com/settings/apps/new") {
		t.Fatal(out.String())
	}
	if Run(ctx, []string{"auth", "github", "--setup"}, nil, brokenIO{}, io.Discard, nil) != 1 {
		t.Fatal("write error")
	}
	for _, args := range [][]string{{"auth", "github", "--setup", "--configure"}, {"auth", "github", "--setup", "extra"}, {"auth", "github", "--repo", "o/r", "--client-id", "app"}} {
		if Run(ctx, args, nil, io.Discard, io.Discard, nil) != 1 {
			t.Fatal("ambiguous arguments")
		}
	}
	args := []string{"auth", "github", "--configure", "--repo", "o/r", "--client-id", "app", "--installation-id", "42", "--private-key-file", "/private/app.pem"}
	if Run(ctx, args, nil, io.Discard, io.Discard, nil) != 1 {
		t.Fatal("missing configure adapter")
	}
	out.Reset()
	if Run(ctx, args, nil, &out, io.Discard, &configureStub{}) != 0 || !strings.Contains(out.String(), "ferretta[bot]") {
		t.Fatal(out.String())
	}
	if Run(ctx, args, nil, brokenIO{}, io.Discard, &configureStub{}) != 1 {
		t.Fatal("output error")
	}
	if Run(ctx, args, nil, io.Discard, io.Discard, &configureStub{err: errors.New("not installed")}) != 1 {
		t.Fatal("verification error")
	}
}
