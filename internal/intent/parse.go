// Package intent contains deterministic interpretation and confirmation rules.
package intent

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type Vocabulary string

const (
	Proposed  Vocabulary = "PROPOSED"
	Corrected Vocabulary = "CORRECTED"
	Confirmed Vocabulary = "CONFIRMED"
)

// Marker identifies exact proposal wording. Body remains ordinary Markdown.
type Marker struct {
	Vocabulary Vocabulary `json:"vocabulary"`
	Topic      string     `json:"topic"`
	Version    int        `json:"version"`
	Body       string     `json:"body"`
	Line       int        `json:"line"`
}

var markerPattern = regexp.MustCompile(`^([A-Z]+)-(.+)-v([1-9][0-9]*)::[ \t]*(.*)$`)

// Parse recognizes markers at the start of a line, outside Markdown fences.
// Quoted and indented examples are not instructions. A malformed protocol
// comment is rejected as a whole, rather than partially interpreted.
func Parse(text string) ([]Marker, error) {
	markers := []Marker{}
	var body []string
	var fence byte
	var fenceLength int
	flush := func() {
		if len(markers) > 0 {
			markers[len(markers)-1].Body = strings.TrimSpace(strings.Join(body, "\n"))
		}
		body = nil
	}
	for i, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimLeft(line, " ")
		indent := len(line) - len(trimmed)
		fenced := fence != 0
		if indent <= 3 && len(trimmed) >= 3 && (trimmed[0] == '`' || trimmed[0] == '~') {
			ch := trimmed[0]
			n := 0
			for n < len(trimmed) && trimmed[n] == ch {
				n++
			}
			if n >= 3 {
				if fence == 0 {
					fence, fenceLength = ch, n
					fenced = true
				} else if fence == ch && n >= fenceLength && strings.TrimSpace(trimmed[n:]) == "" {
					fence = 0
				}
			}
		}
		if !fenced {
			match := markerPattern.FindStringSubmatch(line)
			if match != nil {
				vocab := Vocabulary(match[1])
				if vocab != Proposed && vocab != Corrected && vocab != Confirmed {
					return nil, fmt.Errorf("line %d: unsupported vocabulary %q", i+1, vocab)
				}
				version, err := strconv.Atoi(match[3])
				if err != nil {
					return nil, fmt.Errorf("line %d: invalid version: %w", i+1, err)
				}
				topic := match[2]
				if strings.TrimSpace(topic) != topic || strings.Contains(topic, "::") {
					return nil, fmt.Errorf("line %d: invalid topic", i+1)
				}
				flush()
				markers = append(markers, Marker{Vocabulary: vocab, Topic: topic, Version: version, Line: i + 1})
				body = append(body, match[4])
				continue
			}
			for _, prefix := range []string{"PROPOSED-", "CORRECTED-", "CONFIRMED-", "AMENDED-"} {
				if strings.HasPrefix(line, prefix) {
					return nil, fmt.Errorf("line %d: expected VOCAB-Topic-vN::", i+1)
				}
			}
		}
		if len(markers) > 0 {
			body = append(body, line)
		}
	}
	flush()
	for _, marker := range markers {
		if marker.Vocabulary != Confirmed && marker.Body == "" {
			return nil, fmt.Errorf("line %d: %s requires a body", marker.Line, marker.Vocabulary)
		}
	}
	return markers, nil
}
