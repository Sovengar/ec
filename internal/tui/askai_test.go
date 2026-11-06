package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/x/ansi"

	"github.com/sovengar/ec/internal/cli"
	"github.com/sovengar/ec/internal/engine"
	"github.com/sovengar/ec/internal/markers"
)

// fakeBridge is the picker's adapter seam: it answers the catalog questions and
// records the resolve call without spawning anything.
type fakeBridge struct {
	harnesses  []string
	models     map[string][]string
	efforts    map[string][]string
	caps       map[string][3]bool
	defaults   [3]string
	remembered [][3]string
	resolve    func(ctx context.Context, harness, model, effort, path, ours, base, theirs string) (string, error)
	calls      int
	lastCall   [7]string
}

func (f *fakeBridge) Harnesses() []string { return f.harnesses }

func (f *fakeBridge) Options(harness string) ([]string, []string) {
	return f.models[harness], f.efforts[harness]
}

func (f *fakeBridge) Capabilities(harness string) (bool, bool, bool) {
	c, ok := f.caps[harness]
	if !ok {
		// Unknown harnesses default to model+headless, no effort.
		return true, false, true
	}
	return c[0], c[1], c[2]
}

func (f *fakeBridge) Defaults() (string, string, string) {
	return f.defaults[0], f.defaults[1], f.defaults[2]
}

func (f *fakeBridge) Remember(harness, model, effort string) {
	f.remembered = append(f.remembered, [3]string{harness, model, effort})
}

func (f *fakeBridge) Resolve(ctx context.Context, harness, model, effort, path, ours, base, theirs string) (string, error) {
	f.calls++
	f.lastCall = [7]string{harness, model, effort, path, ours, base, theirs}
	if f.resolve != nil {
		return f.resolve(ctx, harness, model, effort, path, ours, base, theirs)
	}
	return "merged\n", nil
}

const askAIConflict = "start\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> branch\nend\n"

// newAskAIModel builds a ready resolver over a real temp merged file, so the
// file-fingerprint guard has something to hash.
func newAskAIModel(t *testing.T, raw string) (model, string) {
	t.Helper()
	doc, err := markers.Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return newAskAIModelWithDoc(t, raw, doc)
}

func newAskAIModelWithDoc(t *testing.T, raw string, doc markers.Document) (model, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	state, err := engine.NewState(doc)
	if err != nil {
		t.Fatal(err)
	}
	m := model{
		ctx:             context.Background(),
		opts:            cli.Options{MergedPath: path},
		state:           state,
		doc:             doc,
		currentConflict: 0,
		selectedSide:    selectedOurs,
		manualResolved:  map[int][]byte{},
		viewportOurs:    viewport.New(10, 5),
		viewportResult:  viewport.New(10, 5),
		viewportTheirs:  viewport.New(10, 5),
		width:           120,
		height:          40,
		ready:           true,
	}
	m.applyDraft()
	return m, path
}

func withBridge(t *testing.T, b AgentRunner) {
	t.Helper()
	previous := AgentBridge
	AgentBridge = b
	t.Cleanup(func() { AgentBridge = previous })
}

func pressKey(t *testing.T, m model, key tea.Msg) (model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(key)
	next, ok := updated.(model)
	if !ok {
		t.Fatalf("Update returned %T", updated)
	}
	return next, cmd
}

