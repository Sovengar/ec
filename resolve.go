package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Strategy is a non-interactive resolution choice.
type Strategy string

const (
	Ours   Strategy = "ours"
	Theirs Strategy = "theirs"
	Both   Strategy = "both"
	None   Strategy = "none"
)

// ParseStrategy validates a --apply-all value.
func ParseStrategy(s string) (Strategy, error) {
	switch Strategy(s) {
	case Ours, Theirs, Both, None:
		return Strategy(s), nil
	}
	return "", fmt.Errorf("unknown strategy %q (want ours|theirs|both|none)", s)
}

// Resolve returns the file content with every conflict replaced.
func (f *ConflictFile) Resolve(s Strategy) (string, int, error) {
	lines := append([]string(nil), f.Lines...)
	for i := len(f.Conflicts) - 1; i >= 0; i-- {
		c := f.Conflicts[i]
		var repl string
		switch s {
		case Ours:
			repl = c.Ours
		case Theirs:
			repl = c.Theirs
		case Both:
			repl = joinLines(c.Ours, c.Theirs)
		case None:
			repl = ""
		default:
			return "", 0, fmt.Errorf("unknown strategy %q", s)
		}
		from, to := c.Start-1, c.End
		if from < 0 || to > len(lines) || from >= to {
			return "", 0, fmt.Errorf("%s: conflict %d has invalid range %d..%d", f.Path, c.ID, c.Start, c.End)
		}
		next := make([]string, 0, len(lines)-(to-from)+len(splitLines(repl)))
		next = append(next, lines[:from]...)
		next = append(next, splitLines(repl)...)
		next = append(next, lines[to:]...)
		lines = next
	}
	return strings.Join(lines, ""), len(f.Conflicts), nil
}

// ResolveOne resolves a single conflict block and leaves the rest untouched.
func (f *ConflictFile) ResolveOne(id int, s Strategy) (string, Conflict, error) {
	var target Conflict
	found := false
	for _, c := range f.Conflicts {
		if c.ID == id {
			target, found = c, true
			break
		}
	}
	if !found {
		return "", Conflict{}, fmt.Errorf("%s: no conflict %d", f.Path, id)
	}
	var repl string
	switch s {
	case Ours:
		repl = target.Ours
	case Theirs:
		repl = target.Theirs
	case Both:
		repl = joinLines(target.Ours, target.Theirs)
	case None:
		repl = ""
	default:
		return "", target, fmt.Errorf("unknown strategy %q", s)
	}
	lines := append([]string(nil), f.Lines...)
	next := make([]string, 0, len(lines)-(target.End-target.Start)+len(splitLines(repl)))
	next = append(next, lines[:target.Start-1]...)
	next = append(next, splitLines(repl)...)
	next = append(next, lines[target.End:]...)
	return strings.Join(next, ""), target, nil
}

// ReplaceBlocks swaps conflict block i for text, used by the AI apply path.
func (f *ConflictFile) ReplaceBlocks(texts map[int]string) (string, error) {
	lines := append([]string(nil), f.Lines...)
	for i := len(f.Conflicts) - 1; i >= 0; i-- {
		c := f.Conflicts[i]
		text, ok := texts[c.ID]
		if !ok {
			return "", fmt.Errorf("%s: conflict %d has no resolution", f.Path, c.ID)
		}
		if hasMarker(text) {
			return "", fmt.Errorf("%s: resolution for conflict %d still contains markers", f.Path, c.ID)
		}
		from, to := c.Start-1, c.End
		next := make([]string, 0, len(lines)-(to-from)+len(splitLines(text)))
		next = append(next, lines[:from]...)
		next = append(next, splitLines(text)...)
		next = append(next, lines[to:]...)
		lines = next
	}
	return strings.Join(lines, ""), nil
}

func hasMarker(text string) bool {
	for _, l := range splitLines(text) {
		for _, m := range []string{markerOurs, markerBase, markerSep, markerTheirs} {
			if isMarker(l, m) {
				return true
			}
		}
	}
	return false
}

// Load reads and parses one file.
func Load(path string) (*ConflictFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseFile(path, string(data))
}

// WriteAtomic writes content next to the target, preserving its mode.
func WriteAtomic(path, content string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".ec-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, info.Mode().Perm()); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// Backup copies path to path.ec.bak, keeping the original safe.
func Backup(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	dst := path + ".ec.bak"
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return "", err
	}
	return dst, nil
}
