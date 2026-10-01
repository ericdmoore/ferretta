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

type fakeAuth struct {
	fakeClient
	err error
}

func (f *fakeAuth) Status(context.Context, string) (github.Identity, error) {
	return github.Identity{AppID: 7, InstallationID: 42, BotLogin: "ferretta[bot]", Repository: "o/r"}, f.err
}
func TestAuthCLI(t *testing.T) {
	ctx := context.Background()
	client := &fakeAuth{}
	args := []string{"auth", "github", "--repo", "o/r"}
	var output bytes.Buffer
	if code := Run(ctx, args, nil, &output, io.Discard, client); code != 0 || !strings.Contains(output.String(), "ferretta[bot]") {
		t.Fatal(code, output.String())
	}
	for _, bad := range [][]string{{"auth"}, {"auth", "other"}, {"auth", "github"}, {"auth", "github", "--bad"}, {"auth", "github", "--repo", "o/r", "extra"}} {
		if Run(ctx, bad, nil, io.Discard, io.Discard, client) != 1 {
			t.Fatal("bad auth command accepted")
		}
	}
	if Run(ctx, args, nil, brokenIO{}, io.Discard, client) != 1 {
		t.Fatal("output error lost")
	}
	if Run(ctx, args, nil, io.Discard, io.Discard, &fakeClient{}) != 1 {
		t.Fatal("no auth adapter")
	}
	client.err = errors.New("not installed")
	if Run(ctx, args, nil, io.Discard, io.Discard, client) != 1 {
		t.Fatal("auth failure lost")
	}
}

func TestServiceDispatch(t *testing.T) {
	var output bytes.Buffer
	if Run(context.Background(), []string{"service"}, nil, io.Discard, &output, nil) != 1 || !strings.Contains(output.String(), "service run") {
		t.Fatal("service command not routed", output.String())
	}
}