func runeKey(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

// configuredBridge is a pi-only catalog with models and efforts.
func configuredBridge() *fakeBridge {
	return &fakeBridge{
		harnesses: []string{"pi"},
		models:    map[string][]string{"pi": {"free", "big"}},
		efforts:   map[string][]string{"pi": {"low", "high"}},
		caps:      map[string][3]bool{"pi": {true, true, true}},
		defaults:  [3]string{"pi", "free", "low"},
	}
}

func TestAskAIResolvesTheSelectedConflict(t *testing.T) {
	bridge := configuredBridge()
	withBridge(t, bridge)
	m, path := newAskAIModel(t, askAIConflict)

	m, _ = pressKey(t, m, runeKey('a'))
	if !m.askAIOpen {
		t.Fatal("a must open the Ask AI overlay")
	}
	for _, want := range []string{"harness", "model", "effort", "pi"} {
		if !strings.Contains(ansi.Strip(m.View()), want) {
			t.Fatalf("overlay missing %q:\n%s", want, ansi.Strip(m.View()))
		}
	}
	if m.askAIIndex != 0 {
		t.Fatalf("first installed harness must be selected, index = %d", m.askAIIndex)
	}

	// enter on harness -> model, j picks a model, enter -> effort, j -> effort,
	// enter on the last field starts the run.
	m, _ = pressKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.askAIFocus != askAIFieldModel {
		t.Fatalf("enter must advance to the model field, got %d", m.askAIFocus)
	}
	m, _ = pressKey(t, m, runeKey('j'))
	if m.askAIModel != "big" {
		t.Fatalf("model = %q, want big", m.askAIModel)
	}
	m, _ = pressKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m, cmd := pressKey(t, m, runeKey('j'))
	if m.askAIEffort != "high" {
		t.Fatalf("effort = %q, want high", m.askAIEffort)
	}
	m, cmd = pressKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.askAIOpen {
		t.Fatal("confirming must close the overlay")
	}
	if !m.askAIBusy || !strings.Contains(ansi.Strip(m.View()), "resolving") {
		t.Fatalf("an in-flight run must be visible:\n%s", ansi.Strip(m.View()))
	}
	if cmd == nil {
		t.Fatal("confirming must start an asynchronous run")
	}
	if len(bridge.remembered) != 1 || bridge.remembered[0] != [3]string{"pi", "big", "high"} {
		t.Fatalf("choice not remembered: %v", bridge.remembered)
	}

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	m, _ = pressKey(t, m, cmd())
	if len(m.manualResolved) != 1 {
		t.Fatalf("conflict not resolved in memory: %v", m.manualResolved)
	}
	if !strings.Contains(string(m.state.RenderMerged()), "merged") {
		t.Fatalf("RESULT does not carry the resolution:\n%s", m.state.RenderMerged())
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("the AI path must never write the merged file; only w does")
	}
	if bridge.calls != 1 || bridge.lastCall[0] != "pi" || bridge.lastCall[1] != "big" || bridge.lastCall[2] != "high" {
		t.Fatalf("harness/model/effort not forwarded: %v", bridge.lastCall)
	}
	if bridge.lastCall[4] != "ours\n" || bridge.lastCall[6] != "theirs\n" {
		t.Fatalf("conflict sides not forwarded: %v", bridge.lastCall)
	}
}

func TestAskAIOnlyListsInstalledHarnesses(t *testing.T) {
	bridge := &fakeBridge{
		harnesses: []string{"pi"},
		caps:      map[string][3]bool{"pi": {true, true, true}},
	}
	withBridge(t, bridge)
	m, _ := newAskAIModel(t, askAIConflict)

	m, _ = pressKey(t, m, runeKey('a'))
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "pi") {
		t.Fatalf("installed harness not listed:\n%s", view)
	}
	if strings.Contains(view, "codex") {
		t.Fatalf("uninstalled harness must not be listed:\n%s", view)
	}
}

func TestAskAIRefusesAHarnessWithoutHeadless(t *testing.T) {
	bridge := &fakeBridge{
		harnesses: []string{"claude"},
		caps:      map[string][3]bool{"claude": {true, false, false}},
		defaults:  [3]string{"claude", "", ""},
	}
	withBridge(t, bridge)
	m, _ := newAskAIModel(t, askAIConflict)

	m, _ = pressKey(t, m, runeKey('a'))
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "no headless mode") {
		t.Fatalf("harness must be shown unavailable with the reason:\n%s", view)
	}

	// Confirm on the last supported field must not start anything.
	m.askAIFocus = askAIFieldModel
	m, cmd := pressKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("no process may start without a headless mode")
	}
	if m.askAIBusy {
		t.Fatal("busy state must not be set")
	}
	if !m.askAIOpen {
		t.Fatal("the overlay must stay open")
	}
	if bridge.calls != 0 {
		t.Fatal("Resolve must not be called")
	}
	if !strings.Contains(ansi.Strip(m.View()), "no headless mode") {
		t.Fatalf("the overlay must explain the refusal:\n%s", ansi.Strip(m.View()))
	}
}

func TestAskAIShowsUnsupportedEffort(t *testing.T) {
	bridge := &fakeBridge{
		harnesses: []string{"pi"},
		models:    map[string][]string{"pi": {"free"}},
		caps:      map[string][3]bool{"pi": {true, false, true}},
		defaults:  [3]string{"pi", "free", ""},
	}
	withBridge(t, bridge)
	m, _ := newAskAIModel(t, askAIConflict)

	m, _ = pressKey(t, m, runeKey('a'))
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "not supported") {
		t.Fatalf("effort must be shown as not supported:\n%s", view)
	}
	// The effort field is skipped, so enter on the model field runs directly.
	m, _ = pressKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.askAIFocus != askAIFieldModel {
		t.Fatalf("focus = %d, want model", m.askAIFocus)
	}
	m, cmd := pressKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("the harness must still run with the model it supports")
	}
	m, _ = pressKey(t, m, cmd())
	if bridge.lastCall[1] != "free" {
		t.Fatalf("model not forwarded: %v", bridge.lastCall)
	}
}

