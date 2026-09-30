package review

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ericdmoore/ferretta/internal/github"
)

// Setup performs metadata discovery and writes a policy only after validation
// and selection. It never invokes inference, checks, or GitHub mutations.
type Setup struct {
	HTTP Doer
	Exec Commander
	Auth func(context.Context, string) (github.Identity, error)
}

func (s Setup) Run(ctx context.Context, args []string, input io.Reader, output, stderr io.Writer) int {
	fail := func(err error) int { fmt.Fprintln(stderr, err); return 1 }
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(stderr)
	endpoint := flags.String("endpoint", "http://127.0.0.1:11434", "local Ollama HTTP endpoint")
	model := flags.String("model", "", "installed Ollama model; otherwise choose interactively")
	check := flags.String("check", "", `required check as a JSON argv array, e.g. '["make","check"]'`)
	effort := flags.String("effort", "", "explicit named reasoning effort: low, medium, high")
	thinking := flags.String("thinking", "auto", "auto, enabled, low, medium, high")
	contextTokens := flags.Int("context", 16384, "context token limit; memory use grows with context")
	maxTurns := flags.Int("max-turns", 100, "model-turn ceiling; 0 disables it")
	outputTokens := flags.Int("max-output-tokens", 4096, "maximum generated tokens per reply, including thinking")
	timeout := flags.Int("timeout", 600, "active attempt seconds; 0 disables the deadline")
	repo := flags.String("repo", "", "repository for GitHub App access verification")
	path := flags.String("policy", ".ferretta/review.json", "new policy path; existing files are never overwritten")
	yes := flags.Bool("yes", false, "save without prompting; requires --model and --check")
	onlyDiscover := flags.Bool("discover", false, "report local endpoints and GitHub readiness without writing")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if (*repo != "" && !github.ValidRepository(*repo)) || (*effort != "" && *thinking != "auto") || flags.NArg() != 0 || (*yes && !*onlyDiscover && (*model == "" || *check == "")) {
		return fail(fmt.Errorf("no positional arguments; --yes requires --model and --check"))
	}
	// Validate the endpoint before making any network request.
	if err := validateOllamaEndpoint(*endpoint); err != nil {
		return fail(err)
	}
	if !*onlyDiscover {
		if _, err := os.Lstat(*path); !errors.Is(err, os.ErrNotExist) {
			return fail(fmt.Errorf("cannot create %s: path exists or cannot be inspected; use --discover or another --policy path", *path))
		}
	}
	var models []string
	var inventory bytes.Buffer
	for i, p := range discoveryPlan(*endpoint) {
		probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		names, err := discover(probeCtx, s.HTTP, p)
		cancel()
		if ctx.Err() != nil {
			return fail(ctx.Err())
		}
		if err != nil {
			fmt.Fprintf(&inventory, "%s at %s: %s\n", p.hint, p.endpoint, err)
			continue
		}
		fmt.Fprintf(&inventory, "%s at %s: %d model(s)\n", p.hint, p.endpoint, len(names))
		for _, name := range names {
			fmt.Fprintf(&inventory, "  %q\n", name)
		}
		if i == 0 {
			models = names
		} else {
			fmt.Fprintln(&inventory, "  Inventory only: inference location/capabilities unknown; review adapter not implemented.")
		}
	}
	if *repo == "" || s.Auth == nil {
		fmt.Fprintln(&inventory, "GitHub: not verified. Run ferretta auth github --setup; then ferretta doctor --repo owner/repo.")
	} else {
		authCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		identity, err := s.Auth(authCtx, *repo)
		cancel()
		if ctx.Err() != nil {
			return fail(ctx.Err())
		}
		if err != nil {
			fmt.Fprintln(&inventory, "GitHub: App connection unavailable. Run ferretta auth github --setup.")
		} else {
			fmt.Fprintf(&inventory, "GitHub: verified %s for %s.\n", identity.BotLogin, *repo)
		}
	}
	fmt.Fprintln(&inventory, "OpenRouter: integration not implemented yet. No credentials are collected by init.")
	if _, err := output.Write(inventory.Bytes()); err != nil {
		return fail(err)
	}
	if *onlyDiscover {
		return 0
	}
	if len(models) == 0 {
		return fail(fmt.Errorf("no Ollama models found; install/start Ollama (https://ollama.com/download), then explicitly pull a tools/thinking model, e.g. ollama pull qwen3:4b-thinking; or supply --endpoint"))
	}
	reader := bufio.NewScanner(input)
	prompt := func(label string) (string, error) {
		if _, err := fmt.Fprint(output, label); err != nil {
			return "", err
		}
		if !reader.Scan() {
			if err := reader.Err(); err != nil {
				return "", err
			}
			return "", fmt.Errorf("input ended; no policy written (use --yes --model NAME --check JSON for unattended setup)")
		}
		return strings.TrimSpace(reader.Text()), nil
	}
	if *model == "" {
		for i, name := range models {
			fmt.Fprintf(output, "%d) %q\n", i+1, name)
		}
		answer, err := prompt("Choose an Ollama model number: ")
		if err != nil {
			return fail(err)
		}
		i, err := strconv.Atoi(answer)
		if err != nil || i < 1 || i > len(models) {
			return fail(fmt.Errorf("choose a listed model number"))
		}
		*model = models[i-1]
	}
	if !slices.Contains(models, *model) {
		return fail(fmt.Errorf("model %q is not listed at the selected Ollama endpoint", *model))
	}
	if *check == "" {
		suggestion := suggestCheck()
		label := "Required check as a JSON argv array: "
		if suggestion != "" {
			label = "Required check as a JSON argv array " + suggestion + " (Enter accepts): "
		}
		answer, err := prompt(label)
		if err != nil {
			return fail(err)
		}
		*check = answer
		if *check == "" {
			*check = suggestion
		}
	}
	var command []string
	if err := json.Unmarshal([]byte(*check), &command); err != nil {
		return fail(fmt.Errorf("--check must be a JSON argv array: %w", err))
	}
	metadataCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	info, err := (Ollama{HTTP: s.HTTP}).Info(metadataCtx, *endpoint, *model)
	cancel()
	if err != nil {
		return fail(err)
	}
	requested := *thinking
	if *effort != "" {
		requested = *effort
	}
	if requested == "auto" {
		requested = ""
		for _, candidate := range []string{"medium", "enabled", "low", "high"} {
			if slices.Contains(info.thinkingValues(), candidate) {
				requested = candidate
				break
			}
		}
	}
	cfg := config{Provider: "ollama", Endpoint: *endpoint, Model: *model, ContextTokens: *contextTokens,
		MaxTurns: *maxTurns, MaxTokens: *outputTokens, TimeoutSeconds: *timeout, Checks: [][]string{command}}
	if requested == "enabled" {
		cfg.Thinking = requested
	} else {
		cfg.Effort = requested
	}
	p, err := setupPolicy(cfg)
	if err != nil {
		return fail(err)
	}
	if err := info.validate(p); err != nil {
		return fail(err)
	}
	data, _ := json.MarshalIndent(p.config, "", "  ") // config contains only JSON-native values.
	data = append(data, '\n')
	if _, err := fmt.Fprintf(output, "Policy for %s:\n%s", *path, data); err != nil {
		return fail(err)
	}
	if !*yes {
		answer, err := prompt("Create this policy? [y/N]: ")
		if err != nil {
			return fail(err)
		}
		if answer != "y" && answer != "Y" && !strings.EqualFold(answer, "yes") {
			fmt.Fprintln(output, "No policy written.")
			return 0
		}
	}
	if ctx.Err() != nil {
		return fail(ctx.Err())
	}
	if err := createPolicyFile(*path, data); err != nil {
		return fail(err)
	}
	fmt.Fprintf(output, "Created %s. Review its limits and checks, then run ferretta review --repo owner/repo --pr N --policy %q.\n", *path, *path)
	return 0
}

// Suggestions inspect trusted local files only; setup never executes checks.
func suggestCheck() string {
	data, _ := os.ReadFile("Makefile")
	if regexp.MustCompile(`(?m)^check\s*:`).Match(data) {
		return `["make","check"]`
	}
	if info, err := os.Stat("go.mod"); err == nil && info.Mode().IsRegular() {
		return `["go","test","./..."]`
	}
	return ""
}

func setupPolicy(c config) (Policy, error) {
	data, _ := json.Marshal(c)
	return ParsePolicy(data)
}

func createPolicyFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		// Preserve evidence of an uncertain/partial write; never overwrite on retry.
		return fmt.Errorf("policy write incomplete; inspect %s before retrying: %w", path, err)
	}
	return nil
}
