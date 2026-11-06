package handoff

import (
	"errors"
	"fmt"
	"strings"
)

// Preamble is the fixed instruction that precedes the payload. It tells the
// agent what to answer and, just as importantly, what not to do: the resolver
// owns the file, so an agent that edits it would race the user's own writes.
const Preamble = "You are resolving git merge conflicts for the `ec` tool.\n" +
	"Resolve every conflict in the JSON payload below. Answer with exactly one JSON object and nothing else:\n" +
	`{"version":"1","resolutions":[{"path":"<path>","id":<conflict id>,"text":"<resolved text>"}]}` + "\n" +
	"`text` is the full replacement for the conflict block, without conflict markers.\n" +
	"Do not edit any file; the tool applies your answer itself.\n\n"

// Prompt composes the headless prompt: the fixed preamble plus the exact
// encoded handoff payload. The agent sees the same bytes `ec --emit` prints.
func Prompt(req Request) (string, error) {
	payload, err := EncodeRequest(req)
	if err != nil {
		return "", err
	}
	return Preamble + string(payload), nil
}

// ErrNoReply is returned when no JSON object can be recovered from a reply.
var ErrNoReply = errors.New("agent reply has no JSON object")

// ExtractResponse recovers the JSON object from a harness reply.
//
// Coding harnesses wrap their answer in prose or a fenced code block, so the
// reply is scanned leniently. Validation stays strict: the caller still runs
// ParseResponse and the marker guard on what comes out of here.
func ExtractResponse(raw []byte) ([]byte, error) {
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return nil, ErrNoReply
	}
	if isJSONObject([]byte(text)) {
		return []byte(text), nil
	}
	if fenced := fencedJSON(text); fenced != nil {
		return fenced, nil
	}
	if obj := outerObject(text); obj != nil && isJSONObject(obj) {
		return obj, nil
	}
	return nil, ErrNoReply
}

func isJSONObject(data []byte) bool {
	trimmed := strings.TrimSpace(string(data))
	if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
		return false
	}
	// The caller's ParseResponse does the real decode; this only weeds out the
	// prose that starts and ends with braces.
	_, err := ParseResponse([]byte(trimmed))
	return err == nil
}

// fencedJSON returns the body of the first ``` / ```json fenced block.
func fencedJSON(text string) []byte {
	for offset := 0; ; {
		open := strings.Index(text[offset:], "```")
		if open < 0 {
			return nil
		}
		start := offset + open + len("```")
		// Skip the optional language tag up to the end of the line.
		if nl := strings.IndexByte(text[start:], '\n'); nl >= 0 {
			start += nl + 1
		} else {
			return nil
		}
		end := strings.Index(text[start:], "```")
		if end < 0 {
			return nil
		}
		body := strings.TrimSpace(text[start : start+end])
		if isJSONObject([]byte(body)) {
			return []byte(body)
		}
		offset = start + end + len("```")
	}
}

// outerObject returns the widest brace-balanced region of the text.
func outerObject(text string) []byte {
	start := strings.IndexByte(text, '{')
	if start < 0 {
		return nil
	}
	end := strings.LastIndexByte(text, '}')
	if end <= start {
		return nil
	}
	return []byte(strings.TrimSpace(text[start : end+1]))
}

// ResolutionFor picks the resolution for one conflict id, refusing markers.
func ResolutionFor(resp Response, id int) (string, error) {
	for _, r := range resp.Resolutions {
		if r.ID != id {
			continue
		}
		if HasMarker(r.Text) {
			return "", fmt.Errorf("resolution for conflict %d contains conflict markers", id)
		}
		return r.Text, nil
	}
	return "", fmt.Errorf("agent gave no resolution for conflict %d", id)
}
