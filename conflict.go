package main

import (
	"fmt"
	"strings"
)

// Conflict is one conflict region inside a file.
type Conflict struct {
	ID      int    `json:"id"`
	Start   int    `json:"start"` // 1-based line of the <<<<<<< marker
	End     int    `json:"end"`   // 1-based line of the >>>>>>> marker
	Ours    string `json:"ours"`
	Base    string `json:"base,omitempty"`
	Theirs  string `json:"theirs"`
	HasBase bool   `json:"has_base,omitempty"`
}

// ConflictFile is a file parsed for conflict markers.
type ConflictFile struct {
	Path      string     `json:"path"`
	Raw       string     `json:"-"`
	Lines     []string   `json:"-"`
	Conflicts []Conflict `json:"conflicts"`
}

const (
	markerOurs   = "<<<<<<<"
	markerBase   = "|||||||"
	markerSep    = "======="
	markerTheirs = ">>>>>>>"
)

func isMarker(line, marker string) bool {
	line = strings.TrimRight(line, "\r\n")
	if !strings.HasPrefix(line, marker) {
		return false
	}
	rest := line[len(marker):]
	return rest == "" || strings.HasPrefix(rest, " ")
}

// splitLines keeps the trailing newline on every line.
func splitLines(raw string) []string {
	var lines []string
	for len(raw) > 0 {
		i := strings.IndexByte(raw, '\n')
		if i < 0 {
			lines = append(lines, raw)
			break
		}
		lines = append(lines, raw[:i+1])
		raw = raw[i+1:]
	}
	return lines
}

// ParseFile parses conflict blocks out of raw.
func ParseFile(path, raw string) (*ConflictFile, error) {
	f := &ConflictFile{Path: path, Raw: raw, Lines: splitLines(raw)}
	for i := 0; i < len(f.Lines); i++ {
		if !isMarker(f.Lines[i], markerOurs) {
			continue
		}
		c := Conflict{ID: len(f.Conflicts) + 1, Start: i + 1}
		var ours, base, theirs []string
		target := &ours
		j := i + 1
		closed := false
		for ; j < len(f.Lines); j++ {
			l := f.Lines[j]
			switch {
			case isMarker(l, markerBase) && target == &ours:
				target = &base
				c.HasBase = true
			case isMarker(l, markerSep) && target != &theirs:
				target = &theirs
			case isMarker(l, markerTheirs):
				c.End = j + 1
				closed = true
			default:
				*target = append(*target, l)
			}
			if closed {
				break
			}
		}
		if !closed {
			return nil, fmt.Errorf("%s:%d: unterminated conflict block", path, i+1)
		}
		c.Ours = strings.Join(ours, "")
		c.Base = strings.Join(base, "")
		c.Theirs = strings.Join(theirs, "")
		f.Conflicts = append(f.Conflicts, c)
		i = j
	}
	return f, nil
}

// Context returns up to n untouched lines before and after a conflict.
func (f *ConflictFile) Context(c Conflict, n int) (before, after []string) {
	for i := c.Start - 1 - n; i < c.Start-1; i++ {
		if i >= 0 {
			before = append(before, f.Lines[i])
		}
	}
	for i := c.End; i < c.End+n && i < len(f.Lines); i++ {
		after = append(after, f.Lines[i])
	}
	return before, after
}

// joinLines concatenates raw fragments, keeping newline boundaries sane.
func joinLines(parts ...string) string {
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p)
	}
	return b.String()
}
