package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplicationWiringWithoutCredentials(t *testing.T) {
	t.Setenv("FERRETTA_GITHUB_CONFIG", filepath.Join(t.TempDir(), "not-provisioned.json"))
	var output, stderr bytes.Buffer
	if code := run([]string{"--help"}, nil, &output, &stderr); code != 0 || !strings.Contains(output.String(), "service run") {
		t.Fatal(code, output.String(), stderr.String())
	}
	output.Reset()
	if code := run([]string{"intent", "parse"}, strings.NewReader("PROPOSED-Go-v1:: Keep it pure Go."), &output, &stderr); code != 0 || !strings.Contains(output.String(), "Keep it pure Go") {
		t.Fatal(code, output.String(), stderr.String())
	}
	output.Reset()
	if code := run([]string{"service", "run", "--repo", "o/r", "--state", filepath.Join(t.TempDir(), "state"), "--once"}, nil, &output, &stderr); code != 1 || !strings.Contains(stderr.String(), "GitHub App configuration") {
		t.Fatal("service must use the App connection", code, stderr.String())
	}
}