func TestAskAIAcceptsATypedModelWhenTheCatalogIsEmpty(t *testing.T) {
	bridge := &fakeBridge{
		harnesses: []string{"pi"},
		caps:      map[string][3]bool{"pi": {true, true, true}},
		defaults:  [3]string{"pi", "", ""},
	}
	withBridge(t, bridge)
	m, _ := newAskAIModel(t, askAIConflict)

	m, _ = pressKey(t, m, runeKey('a'))
	m.askAIFocus = askAIFieldModel
	for _, r := range "typed-model" {
		m, _ = pressKey(t, m, runeKey(r))
	}
	if m.askAIModel != "typed-model" {
		t.Fatalf("typed model = %q", m.askAIModel)
	}
	m, _ = pressKey(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	if m.askAIModel != "typed-mode" {
		t.Fatalf("backspace did not edit the model: %q", m.askAIModel)
	}
}

func TestAskAIEmptyCatalogRunsWithoutAModelFlag(t *testing.T) {
	bridge := &fakeBridge{
		harnesses: []string{"pi"},
		caps:      map[string][3]bool{"pi": {true, true, true}},
		defaults:  [3]string{"pi", "", ""},
	}
	withBridge(t, bridge)
	m, _ := newAskAIModel(t, askAIConflict)

	m, _ = pressKey(t, m, runeKey('a'))
	m.askAIFocus = askAIFieldModel
	m, _ = pressKey(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // -> effort
	m, cmd := pressKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("an empty model must be accepted")
	}
	m, _ = pressKey(t, m, cmd())
	if bridge.lastCall[1] != "" {
		t.Fatalf("empty model must be forwarded empty, got %q", bridge.lastCall[1])
	}
}

func TestAskAIWithNoHarnessInstalled(t *testing.T) {
	bridge := &fakeBridge{}
	withBridge(t, bridge)
	m, _ := newAskAIModel(t, askAIConflict)

	m, cmd := pressKey(t, m, runeKey('a'))
	if cmd != nil {
		t.Fatal("no process may start")
	}
	if !m.askAIOpen {
		t.Fatal("the overlay must open to explain the empty catalog")
	}
	if !strings.Contains(ansi.Strip(m.View()), "no agent harness was found") {
		t.Fatalf("missing empty-catalog message:\n%s", ansi.Strip(m.View()))
	}
	m, cmd = pressKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || bridge.calls != 0 {
		t.Fatal("nothing may run without a harness")
	}
}

func TestAskAIRejectsAnUnsafeReply(t *testing.T) {
	bridge := configuredBridge()
	bridge.resolve = func(context.Context, string, string, string, string, string, string, string) (string, error) {
		return "", errors.New("pi: resolution for conflict 1 contains conflict markers")
	}
	withBridge(t, bridge)
	m, _ := newAskAIModel(t, askAIConflict)
	before := string(m.state.RenderMerged())

	m, _ = pressKey(t, m, runeKey('a'))
	m, _ = pressKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = pressKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m, cmd := pressKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("want a run")
	}
	m, _ = pressKey(t, m, cmd())
	if len(m.manualResolved) != 0 {
		t.Fatalf("rejected reply must not change RESULT: %v", m.manualResolved)
	}
	if string(m.state.RenderMerged()) != before {
		t.Fatal("RESULT changed after a rejected reply")
	}
	if !strings.Contains(ansi.Strip(m.View()), "markers") {
		t.Fatalf("a toast must explain the rejection:\n%s", ansi.Strip(m.View()))
	}
}

