package review

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/ericdmoore/ferretta/internal/github"
)

// Keep known credential and Git override variables out of model-requested
// checks. This is hygiene, not an OS sandbox: trusted checks run as the user.
func cleanEnvironment(env []string) []string {
	clean := make([]string, 0, len(env))
	for _, item := range env {
		name, _, _ := strings.Cut(item, "=")
		if strings.HasPrefix(name, "GH_") || strings.HasPrefix(name, "GITHUB_") || strings.HasPrefix(name, "FERRETTA_GITHUB_") || strings.HasPrefix(name, "GIT_") || name == "SSH_AUTH_SOCK" || name == "SSH_ASKPASS" {
			continue
		}
		clean = append(clean, item)
	}
	return clean
}

func fetchCommand(ctx context.Context, repo, sha, token string, env []string) (*exec.Cmd, error) {
	if !github.ValidRepository(repo) || !shaPattern.MatchString(sha) || strings.TrimSpace(token) == "" || strings.ContainsAny(token, "\r\n") {
		return nil, fmt.Errorf("valid repository, exact commit and app credential are required")
	}
	url := "https://github.com/" + repo + ".git"
	command := exec.CommandContext(ctx, "git", "fetch", "--no-tags", "--no-recurse-submodules", url, sha)
	// No credential in argv, URLs, files, config on disk, or diagnostic output.
	command.Env = append(cleanEnvironment(env), "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
	config := [][2]string{
		{"credential.helper", ""}, {"core.askPass", ""}, {"http.extraHeader", ""},
		{"http.followRedirects", "false"}, {"core.hooksPath", os.DevNull},
		{"http." + url + ".extraHeader", "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token))},
	}
	command.Env = append(command.Env, "GIT_CONFIG_COUNT="+strconv.Itoa(len(config)))
	for i, pair := range config {
		command.Env = append(command.Env, fmt.Sprintf("GIT_CONFIG_KEY_%d=%s", i, pair[0]), fmt.Sprintf("GIT_CONFIG_VALUE_%d=%s", i, pair[1]))
	}
	return command, nil
}

func (p Process) Fetch(ctx context.Context, repo, sha string) error {
	if p.Credentials == nil {
		return fmt.Errorf("GitHub App credentials are required for fetch")
	}
	token, err := p.Credentials(ctx, repo)
	if err != nil {
		return err
	}
	command, err := fetchCommand(ctx, repo, sha, token, os.Environ())
	if err != nil {
		return err
	}
	// Git/credential diagnostics can contain secrets. Do not persist or return them.
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("GitHub App Git fetch failed (check installation access and network)")
	}
	return nil
}
