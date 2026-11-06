package main

import (
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/sovengar/ec/internal/handoff"
)

// ProtocolVersion is the wire version of the AI handoff.
const ProtocolVersion = handoff.ProtocolVersion

// Request is what ec hands to an agent.
type Request = handoff.Request

// File carries one conflicted file.
type File = handoff.File

// HandoffConflict is a conflict plus local context.
type HandoffConflict = handoff.HandoffConflict

// Response is what the agent returns.
type Response = handoff.Response

// Resolution is one resolved conflict block.
type Resolution = handoff.Resolution

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
				Conflict: handoff.Conflict{
					ID:      c.ID,
					Start:   c.Start,
					End:     c.End,
					Ours:    c.Ours,
					Base:    c.Base,
					Theirs:  c.Theirs,
					HasBase: c.HasBase,
				},
				Before: before,
				After:  after,
			})
		}
		req.Files = append(req.Files, out)
	}
	return req
}

// EncodeRequest renders the handoff payload as indented JSON.
func EncodeRequest(req Request) ([]byte, error) {
	return handoff.EncodeRequest(req)
}

// ParseResponse decodes and validates an agent reply.
func ParseResponse(data []byte) (Response, error) {
	return handoff.ParseResponse(data)
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
	return handoff.DecodeRequest(data)
}

// errAIRemoved explains the removal of --ai. Its old value was a shell command,
// so aliasing it to a harness name would silently change what it does.
var errAIRemoved = errors.New(`--ai was removed: the AI handoff now runs through the resolver's "a" overlay; ` +
	`for scripted handoffs use --emit and --apply-json`)
