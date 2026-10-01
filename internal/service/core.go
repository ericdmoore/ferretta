// Package service owns notification intake policy. Observations are not review
// authorization: a scheduler must bind policy and revalidate inputs separately.
package service

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/ericdmoore/ferretta/internal/github"
)

// RecordSnapshot is a validated persistence command. Its zero value is invalid.
// A complete listing can replace current candidates; a failed/partial read cannot.
type RecordSnapshot struct {
	repository string
	at         time.Time
	revisions  []Revision
}

type Revision struct {
	ID         string    `json:"id"`
	Repository string    `json:"repository"`
	PR         int       `json:"pr"`
	Head       string    `json:"head"`
	Base       string    `json:"base"`
	FirstSeen  time.Time `json:"first_seen"`
}

var commitSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

func PlanSnapshot(repo string, pulls []github.PullRequest, now time.Time) (RecordSnapshot, error) {
	if !github.ValidRepository(repo) || now.IsZero() {
		return RecordSnapshot{}, fmt.Errorf("repository and observation time are required")
	}
	repo = strings.ToLower(repo)
	command := RecordSnapshot{repository: repo, at: now.UTC()}
	seen := make(map[int]bool)
	for _, pr := range pulls {
		if pr.Number < 1 || seen[pr.Number] || !commitSHA.MatchString(pr.Head) || !commitSHA.MatchString(pr.Base) || pr.State != "OPEN" {
			return RecordSnapshot{}, fmt.Errorf("inconsistent open PR listing; retry a fresh listing")
		}
		seen[pr.Number] = true
		if pr.Draft {
			continue
		}
		identity, _ := json.Marshal([]any{"pr-observation-v1", repo, pr.Number, pr.Head, pr.Base})
		command.revisions = append(command.revisions, Revision{fmt.Sprintf("%x", sha256.Sum256(identity)), repo, pr.Number, pr.Head, pr.Base, command.at})
	}
	return command, nil
}