func TestAskAIRunsAsynchronouslyAndGuardsASecondRun(t *testing.T) {
	release := make(chan struct{})
	bridge := configuredBridge()
	bridge.resolve = func(context.Context, string, string, string, string, string, string, string) (string, error) {
		<-release
		return "merged\n", nil
	}
	withBridge(t, bridge)
	m, _ := newAskAIModel(t, askAIConflict)

	m, _ = pressKey(t, m, runeKey('a'))
	m, _ = pressKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = pressKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m, cmd := pressKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || !m.askAIBusy {
		t.Fatal("the run must start in the background")
	}
	// Navigation still works while the harness works.
	m, _ = pressKey(t, m, tea.KeyMsg{Type: tea.KeyRight})
	if m.selectedSide != selectedTheirs {
		t.Fatal("navigation keys must keep working during a run")
	}
	m, _ = pressKey(t, m, runeKey('a'))
	if m.askAIOpen {
		t.Fatal("a second run must not start while the first is in flight")
	}
	if !strings.Contains(ansi.Strip(m.View()), "already in flight") {
		t.Fatalf("second press must be explained:\n%s", ansi.Strip(m.View()))
	}
	close(release)
	m, _ = pressKey(t, m, cmd())
	if m.askAIBusy {
		t.Fatal("busy state must clear when the reply arrives")
	}
}

func TestAskAIReplyAfterMovingOnIsDropped(t *testing.T) {
	bridge := configuredBridge()
	withBridge(t, bridge)
	m, _ := newAskAIModelWithDoc(t, askAIMultiConflict, parseAskAIMultiConflict(t))

	m, _ = pressKey(t, m, runeKey('a'))
	m, _ = pressKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = pressKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m, cmd := pressKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("want a run")
	}
	msg := cmd()
	// Move to conflict 2 before the reply lands.
	m, _ = pressKey(t, m, runeKey('j'))
	if m.currentConflict != 1 {
		t.Fatalf("currentConflict = %d, want 1", m.currentConflict)
	}
	before := len(m.manualResolved)
	m, _ = pressKey(t, m, msg)
	if len(m.manualResolved) != before {
		t.Fatalf("a stale reply must not be applied: %v", m.manualResolved)
	}
	if m.currentConflict != 1 {
		t.Fatal("moving on must not be undone by the reply")
	}
}

func TestAskAIReplyIsDroppedWhenTheFileChanges(t *testing.T) {
	bridge := configuredBridge()
	withBridge(t, bridge)
	m, path := newAskAIModel(t, askAIConflict)

	m, _ = pressKey(t, m, runeKey('a'))
	m, _ = pressKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = pressKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m, cmd := pressKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("want a run")
	}
	msg := cmd()
	// The harness (or the user) edits the merged file before the reply lands.
	if err := os.WriteFile(path, []byte(askAIConflict+"extra\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, _ = pressKey(t, m, msg)
	if len(m.manualResolved) != 0 {
		t.Fatalf("the reply must be discarded: %v", m.manualResolved)
	}
	if !strings.Contains(ansi.Strip(m.View()), "file changed") {
		t.Fatalf("a toast must say the file changed:\n%s", ansi.Strip(m.View()))
	}
}

func TestAskAIKeymapMovesToLowercaseA(t *testing.T) {
	bridge := configuredBridge()
	withBridge(t, bridge)
	m, _ := newAskAIModel(t, askAIConflict)

	m, _ = pressKey(t, m, runeKey('A'))
	if m.askAIOpen {
		t.Fatal("uppercase A must no longer open the overlay")
	}
	m, _ = pressKey(t, m, runeKey('a'))
	if !m.askAIOpen {
		t.Fatal("lowercase a must open the overlay")
	}

	// Space no longer accepts the previewed side.
	m2, _ := newAskAIModel(t, askAIConflict)
	m2.selectedSide = selectedTheirs
	m2, _ = pressKey(t, m2, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	if len(m2.manualResolved) != 0 || m2.undoDepth() != 0 {
		t.Fatal("space must no longer accept the previewed side")
	}

	// The keybind overlay advertises a and no accept binding.
	help := ansi.Strip(m.renderHelpOverlay(140, 50))
	if !strings.Contains(help, "ask AI") {
		t.Fatalf("help overlay must list a as ask AI:\n%s", help)
	}
	if strings.Contains(help, "space") {
		t.Fatalf("help overlay must not advertise an accept binding:\n%s", help)
	}
	for _, entry := range resolverKeyHelp {
		if entry.key == "space" {
			t.Fatal("resolverKeyHelp must not carry a space accept binding")
		}
	}
}

const askAIMultiConflict = "<<<<<<< HEAD\na\n=======\nb\n>>>>>>> x\n<<<<<<< HEAD\nc\n=======\nd\n>>>>>>> y\n"

func parseAskAIMultiConflict(t *testing.T) markers.Document {
	t.Helper()
	doc, err := markers.Parse([]byte(askAIMultiConflict))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}
