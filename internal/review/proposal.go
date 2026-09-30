package review

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/ericdmoore/ferretta/internal/github"
	"github.com/ericdmoore/ferretta/internal/intent"
)

type ProposalRequest struct {
	Topic          string   `json:"topic"`
	Question       string   `json:"question"`
	Options        []string `json:"options,omitempty"`
	Recommendation string   `json:"recommendation,omitempty"`
	Reason         string   `json:"reason"`
}
type RequestIntent struct{ request ProposalRequest }

func (RequestIntent) command() {}

func (r ProposalRequest) validate() error {
	if !regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]{0,63}$`).MatchString(r.Topic) || strings.TrimSpace(r.Question) == "" || strings.TrimSpace(r.Reason) == "" || len(r.Options) > 4 {
		return fmt.Errorf("request_intent_confirmation requires topic (letters/digits/hyphens), question, reason and at most four options")
	}
	data, _ := json.Marshal(r)
	if len(data) > 12000 {
		return fmt.Errorf("proposal exceeds 12000 bytes")
	}
	return nil
}

// Delivery only returns to draft when the adapter establishes no dispatch.
// Uncertain means a dispatch may have
// happened; absence from a later snapshot is NOT permission to post again.
type Delivery string

const (
	Draft     Delivery = "draft"
	Uncertain Delivery = "uncertain"
	Posted    Delivery = "posted"
)

type Proposal struct {
	Request  ProposalRequest  `json:"request"`
	Topic    string           `json:"topic"`
	Version  int              `json:"version"`
	EffectID string           `json:"effect_id"`
	Body     string           `json:"body"`
	Delivery Delivery         `json:"delivery"`
	Comment  github.Comment   `json:"comment"`
	Decision *intent.Evidence `json:"decision,omitempty"`
}

func newProposal(report Report, request ProposalRequest) (Proposal, error) {
	if err := request.validate(); err != nil {
		return Proposal{}, err
	}
	data, _ := json.Marshal(request)
	topic, version := request.Topic, 1
	seen := map[string]bool{}
	for i := len(report.Proposals) - 1; i >= 0; i-- {
		p := report.Proposals[i]
		if seen[p.Request.Topic] {
			continue
		}
		seen[p.Request.Topic] = true
		if p.Decision == nil {
			return Proposal{}, fmt.Errorf("pending proposal must be resolved first")
		}
		if p.Decision.Marker.Vocabulary == intent.Corrected {
			if request.Topic != p.Request.Topic {
				return Proposal{}, fmt.Errorf("revise corrected topic %s before continuing", p.Request.Topic)
			}
			topic, version = p.Topic, p.Version+1
		} else if request.Topic == p.Request.Topic {
			return Proposal{}, fmt.Errorf("topic already confirmed; amendment semantics are deferred")
		}
	}
	if version == 1 {
		hash := sha256.Sum256([]byte(report.PR.URL + report.PR.Head + report.PR.Base + report.PolicySHA256 + string(data)))
		topic = fmt.Sprintf("%s-%x", request.Topic, hash[:8])
	}
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s/%s/%s/%s/%s/%d/%s", report.PR.URL, report.PR.Head, report.PR.Base, report.PolicySHA256, topic, version, data)))
	p := Proposal{Request: request, Topic: topic, Version: version, EffectID: fmt.Sprintf("%x", hash), Delivery: Draft}
	// Model prose is indented so embedded protocol-looking text cannot mint a
	// second marker. Only the harness emits the actual protocol header.
	quote := func(s string) string { return "    " + strings.ReplaceAll(s, "\n", "\n    ") }
	p.Body = fmt.Sprintf("<!-- ferretta-effect:%s -->\nPROPOSED-%s-v%d::\n%s\n\nReason:\n%s\n", p.EffectID, topic, version, quote(request.Question), quote(request.Reason))
	for i, o := range request.Options {
		p.Body += fmt.Sprintf("\n%c.\n%s\n", 'A'+i, quote(o))
	}
	if request.Recommendation != "" {
		p.Body += "\nRecommendation:\n" + quote(request.Recommendation) + "\n"
	}
	actual := "unknown"
	if len(report.Attempts) > 0 {
		actual = report.Attempts[len(report.Attempts)-1].Model
	}
	p.Body += fmt.Sprintf("\nCommit: `%s`\nPolicy: `%s`\nProvider: %q; requested model: %q; requested effort: %q; requested thinking: %q; reported model: %q; reported effort: unknown; attempt: %d.\n\nReply with `CONFIRMED-%s-v%d::` and your selection, or `CORRECTED-%s-v%d::` and the correction.\n", report.PR.Head, report.PolicySHA256, report.Provider, report.RequestedModel, report.RequestedEffort, report.RequestedThinking, actual, len(report.Attempts), topic, version, topic, version)
	return p, nil
}

type ProposalGitHub interface {
	Comments(context.Context, string, int) ([]github.Comment, error)
	CreateComment(context.Context, string, int, string) (github.Comment, error)
	Status(context.Context, string) (github.Identity, error)
}

// ReconcileProposal is pure. A matching marker from another actor, changed
// wording, or multiple matches cannot count as successful delivery.
func ReconcileProposal(p Proposal, comments []github.Comment, bot string) (Proposal, error) {
	var matches []github.Comment
	for _, c := range comments {
		if strings.Contains(c.Body, "<!-- ferretta-effect:"+p.EffectID+" -->") && c.User.Type == "Bot" && c.User.Login == bot {
			matches = append(matches, c)
		}
	}
	if len(matches) > 1 {
		return p, fmt.Errorf("multiple comments match proposal effect; reconcile manually")
	}
	if len(matches) == 1 {
		c := matches[0]
		if c.ID <= 0 || c.Body != p.Body || c.CreatedAt == "" || c.UpdatedAt != c.CreatedAt {
			return p, fmt.Errorf("proposal comment changed or has incomplete identity")
		}
		p.Delivery, p.Comment = Posted, c
	} else if p.Delivery == Posted {
		return p, fmt.Errorf("posted proposal is missing; cannot accept replies")
	}
	return p, nil
}

func proposalDecision(p Proposal, comments []github.Comment, humans []string) (*intent.Evidence, error) {
	if p.Delivery != Posted {
		return nil, nil
	}
	ordered := append([]github.Comment(nil), comments...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	var invalid error
	for _, c := range ordered {
		allowed := false
		for _, h := range humans {
			if strings.EqualFold(c.User.Login, h) {
				allowed = true
			}
		}
		if !allowed || c.User.Type != "User" || c.ID <= p.Comment.ID {
			continue
		}
		markers, err := intent.Parse(c.Body)
		if err != nil {
			continue
		}
		bad := false
		var selected *intent.Marker
		for _, m := range markers {
			if m.Topic != p.Topic || m.Version != p.Version || (m.Vocabulary != intent.Confirmed && m.Vocabulary != intent.Corrected) {
				continue
			}
			if c.CreatedAt == "" || c.CreatedAt != c.UpdatedAt || selected != nil {
				invalid = fmt.Errorf("edited or conflicting human decision; post one unedited decision")
				bad = true
				break
			}
			copy := m
			selected = &copy
		}
		if bad {
			continue
		}
		if selected != nil {
			if selected.Vocabulary == intent.Confirmed && len(p.Request.Options) > 0 && strings.TrimSpace(selected.Body) == "" {
				invalid = fmt.Errorf("confirmation must include the selected option")
				continue
			}
			hash := sha256.Sum256([]byte(c.Body))
			return &intent.Evidence{CommentID: c.ID, Author: c.User.Login, URL: c.URL, SHA256: fmt.Sprintf("%x", hash), Marker: *selected}, nil
		}
	}
	return nil, invalid
}
