//go:build network

package review

import (
	"context"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/ericdmoore/ferretta/internal/github"
)

// Metadata only: no generation, downloads, service startup, or GitHub writes.
func TestNetworkModelInventory(t *testing.T) {
	endpoint, model := os.Getenv("FERRETTA_TEST_MODEL_ENDPOINT"), os.Getenv("FERRETTA_TEST_MODEL")
	if err := validateOllamaEndpoint(endpoint); err != nil || model == "" {
		t.Fatal("set FERRETTA_TEST_MODEL_ENDPOINT to a loopback HTTP base URL and FERRETTA_TEST_MODEL to a listed model")
	}
	path := "/api/tags"
	switch os.Getenv("FERRETTA_TEST_MODEL_API") {
	case "ollama":
	case "compatible":
		path = "/v1/models"
	default:
		t.Fatal("set FERRETTA_TEST_MODEL_API to ollama or compatible")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	models, err := discover(ctx, NewCLI(&github.Connection{}).Setup.HTTP, probe{endpoint: endpoint, path: path})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(models, model) {
		t.Fatalf("expected model %q absent from inventory", model)
	}
}

// Explicit inference validation, deliberately separate from inventory discovery.
func TestNetworkModelTools(t *testing.T) {
	path := os.Getenv("FERRETTA_TEST_MODEL_POLICY")
	if path == "" {
		t.Fatal("set FERRETTA_TEST_MODEL_POLICY to a trusted local policy; this test runs inference")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	p, err := ParsePolicy(data)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), min(time.Duration(p.config.TimeoutSeconds)*time.Second, 2*time.Minute))
	defer cancel()
	replies, err := Probe(ctx, NewCLI(&github.Connection{}).Runner.Model, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(replies) != 2 || replies[0].Model == "" || replies[1].Model == "" {
		t.Fatal("missing model execution evidence")
	}
}
