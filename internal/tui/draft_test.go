package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sovengar/ec/internal/cli"
	"github.com/sovengar/ec/internal/engine"
	"github.com/sovengar/ec/internal/markers"
)

const draftFixture = "<<<<<<< HEAD\nours\n||||||| base\nbase\n=======\ntheirs\n>>>>>>> feature\n"

func newDraftModel(t *testing.T, mergedPath, draftPath, content string) model {
	t.Helper()
	doc, err := markers.Parse([]byte(content))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	state, err := engine.NewState(doc)
	if err != nil {
		t.Fatalf("NewState: %v", err)
	}
	m := model{
		opts:            cli.Options{MergedPath: mergedPath},
		state:           state,
		doc:             doc,
		currentConflict: 0,
		selectedSide:    selectedOurs,
		manualResolved:  map[int][]byte{},
		draftPath:       draftPath,
	}
	m.refreshResolverCaches()
	return m
}

func TestDraftRestoresResolutions(t *testing.T) {
	dir := t.TempDir()
	mergedPath := filepath.Join(dir, "f.txt")
	draftPath := filepath.Join(dir, "draft.json")
	if err := os.WriteFile(mergedPath, []byte(draftFixture), 0o644); err != nil {
		t.Fatalf("write merged: %v", err)
	}

	m := newDraftModel(t, mergedPath, draftPath, draftFixture)
	if _, err := m.handleApplyOurs(); err != nil {
		t.Fatalf("apply ours: %v", err)
	}
	if _, err := os.Stat(draftPath); err != nil {
		t.Fatalf("draft not written: %v", err)
	}

	restored := newDraftModel(t, mergedPath, draftPath, draftFixture)
	restored.applyDraft()
	doc := restored.state.Document()
	seg := doc.Segments[doc.Conflicts[0].SegmentIndex].(markers.ConflictSegment)
	if seg.Resolution != markers.ResolutionOurs {
		t.Fatalf("resolution = %q, want ours", seg.Resolution)
	}
}

func TestDraftDiscardedWhenMergedFileChanged(t *testing.T) {
	dir := t.TempDir()
	mergedPath := filepath.Join(dir, "f.txt")
	draftPath := filepath.Join(dir, "draft.json")
	if err := os.WriteFile(mergedPath, []byte(draftFixture), 0o644); err != nil {
		t.Fatalf("write merged: %v", err)
	}
	m := newDraftModel(t, mergedPath, draftPath, draftFixture)
	if _, err := m.handleApplyOurs(); err != nil {
		t.Fatalf("apply ours: %v", err)
	}

	if err := os.WriteFile(mergedPath, []byte("changed\n"), 0o644); err != nil {
		t.Fatalf("rewrite merged: %v", err)
	}
	restored := newDraftModel(t, mergedPath, draftPath, draftFixture)
	restored.applyDraft()
	doc := restored.state.Document()
	seg := doc.Segments[doc.Conflicts[0].SegmentIndex].(markers.ConflictSegment)
	if seg.Resolution != markers.ResolutionUnset {
		t.Fatalf("stale draft applied, resolution = %q", seg.Resolution)
	}
	if _, err := os.Stat(draftPath); !os.IsNotExist(err) {
		t.Fatalf("stale draft must be removed, stat err = %v", err)
	}
}

func TestDraftRemovedAfterWrite(t *testing.T) {
	dir := t.TempDir()
	mergedPath := filepath.Join(dir, "f.txt")
	draftPath := filepath.Join(dir, "draft.json")
	if err := os.WriteFile(mergedPath, []byte(draftFixture), 0o644); err != nil {
		t.Fatalf("write merged: %v", err)
	}
	m := newDraftModel(t, mergedPath, draftPath, draftFixture)
	if _, err := m.handleApplyOurs(); err != nil {
		t.Fatalf("apply ours: %v", err)
	}
	if err := m.writeResolved(); err != nil {
		t.Fatalf("writeResolved: %v", err)
	}
	if _, err := os.Stat(draftPath); !os.IsNotExist(err) {
		t.Fatalf("draft must be removed after write, stat err = %v", err)
	}
}

func TestDraftRemovedWithoutProgress(t *testing.T) {
	dir := t.TempDir()
	mergedPath := filepath.Join(dir, "f.txt")
	draftPath := filepath.Join(dir, "draft.json")
	if err := os.WriteFile(mergedPath, []byte(draftFixture), 0o644); err != nil {
		t.Fatalf("write merged: %v", err)
	}
	if err := os.WriteFile(draftPath, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write draft: %v", err)
	}
	m := newDraftModel(t, mergedPath, draftPath, draftFixture)
	m.saveDraft()
	if _, err := os.Stat(draftPath); !os.IsNotExist(err) {
		t.Fatalf("draft must be removed without progress, stat err = %v", err)
	}
}
