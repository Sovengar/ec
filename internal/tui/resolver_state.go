package tui

import (
	"github.com/sovengar/ec/internal/markers"
)

type resolverHunkState struct {
	selected string
	applied  string // Empty means not applied. NONE is an explicit resolution.
	valid    bool
}

// label is the applied choice as shown next to RESOLVED. A manual edit is
// REDONE, not the side it happens to resemble.
func (s resolverHunkState) label() string {
	if s.applied == "" {
		return ""
	}
	if s.applied == "manual" {
		return "REDONE"
	}
	return s.applied
}

// Selection is keyboard intent. Applied state belongs to the merge document,
// so moving focus must never change the displayed accepted result.
func (m model) currentHunkState() resolverHunkState {
	state := resolverHunkState{selected: "OURS"}
	if m.selectedSide == selectedTheirs {
		state.selected = "THEIRS"
	}
	if m.currentConflict < 0 || m.currentConflict >= len(m.doc.Conflicts) {
		return state
	}
	ref := m.doc.Conflicts[m.currentConflict]
	if ref.SegmentIndex < 0 || ref.SegmentIndex >= len(m.doc.Segments) {
		return state
	}
	seg, ok := m.doc.Segments[ref.SegmentIndex].(markers.ConflictSegment)
	if !ok {
		return state
	}
	state.valid = true
	if _, manual := m.manualResolved[m.currentConflict]; manual {
		state.applied = "manual"
		return state
	}
	switch seg.Resolution {
	case markers.ResolutionOurs:
		state.applied = "OURS"
	case markers.ResolutionTheirs:
		state.applied = "THEIRS"
	case markers.ResolutionBoth:
		state.applied = "BOTH"
	case markers.ResolutionNone:
		state.applied = "NONE"
	}
	return state
}
