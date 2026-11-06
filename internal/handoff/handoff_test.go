package handoff

import (
	"encoding/json"
	"strings"
	"testing"
)

func sampleRequest() Request {
	return Request{Version: ProtocolVersion, Files: []File{{
		Path: "f.txt",
		Conflicts: []HandoffConflict{{
			Conflict: Conflict{ID: 1, Start: 2, End: 6, Ours: "ours\n", Base: "base\n", Theirs: "theirs\n", HasBase: true},
			Before:   []string{"start\n"},
			After:    []string{"end\n"},
		}},
	}}}
}

func TestPromptCarriesTheExactHandoffPayload(t *testing.T) {
	prompt, err := Prompt(sampleRequest())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(prompt, Preamble) {
		t.Fatalf("prompt does not start with the preamble:\n%s", prompt)
	}
	payload, err := EncodeRequest(sampleRequest())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, string(payload)) {
		t.Fatalf("prompt does not embed the encoded payload:\n%s", prompt)
	}
	if !strings.Contains(prompt, `"version": "1"`) {
		t.Fatalf("payload lost the protocol version:\n%s", prompt)
	}
}

func TestPromptKeepsTheNoEditInstruction(t *testing.T) {
	prompt, err := Prompt(sampleRequest())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "Do not edit any file") {
		t.Fatalf("prompt must tell the harness not to edit files:\n%s", prompt)
	}
}

func TestHandoffConflictMarshalsFlat(t *testing.T) {
	data, err := json.Marshal(HandoffConflict{
		Conflict: Conflict{ID: 3, Ours: "o\n", Theirs: "t\n"},
		Before:   []string{"b\n"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"id", "ours", "theirs", "before"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("flat conflict lost %q: %s", key, data)
		}
	}
	if _, nested := got["Conflict"]; nested {
		t.Fatalf("embedded struct must not nest: %s", data)
	}
}

func TestExtractResponsePlainJSON(t *testing.T) {
	body, err := ExtractResponse([]byte(`{"version":"1","resolutions":[{"path":"f","id":1,"text":"x"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseResponse(body); err != nil {
		t.Fatalf("plain reply rejected: %v", err)
	}
}

func TestExtractResponseFencedCodeBlock(t *testing.T) {
	reply := "Sure, here it is:\n\n```json\n{\"version\":\"1\",\"resolutions\":[{\"path\":\"f\",\"id\":1,\"text\":\"x\"}]}\n```\n\nHope that helps."
	body, err := ExtractResponse([]byte(reply))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseResponse(body); err != nil {
		t.Fatalf("fenced reply rejected: %v", err)
	}
}

func TestExtractResponseSurroundingProse(t *testing.T) {
	reply := "I resolved it as follows {\"version\":\"1\",\"resolutions\":[{\"path\":\"f\",\"id\":1,\"text\":\"x\"}]} done."
	body, err := ExtractResponse([]byte(reply))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseResponse(body); err != nil {
		t.Fatalf("prose-wrapped reply rejected: %v", err)
	}
}

func TestExtractResponseRejectsTextWithoutJSON(t *testing.T) {
	if _, err := ExtractResponse([]byte("I could not resolve this conflict.")); err == nil {
		t.Fatal("prose without a JSON object must be refused")
	}
}

func TestParseResponseRejectsUnsupportedVersion(t *testing.T) {
	_, err := ParseResponse([]byte(`{"version":"2","resolutions":[{"path":"f","id":1,"text":"x"}]}`))
	if err == nil {
		t.Fatal("want version rejection")
	}
}

func TestResolutionForRejectsConflictMarkers(t *testing.T) {
	resp := Response{Version: ProtocolVersion, Resolutions: []Resolution{
		{Path: "f", ID: 1, Text: "<<<<<<< HEAD\noops\n=======\nx\n>>>>>>> y\n"},
	}}
	if _, err := ResolutionFor(resp, 1); err == nil {
		t.Fatal("marker-carrying resolution must be refused")
	}
}

func TestResolutionForRejectsMissingConflict(t *testing.T) {
	resp := Response{Version: ProtocolVersion, Resolutions: []Resolution{
		{Path: "f", ID: 2, Text: "x\n"},
	}}
	if _, err := ResolutionFor(resp, 1); err == nil {
		t.Fatal("a missing conflict id must be refused")
	}
}

func TestHasMarker(t *testing.T) {
	for _, text := range []string{"<<<<<<< HEAD\n", "||||||| base\n", "=======\n", ">>>>>>> other\n"} {
		if !HasMarker(text) {
			t.Fatalf("marker %q not detected", text)
		}
	}
	for _, text := range []string{"normal text\n", "<<<<not a marker\n", "a <<<<<<< b\n"} {
		if HasMarker(text) {
			t.Fatalf("false positive on %q", text)
		}
	}
}
