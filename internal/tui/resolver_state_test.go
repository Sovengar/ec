package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/sovengar/ec/internal/markers"
)

func TestResolverHunkLabelsSeparateSelectionFromResolution(t *testing.T) {
	patch := []byte("<<<<<<< HEAD\nconst value = 1;\n||||||| base\nconst value = 0;\n=======\nconst value = 2;\n>>>>>>> branch\n")
	doc, err := markers.Parse(patch)
	if err != nil {
		t.Fatal(err)
	}
	for _, full := range []bool{false, true} {
		t.Run(fmt.Sprintf("full=%t", full), func(t *testing.T) {
			m := newModelForDoc(t, doc)
			if full {
				m.useFullDiff = true
				m.baseLines = []string{"const value = 0;"}
				m.oursLines = []string{"const value = 1;"}
				m.theirsLines = []string{"const value = 2;"}
				var ok bool
				m.conflictRanges, ok = computeConflictRanges(doc, m.baseLines, m.oursLines, m.theirsLines)
				if !ok {
					t.Fatal("fixture should support full-file diff")
				}
			}
			m = updateConflictSyntaxModel(t, m, tea.WindowSizeMsg{Width: 210, Height: 32})
			for _, step := range []struct {
				key, status string
			}{
				{status: "UNRESOLVED"}, {key: "l", status: "UNRESOLVED"},
				{key: "t", status: "RESOLVED: THEIRS"}, {key: "h", status: "RESOLVED: THEIRS"},
				{key: "u", status: "UNRESOLVED"},
			} {
				if step.key != "" {
					m = updateConflictSyntaxModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(step.key)})
				}
				for _, pane := range []struct {
					text     string
					styles   []lipgloss.Style
					selected bool
				}{
					{m.viewportOurs.View(), m.oursLineStyles, m.selectedSide == selectedOurs},
					{m.viewportTheirs.View(), m.theirsLineStyles, m.selectedSide == selectedTheirs},
				} {
					text := ansi.Strip(pane.text)
					if strings.Contains(text, "[SELECTED]") != pane.selected {
						t.Fatalf("only the selected source should be labeled: %q", text)
					}
					want := lineNumberStyle.GetForeground()
					if pane.selected {
						want = selectedHunkMarkerStyle.GetForeground()
					}
					if pane.styles[0].GetForeground() != want {
						t.Fatal("source hunk color does not match selection")
					}
					if pane.styles[len(pane.styles)-1].GetForeground() != lineNumberStyle.GetForeground() {
						t.Fatal("end boundaries should stay neutral")
					}
				}
				if !strings.Contains(ansi.Strip(m.viewportResult.View()), "Conflict 1 ["+step.status+"]") {
					t.Fatal("result hunk is missing its resolution status")
				}
				want := unresolvedLabelStyle.GetForeground()
				if strings.HasPrefix(step.status, "RESOLVED") {
					want = statusResolvedStyle.GetForeground()
				}
				if m.resultLineStyles[0].GetForeground() != want {
					t.Fatal("result hunk color does not match resolution")
				}
			}
		})
	}
}
