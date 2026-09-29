package intent

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
)

type Comment struct {
	ID                            int64
	Author, AuthorType, URL, Body string
	Edited                        bool
}

type Policy struct {
	Humans []string
	Agents []string
}

type Finding struct {
	CommentID int64  `json:"comment_id"`
	URL       string `json:"url"`
	Message   string `json:"message"`
}

type Report struct {
	// This is a projection of current comments, not a durable confirmation log.
	Kind     string    `json:"kind"`
	Valid    bool      `json:"valid"`
	Records  []Record  `json:"records"`
	Findings []Finding `json:"findings"`
}

func (p Policy) Validate() error {
	if len(p.Humans) == 0 || len(p.Agents) == 0 {
		return fmt.Errorf("configure at least one human and one agent")
	}
	seen := map[string]bool{}
	for _, login := range append(append([]string(nil), p.Humans...), p.Agents...) {
		key := strings.ToLower(strings.TrimSpace(login))
		if key == "" || key != strings.ToLower(login) {
			return fmt.Errorf("invalid actor login %q", login)
		}
		if seen[key] {
			return fmt.Errorf("actor %q is configured more than once", login)
		}
		seen[key] = true
	}
	return nil
}

func (p Policy) role(comment Comment) Role {
	if comment.AuthorType == "User" {
		for _, login := range p.Humans {
			if strings.EqualFold(login, comment.Author) {
				return Human
			}
		}
	}
	for _, login := range p.Agents {
		if strings.EqualFold(login, comment.Author) {
			return Agent
		}
	}
	return ""
}

// Inspect applies commands to an in-memory projection. It is deterministic and
// performs no provider calls. Any finding makes the entire report invalid.
func Inspect(comments []Comment, policy Policy) (Report, error) {
	if err := policy.Validate(); err != nil {
		return Report{}, err
	}
	report := Report{Kind: "current-comment-snapshot", Valid: true, Records: []Record{}, Findings: []Finding{}}
	states := map[string]Record{}
	ordered := append([]Comment(nil), comments...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	seen := map[int64]bool{}
	for _, comment := range ordered {
		finding := func(message string) {
			report.Findings = append(report.Findings, Finding{comment.ID, comment.URL, message})
			report.Valid = false
		}
		markers, err := Parse(comment.Body)
		if err != nil {
			finding(err.Error())
			continue
		}
		if len(markers) == 0 {
			continue
		}
		if seen[comment.ID] {
			finding("duplicate comment ID; fetch a fresh snapshot")
			continue
		}
		seen[comment.ID] = true
		if comment.Edited {
			finding("edited protocol comment cannot establish historical wording; use a new comment")
			continue
		}
		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(comment.Body)))
		for _, marker := range markers {
			evidence := Evidence{CommentID: comment.ID, Author: comment.Author, URL: comment.URL, SHA256: hash, Marker: marker}
			command, err := Decide(states[marker.Topic], policy.role(comment), evidence)
			if err != nil {
				finding(fmt.Sprintf("line %d: %s", marker.Line, err))
				continue
			}
			states[marker.Topic] = command.Record
		}
	}
	for _, record := range states {
		report.Records = append(report.Records, record)
	}
	sort.Slice(report.Records, func(i, j int) bool { return report.Records[i].Topic < report.Records[j].Topic })
	return report, nil
}
