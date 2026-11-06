package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
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

// HandoffConflict is a conflict plus local context.
type HandoffConflict struct {
	Conflict
	Before []string `json:"before,omitempty"`
	After  []string `json:"after,omitempty"`
}

// Conflict marshals with context for the agent.
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

// BuildRequest turns parsed files into the handoff payload.
func BuildRequest(files []*ConflictFile, context int) Request {
	req := Request{Version: ProtocolVersion}
	for _, f := range files {
		if len(f.Conflicts) == 0 {
			continue
		}
		out := File{Path: f.Path}
		for _, c := range f.Conflicts {
			before, after := f.Context(c, context)
			out.Conflicts = append(out.Conflicts, HandoffConflict{
				Conflict: c,
				Before:   before,
				After:    after,
			})
		}
		req.Files = append(req.Files, out)
	}
	return req
}

// EncodeRequest renders the handoff payload as indented JSON.
func EncodeRequest(req Request) ([]byte, error) {
	return json.MarshalIndent(req, "", "  ")
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

// ApplyResponse writes resolutions back into files and reports leftovers.
func ApplyResponse(files []*ConflictFile, resp Response, backup, dryRun bool) (resolved int, failed []string, err error) {
	byPath := map[string]map[int]string{}
	for _, r := range resp.Resolutions {
		if r.ID <= 0 {
			return 0, nil, fmt.Errorf("resolution with invalid id %d", r.ID)
		}
		if r.Text != "" && hasMarker(r.Text) {
			failed = append(failed, fmt.Sprintf("%s#%d: markers in resolution", r.Path, r.ID))
			continue
		}
		if byPath[r.Path] == nil {
			byPath[r.Path] = map[int]string{}
		}
		byPath[r.Path][r.ID] = r.Text
	}
	for _, f := range files {
		texts := byPath[f.Path]
		if len(texts) == 0 {
			failed = append(failed, f.Path+": no resolutions")
			continue
		}
		missing := []int{}
		for _, c := range f.Conflicts {
			if _, ok := texts[c.ID]; !ok {
				missing = append(missing, c.ID)
			}
		}
		if len(missing) > 0 {
			sort.Ints(missing)
			failed = append(failed, fmt.Sprintf("%s: unresolved conflicts %v", f.Path, missing))
		}
		next, rerr := f.ReplaceBlocks(texts)
		if rerr != nil {
			failed = append(failed, rerr.Error())
			continue
		}
		if dryRun {
			fmt.Printf("--- %s (dry-run, %d conflict(s))\n", f.Path, len(f.Conflicts))
			resolved += len(f.Conflicts)
			continue
		}
		if backup {
			if _, berr := Backup(f.Path); berr != nil {
				return resolved, failed, fmt.Errorf("backup %s: %w", f.Path, berr)
			}
		}
		if werr := WriteAtomic(f.Path, next); werr != nil {
			return resolved, failed, fmt.Errorf("write %s: %w", f.Path, werr)
		}
		resolved += len(f.Conflicts)
	}
	return resolved, failed, nil
}

// RunAgent runs the agent command, feeding JSON on stdin.
func RunAgent(command string, req Request) (Response, error) {
	payload, err := EncodeRequest(req)
	if err != nil {
		return Response{}, err
	}
	cmd := exec.Command("sh", "-c", command)
	cmd.Stdin = bytes.NewReader(payload)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return Response{}, fmt.Errorf("agent %q: %w: %s", command, err, errBuf.String())
	}
	return ParseResponse(out.Bytes())
}

// LoadAll parses every path that contains conflict markers.
func LoadAll(paths []string) ([]*ConflictFile, error) {
	var files []*ConflictFile
	for _, p := range paths {
		f, err := Load(p)
		if err != nil {
			return nil, err
		}
		if len(f.Conflicts) > 0 {
			files = append(files, f)
		}
	}
	return files, nil
}

// Emit writes the handoff request to stdout.
func Emit(paths []string) error {
	files, err := LoadAll(paths)
	if err != nil {
		return err
	}
	payload, err := EncodeRequest(BuildRequest(files, 3))
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(payload)
	if err == nil {
		_, err = os.Stdout.Write([]byte("\n"))
	}
	return err
}

// decodeRequest reads a handoff payload back, used by tests and tooling.
func decodeRequest(data []byte) (Request, error) {
	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		return req, fmt.Errorf("decode request: %w", err)
	}
	return req, nil
}

// aiResolveOne asks the agent to resolve one conflict from the TUI.
func aiResolveOne(command, path string, ours, base, theirs []byte) ([]byte, error) {
	if command == "" {
		return nil, errors.New("no agent: use --ai CMD or EC_AGENT")
	}
	req := Request{Version: ProtocolVersion, Files: []File{{
		Path: path,
		Conflicts: []HandoffConflict{{
			Conflict: Conflict{
				ID:      1,
				Ours:    string(ours),
				Base:    string(base),
				Theirs:  string(theirs),
				HasBase: len(base) > 0,
			},
		}},
	}}}
	resp, err := RunAgent(command, req)
	if err != nil {
		return nil, err
	}
	for _, r := range resp.Resolutions {
		if r.ID == 1 {
			if hasMarker(r.Text) {
				return nil, errors.New("agent resolution contains conflict markers")
			}
			return []byte(r.Text), nil
		}
	}
	return nil, errors.New("agent gave no resolution for this conflict")
}
