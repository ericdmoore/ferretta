package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/ericdmoore/ferretta/internal/github"
	"github.com/ericdmoore/ferretta/internal/intent"
)

type fakeClient struct {
	comments []github.Comment
	err      error
	calls    int
}

func (c *fakeClient) Comments(_ context.Context, repo string, pr int) ([]github.Comment, error) {
	c.calls++
	if repo != "owner/repo" || pr != 7 {
		return nil, errors.New("unexpected target")
	}
	return c.comments, c.err
}

func TestParseCLI(t *testing.T) {
	var out, errs bytes.Buffer
	code := Run(context.Background(), []string{"intent", "parse"}, strings.NewReader("PROPOSED-Go-v1:: Pure Go"), &out, &errs, nil)
	var markers []intent.Marker
	if err := json.Unmarshal(out.Bytes(), &markers); err != nil {
		t.Fatal(err)
	}
	if code != 0 || len(markers) != 1 || markers[0].Body != "Pure Go" || errs.Len() != 0 {
		t.Fatalf("code %d, stdout %s, stderr %s", code, &out, &errs)
	}
}

func TestHelp(t *testing.T) {
	var out bytes.Buffer
	if code := Run(context.Background(), []string{"--help"}, nil, &out, io.Discard, nil); code != 0 || !strings.Contains(out.String(), "intent inspect") {
		t.Fatalf("help failed: %d, %s", code, &out)
	}
	if code := Run(context.Background(), []string{"-h"}, nil, brokenIO{}, io.Discard, nil); code != 1 {
		t.Fatal("lost help output error")
	}
}

func TestInspectCLI(t *testing.T) {
	args := []string{"intent", "inspect", "--repo", "owner/repo", "--pr", "7", "--humans", "human", "--agents", "agent"}
	var comments []github.Comment
	if err := json.Unmarshal([]byte(`[
		{"id":1,"body":"PROPOSED-Go-v1:: Pure Go","user":{"login":"agent","type":"Bot"}},
		{"id":2,"body":"CONFIRMED-Go-v1:: Agreed","user":{"login":"human","type":"User"}}
	]`), &comments); err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{comments: comments}
	var out, errs bytes.Buffer
	if code := Run(context.Background(), args, nil, &out, &errs, client); code != 0 {
		t.Fatalf("exit %d: %s", code, &errs)
	}
	var result struct {
		Repository string
		PR         int
		intent.Report
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Repository != "owner/repo" || result.PR != 7 || !result.Valid || result.Records[0].Status != intent.Confirmed {
		t.Fatalf("bad output: %s", &out)
	}
	client.comments[1].UpdatedAt = "edited"
	out.Reset()
	if code := Run(context.Background(), args, nil, &out, &errs, client); code != 1 || !strings.Contains(out.String(), `"valid": false`) {
		t.Fatal("edited comment not rejected")
	}
	client.err = errors.New("provider unavailable")
	if code := Run(context.Background(), args, nil, io.Discard, &errs, client); code != 1 {
		t.Fatal("fetch failure hidden")
	}
	if code := Run(context.Background(), args, nil, io.Discard, &errs, nil); code != 1 {
		t.Fatal("nil client accepted")
	}
	client.err = nil
	if code := Run(context.Background(), args, nil, brokenIO{}, &errs, client); code != 1 {
		t.Fatal("write failure hidden")
	}
}

type brokenIO struct{}

type fakeReview struct{ args []string }

func (r *fakeReview) Run(_ context.Context, args []string, _, _ io.Writer) int {
	r.args = args
	return 2
}
func TestReviewDispatch(t *testing.T) {
	r := &fakeReview{}
	if code := RunWithReviewer(context.Background(), []string{"review", "--pr", "1"}, nil, io.Discard, io.Discard, nil, r); code != 2 || len(r.args) != 2 {
		t.Fatal("review dispatch lost arguments or outcome")
	}
	if code := Run(context.Background(), []string{"review"}, nil, io.Discard, io.Discard, nil); code != 1 {
		t.Fatal("missing reviewer accepted")
	}
}

func (brokenIO) Read([]byte) (int, error)  { return 0, errors.New("read failed") }
func (brokenIO) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestInvalidCLI(t *testing.T) {
	for _, args := range [][]string{
		nil, {"intent"}, {"unknown", "command"}, {"intent", "unknown"}, {"intent", "parse", "extra"},
		{"intent", "inspect", "--unknown"}, {"intent", "inspect"},
		{"intent", "inspect", "--pr", "no"},
		{"intent", "inspect", "--repo", "owner/repo", "--pr", "7"},
		{"intent", "inspect", "--repo", "owner/repo", "--pr", "7", "extra"},
	} {
		client := &fakeClient{}
		if code := Run(context.Background(), args, nil, io.Discard, io.Discard, client); code == 0 || client.calls != 0 {
			t.Fatalf("invalid command executed: %v", args)
		}
	}
	for _, input := range []io.Reader{brokenIO{}, strings.NewReader("PROPOSED-Go:: bad"), strings.NewReader(strings.Repeat("x", (8<<20)+1))} {
		if code := Run(context.Background(), []string{"intent", "parse"}, input, io.Discard, io.Discard, nil); code != 1 {
			t.Fatal("bad input accepted")
		}
	}
	if code := Run(context.Background(), []string{"intent", "parse"}, strings.NewReader(""), brokenIO{}, io.Discard, nil); code != 1 {
		t.Fatal("write failure hidden")
	}
}
