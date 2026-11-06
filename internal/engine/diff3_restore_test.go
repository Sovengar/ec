package engine

import (
	"strings"
	"testing"

	"github.com/sovengar/ec/internal/markers"
)

func TestImportMergedPreservesCanonicalBase(t *testing.T) {
	canonical, err := markers.Parse([]byte("a\n<<<<<<< HEAD\nOURS\n||||||| base\nB\n=======\nTHEIRS\n>>>>>>> feature\nc\n"))
	if err != nil {
		t.Fatalf("parse canonical: %v", err)
	}
	state, err := NewState(canonical)
	if err != nil {
		t.Fatalf("NewState: %v", err)
	}

	// The merged file was written with the simple two-way marker style.
	simple := []byte("a\n<<<<<<< HEAD\nOURS\n=======\nTHEIRS\n>>>>>>> feature\nc\n")
	if err := state.ImportMerged(simple); err != nil {
		t.Fatalf("ImportMerged: %v", err)
	}

	got := string(state.RenderMerged())
	if !strings.Contains(got, "|||||||") {
		t.Fatalf("RenderMerged lost diff3 base section:\n%s", got)
	}
	if !strings.Contains(got, "B\n") {
		t.Fatalf("RenderMerged lost base body:\n%s", got)
	}
}

func TestApplyResolutionKeepsKnownMergedLabels(t *testing.T) {
	doc, err := markers.Parse([]byte("<<<<<<< HEAD\nours\n||||||| base\nbase\n=======\ntheirs\n>>>>>>> feature\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	state, err := NewState(doc)
	if err != nil {
		t.Fatalf("NewState: %v", err)
	}
	if err := state.ImportMerged([]byte("<<<<<<< HEAD\nours\n||||||| base\nbase\n=======\ntheirs\n>>>>>>> feature\n")); err != nil {
		t.Fatalf("ImportMerged: %v", err)
	}
	if err := state.ApplyResolution(0, markers.ResolutionOurs); err != nil {
		t.Fatalf("ApplyResolution: %v", err)
	}
	labels, known := state.MergedLabels()
	if len(labels) != 1 || !known[0] {
		t.Fatalf("labels known = %v, want true", known)
	}
	if labels[0].OursLabel != "HEAD" || labels[0].TheirsLabel != "feature" {
		t.Fatalf("labels = %+v, want HEAD/feature", labels[0])
	}
}
