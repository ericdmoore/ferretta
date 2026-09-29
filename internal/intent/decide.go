package intent

import "fmt"

type Role string

const (
	Human Role = "human"
	Agent Role = "agent"
)

// Evidence is supplied by an adapter using provider-reported identity. The
// content hash identifies the fetched comment snapshot, not historical proof.
type Evidence struct {
	CommentID int64  `json:"comment_id"`
	Author    string `json:"author"`
	URL       string `json:"url"`
	SHA256    string `json:"sha256"`
	Marker    Marker `json:"marker"`
}

type Record struct {
	Topic   string     `json:"topic"`
	Version int        `json:"version"`
	Status  Vocabulary `json:"status"`
	History []Evidence `json:"history"`
}

// SaveRecord is a command value. The caller chooses how to store or project it.
// Decide does not mutate its input record or perform effects.
type SaveRecord struct{ Record Record }

func Decide(current Record, role Role, evidence Evidence) (SaveRecord, error) {
	m := evidence.Marker
	if evidence.Author == "" || evidence.CommentID <= 0 || evidence.SHA256 == "" {
		return SaveRecord{}, fmt.Errorf("missing author or source evidence")
	}
	if m.Topic == "" || m.Version <= 0 {
		return SaveRecord{}, fmt.Errorf("invalid topic or version")
	}
	if current.Topic != "" && current.Topic != m.Topic {
		return SaveRecord{}, fmt.Errorf("topic mismatch")
	}
	if m.Vocabulary == Proposed {
		if role != Agent {
			return SaveRecord{}, fmt.Errorf("PROPOSED requires an authorized agent")
		}
	} else if m.Vocabulary == Corrected || m.Vocabulary == Confirmed {
		if role != Human {
			return SaveRecord{}, fmt.Errorf("%s requires an authorized human", m.Vocabulary)
		}
	} else {
		return SaveRecord{}, fmt.Errorf("unsupported vocabulary %q", m.Vocabulary)
	}
	if m.Vocabulary != Confirmed && m.Body == "" {
		return SaveRecord{}, fmt.Errorf("%s requires a body", m.Vocabulary)
	}
	if current.Status == Confirmed {
		return SaveRecord{}, fmt.Errorf("topic is already confirmed; replacement semantics are not defined")
	}
	switch m.Vocabulary {
	case Proposed:
		if current.Status != "" && current.Status != Corrected {
			return SaveRecord{}, fmt.Errorf("existing proposal awaits human response")
		}
		if m.Version != current.Version+1 {
			return SaveRecord{}, fmt.Errorf("expected proposal version %d", current.Version+1)
		}
	case Corrected, Confirmed:
		if current.Status != Proposed || m.Version != current.Version {
			return SaveRecord{}, fmt.Errorf("response must reference the pending proposal version")
		}
	}
	next := Record{Topic: m.Topic, Version: m.Version, Status: m.Vocabulary}
	next.History = append(append([]Evidence(nil), current.History...), evidence)
	return SaveRecord{Record: next}, nil
}
