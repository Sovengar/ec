// Package handoff owns the versioned JSON contract between ec and an AI agent.
//
// It is stdlib-only on purpose: the CLI surfaces (--emit/--apply-json) and the
// harness adapter share one definition of the wire shape, so a transport change
// can never drift from the protocol.
package handoff

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ProtocolVersion is the wire version of the AI handoff.
const ProtocolVersion = "1"

// Request is what ec hands to an agent.
type Request struct {
	Version string `json:"version"`
	Files   []File `json:"files"`
}

// File carries one conflicted file.
type File struct {
	Path      string            `json:"path"`
	Conflicts []HandoffConflict `json:"conflicts"`
}

// Conflict is the wire form of one conflict region.
type Conflict struct {
	ID      int    `json:"id"`
	Start   int    `json:"start"` // 1-based line of the <<<<<<< marker
	End     int    `json:"end"`   // 1-based line of the >>>>>>> marker
	Ours    string `json:"ours"`
	Base    string `json:"base,omitempty"`
	Theirs  string `json:"theirs"`
	HasBase bool   `json:"has_base,omitempty"`
}

// HandoffConflict is a conflict plus local context.
type HandoffConflict struct {
	Conflict
	Before []string `json:"before,omitempty"`
	After  []string `json:"after,omitempty"`
}

// MarshalJSON flattens the embedded conflict, keeping the wire shape flat.
func (h HandoffConflict) MarshalJSON() ([]byte, error) {
	type alias Conflict
	return json.Marshal(struct {
		alias
		Before []string `json:"before,omitempty"`
		After  []string `json:"after,omitempty"`
	}{alias(h.Conflict), h.Before, h.After})
}

// Response is what the agent returns.
type Response struct {
	Version     string       `json:"version"`
	Resolutions []Resolution `json:"resolutions"`
}

// Resolution is one resolved conflict block.
type Resolution struct {
	Path       string  `json:"path"`
	ID         int     `json:"id"`
	Text       string  `json:"text"`
	Confidence float64 `json:"confidence,omitempty"`
	Note       string  `json:"note,omitempty"`
}

// EncodeRequest renders the handoff payload as indented JSON.
func EncodeRequest(req Request) ([]byte, error) {
	return json.MarshalIndent(req, "", "  ")
}

// DecodeRequest reads a handoff payload back, used by tests and tooling.
func DecodeRequest(data []byte) (Request, error) {
	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		return req, fmt.Errorf("decode request: %w", err)
	}
	return req, nil
}

// ParseResponse decodes and validates an agent reply.
func ParseResponse(data []byte) (Response, error) {
	var resp Response
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&resp); err != nil {
		return resp, fmt.Errorf("decode agent response: %w", err)
	}
	if resp.Version != "" && resp.Version != ProtocolVersion {
		return resp, fmt.Errorf("unsupported protocol version %q", resp.Version)
	}
	if len(resp.Resolutions) == 0 {
		return resp, errors.New("agent returned no resolutions")
	}
	return resp, nil
}

// markerPrefixes are the git conflict markers that must never reach a file.
var markerPrefixes = []string{"<<<<<<<", "|||||||", "=======", ">>>>>>>"}

// HasMarker reports whether text carries a conflict marker line. A resolution
// containing one would re-introduce a conflict, so it is refused.
func HasMarker(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r\n")
		for _, marker := range markerPrefixes {
			if !strings.HasPrefix(line, marker) {
				continue
			}
			rest := line[len(marker):]
			if rest == "" || strings.HasPrefix(rest, " ") {
				return true
			}
		}
	}
	return false
}
