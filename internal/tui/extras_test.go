package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/sovengar/ec/internal/cli"
	"github.com/sovengar/ec/internal/engine"
	"github.com/sovengar/ec/internal/markers"
)

func TestUpdateEditorSavesDiff3(t *testing.T) {
	dir := t.TempDir()
	mergedPath := filepath.Join(dir, "merged.txt")
	if err := os.WriteFile(mergedPath, []byte("a\n<<<<<<< HEAD\nOURS\n=======\nTHEIRS\n>>>>>>> feature\nc\n"), 0o644); err != nil {
		t.Fatalf("write merged: %v", err)
	}

	doc, err := markers.Parse([]byte("a\n<<<<<<< HEAD\nOURS\n||||||| base\nB\n=======\nTHEIRS\n>>>>>>> feature\nc\n"))
	if err != nil {
		t.Fatalf("parse canonical: %v", err)
	}
	state, err := engine.NewState(doc)
	if err != nil {
		t.Fatalf("NewState: %v", err)
	}

	m := model{opts: cli.Options{MergedPath: mergedPath}, state: state}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = updated.(model)
	if !m.editing {
		t.Fatalf("e did not open the inline editor")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = updated.(model)
	if m.editing {
		t.Fatalf("ctrl+s did not close the editor: %s", m.editErr)
	}
	data, err := os.ReadFile(mergedPath)
	if err != nil {
		t.Fatalf("read merged: %v", err)
	}
	if !strings.Contains(string(data), "|||||||") {
		t.Fatalf("saved file lost diff3 base section:\n%s", data)
	}
}

func TestFooterWrapsAtSevenPerRow(t *testing.T) {
	segments := make([]footerSegment, 0, 10)
	for i := 0; i < 10; i++ {
		segments = append(segments, footerSegment{key: "k", label: "label"})
	}
	lines := wrapFooterSegments(segments)
	if len(lines) != 2 {
		t.Fatalf("rows = %d, want 2", len(lines))
	}
	if len(lines[0]) != 7 || len(lines[1]) != 3 {
		t.Fatalf("row sizes = %d/%d, want 7/3", len(lines[0]), len(lines[1]))
	}
}

func TestHelpOverlayListsFullKeybinds(t *testing.T) {
	m := model{}
	m.showHelp = true
	view := ansi.Strip(m.renderHelpOverlay(120, 40))
	for _, want := range []string{"Navigate hunks", "Select preview in result", "space", "tab", "Show", "abort merge"} {
		if !strings.Contains(view, want) {
			t.Fatalf("overlay missing %q:\n%s", want, view)
		}
	}
}

func TestTabTogglesSide(t *testing.T) {
	m := model{selectedSide: selectedOurs}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(model)
	if m.selectedSide != selectedTheirs {
		t.Fatalf("tab must select theirs")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(model)
	if m.selectedSide != selectedOurs {
		t.Fatalf("tab must select ours again")
	}
}

func TestEditorRendersSyntaxColor(t *testing.T) {
	useDiffTrueColor(t)
	doc, err := markers.Parse([]byte("<<<<<<< HEAD\nconst value = \"ours\";\n=======\nconst value = \"theirs\";\n>>>>>>> branch\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	state, err := engine.NewState(doc)
	if err != nil {
		t.Fatalf("NewState: %v", err)
	}
	m := model{state: state, opts: cli.Options{MergedPath: "demo.go"}, width: 80, height: 20}
	m.edit = newEditorState("demo.go", string(state.RenderMerged()), selectedOurs)
	m.editing = true
	view := m.renderEditorView()
	if !strings.Contains(view, "\x1b[") {
		t.Fatalf("editor output has no ANSI styling:\n%s", view)
	}
}

func TestHelpOverlaySectionsAndSearch(t *testing.T) {
	m := model{}
	m.showHelp = true
	view := ansi.Strip(m.renderHelpOverlay(160, 50))
	for _, want := range []string{"Navigation", "Resolve", "Editing", "Merge", "General", "/ search"} {
		if !strings.Contains(view, want) {
			t.Fatalf("overlay missing section %q:\n%s", want, view)
		}
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = updated.(model)
	for _, r := range "write" {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(model)
	}
	filtered := ansi.Strip(m.renderHelpOverlay(160, 50))
	if !strings.Contains(filtered, "write") {
		t.Fatalf("search must keep matching keybind:\n%s", filtered)
	}
	if strings.Contains(filtered, "Navigate hunks") {
		t.Fatalf("search must hide non-matching keybinds:\n%s", filtered)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	if !m.showHelp || m.helpQuery != "" {
		t.Fatalf("esc must clear the query before closing")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	if m.showHelp {
		t.Fatalf("second esc must close the overlay")
	}
}

func TestEditorReferencePanelAndSideToggle(t *testing.T) {
	doc, err := markers.Parse([]byte("a\n<<<<<<< HEAD\nours\n||||||| base\nbase\n=======\ntheirs\n>>>>>>> feature\nc\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	state, err := engine.NewState(doc)
	if err != nil {
		t.Fatalf("NewState: %v", err)
	}
	m := model{state: state, opts: cli.Options{MergedPath: "demo.txt"}, width: 120, height: 30}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = updated.(model)
	view := ansi.Strip(m.renderEditorView())
	if !strings.Contains(view, "(reference)") || !strings.Contains(view, "ours") {
		t.Fatalf("editor must show the clean ours reference:\n%s", view)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(model)
	view = ansi.Strip(m.renderEditorView())
	if !strings.Contains(view, "THEIRS (reference)") || !strings.Contains(view, "theirs") {
		t.Fatalf("tab must switch the reference to theirs:\n%s", view)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	m = updated.(model)
	view = ansi.Strip(m.renderEditorView())
	if strings.Contains(view, "(reference)") {
		t.Fatalf("ctrl+g must hide the reference panel:\n%s", view)
	}
}

func TestEditorPaintsConflictRegions(t *testing.T) {
	lines := []string{"<<<<<<< HEAD", "ours", "||||||| base", "base", "=======", "theirs", ">>>>>>> feature"}
	styles := editorLineStyles(lines)
	if styles[1].GetBackground() == styles[5].GetBackground() {
		t.Fatalf("ours and theirs regions must differ")
	}
	if styles[3].GetForeground() == styles[1].GetForeground() && styles[3].GetBackground() == styles[1].GetBackground() {
		t.Fatalf("base region must differ from ours")
	}
}
