package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// aiRunTimeout bounds one headless run so a wedged harness cannot hold the
// "resolving" state forever.
const aiRunTimeout = 2 * time.Minute

// AgentRunner is the harness adapter the resolver consumes. It lives here as an
// interface so the TUI never imports agentbridge and tests drive the picker with
// a fake. The real implementation is internal/agent.
type AgentRunner interface {
	// Harnesses lists the installed harnesses, sorted by name.
	Harnesses() []string
	// Options returns the local model and effort lists, possibly empty.
	Options(harness string) (models, efforts []string)
	// Capabilities reports which controls the harness actually accepts.
	Capabilities(harness string) (model, effort, headless bool)
	// Defaults resolves the preselection: env override, remembered choice,
	// then the first detected harness.
	Defaults() (harness, model, effort string)
	// Remember persists the picker's selection for the next run.
	Remember(harness, model, effort string)
	// Resolve runs one harness headless on one conflict and returns its text.
	Resolve(ctx context.Context, harness, model, effort, path, ours, base, theirs string) (string, error)
}

// AgentBridge is wired by main. A nil value keeps the key inert instead of
// failing at runtime.
var AgentBridge AgentRunner

// The picker fields, in tab order.
const (
	askAIFieldHarness = iota
	askAIFieldModel
	askAIFieldEffort
	askAIFieldCount
)

// aiResolvedMsg is the asynchronous result of one headless run. It carries the
// identity of the request so a reply that arrives after the user moved on can
// be dropped instead of applied to the wrong conflict.
type aiResolvedMsg struct {
	visitID     uint64
	seq         uint64
	conflict    int
	fingerprint string
	text        string
	err         error
}

// overlayOpen reports whether a centered overlay owns the keyboard. The
// workspace uses it so its esc/q interception cannot steal the overlay's keys.
func (m model) overlayOpen() bool {
	return m.askAIOpen || m.showHelp || m.editing
}

// handleAI opens (or toggles) the Ask AI picker. It never runs a harness: the
// run is confirmed inside the overlay, so opening it is always cheap.
func (m *model) handleAI() (tea.Cmd, error) {
	if m.editing {
		return nil, nil
	}
	if m.askAIBusy {
		return m.showToast("AI: a run is already in flight", 3), nil
	}
	if m.askAIOpen {
		m.closeAskAI()
		return nil, nil
	}
	if AgentBridge == nil {
		return m.showToast("AI: no agent bridge configured", 3), nil
	}
	m.showHelp = false
	m.askAIOpen = true
	m.askAIFocus = askAIFieldHarness
	m.askAIErr = ""
	m.askAINote = ""
	m.askAIList = AgentBridge.Harnesses()
	if len(m.askAIList) == 0 {
		m.askAINote = "no agent harness was found"
		m.askAIModels = nil
		m.askAIEfforts = nil
		m.askAIModel = ""
		m.askAIEffort = ""
		return nil, nil
	}
	harness, modelName, effort := AgentBridge.Defaults()
	m.askAIIndex = indexOfString(m.askAIList, harness)
	m.selectAskAIHarness(m.askAIIndex)
	m.askAIModel = modelName
	m.askAIEffort = effort
	m.askAIModelIdx = indexOfString(m.askAIModels, modelName)
	m.askAIEffortIdx = indexOfString(m.askAIEfforts, effort)
	return nil, nil
}

func indexOfString(values []string, want string) int {
	for i, v := range values {
		if v == want {
			return i
		}
	}
	return 0
}

// selectAskAIHarness loads the option lists of the harness at index, keeping
// whatever the user already chose when it is still offered.
func (m *model) selectAskAIHarness(index int) {
	if index < 0 || index >= len(m.askAIList) {
		return
	}
	m.askAIIndex = index
	name := m.askAIList[index]
	previousModel, previousEffort := m.askAIModel, m.askAIEffort
	m.askAIModels, m.askAIEfforts = AgentBridge.Options(name)
	if _, hasEffort, _ := AgentBridge.Capabilities(name); !hasEffort {
		m.askAIModels, m.askAIEfforts = m.askAIModels, nil
		m.askAIEffort = ""
		m.askAIEffortIdx = 0
	}
	m.askAIModelIdx = indexOfString(m.askAIModels, previousModel)
	if len(m.askAIModels) > 0 {
		m.askAIModel = m.askAIModels[m.askAIModelIdx]
	} else {
		m.askAIModel = previousModel
	}
	m.askAIEffortIdx = indexOfString(m.askAIEfforts, previousEffort)
	if len(m.askAIEfforts) > 0 {
		m.askAIEffort = m.askAIEfforts[m.askAIEffortIdx]
	} else if _, hasEffort, _ := AgentBridge.Capabilities(name); hasEffort {
		m.askAIEffort = previousEffort
	}
}

func (m *model) closeAskAI() {
	m.askAIOpen = false
	m.askAIErr = ""
}

// askAIFieldSupported reports whether a field can be set for the selected
// harness. Unsupported fields are displayed but skipped by tab.
func (m model) askAIFieldSupported(field int) bool {
	if field == askAIFieldHarness {
		return len(m.askAIList) > 0
	}
	if len(m.askAIList) == 0 {
		return false
	}
	name := m.askAIList[min(m.askAIIndex, len(m.askAIList)-1)]
	hasModel, hasEffort, _ := AgentBridge.Capabilities(name)
	switch field {
	case askAIFieldModel:
		return hasModel
	case askAIFieldEffort:
		return hasEffort
	}
	return false
}

// updateAskAI is the picker's key router: tab moves fields, j/k changes the
// focused value, typing edits a free-text model or effort, enter advances and
// finally runs, esc closes.
func (m model) updateAskAI(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "esc":
		m.closeAskAI()
		return m, nil
	case "tab":
		m.moveAskAIFocus(1)
		return m, nil
	case "shift+tab":
		m.moveAskAIFocus(-1)
		return m, nil
	case "j", "down":
		m.moveAskAIValue(1)
		return m, nil
	case "k", "up":
		m.moveAskAIValue(-1)
		return m, nil
	case "enter":
		return m.confirmAskAI()
	case "backspace":
		m.editAskAIFreeText("", true)
		return m, nil
	}
	if len(msg.Runes) > 0 && !msg.Alt {
		m.editAskAIFreeText(string(msg.Runes), false)
	}
	return m, nil
}

// moveAskAIFocus moves to the next supported field, skipping the ones the
// harness does not expose instead of pretending they work.
func (m *model) moveAskAIFocus(delta int) {
	for step := 1; step <= askAIFieldCount; step++ {
		next := ((m.askAIFocus+delta*step)%askAIFieldCount + askAIFieldCount) % askAIFieldCount
		if m.askAIFieldSupported(next) {
			m.askAIFocus = next
			return
		}
	}
}

// moveAskAIValue changes the focused field's value.
func (m *model) moveAskAIValue(delta int) {
	switch m.askAIFocus {
	case askAIFieldHarness:
		if len(m.askAIList) == 0 {
			return
		}
		next := cycleIndex(m.askAIIndex, len(m.askAIList), delta)
		m.selectAskAIHarness(next)
	case askAIFieldModel:
		if len(m.askAIModels) == 0 {
			return
		}
		m.askAIModelIdx = cycleIndex(m.askAIModelIdx, len(m.askAIModels), delta)
		m.askAIModel = m.askAIModels[m.askAIModelIdx]
	case askAIFieldEffort:
		if len(m.askAIEfforts) == 0 {
			return
		}
		m.askAIEffortIdx = cycleIndex(m.askAIEffortIdx, len(m.askAIEfforts), delta)
		m.askAIEffort = m.askAIEfforts[m.askAIEffortIdx]
	}
}

// editAskAIFreeText types a model or effort when the catalog is empty. A field
// backed by a catalog stays a list, so the value can never drift from it.
func (m *model) editAskAIFreeText(text string, backspace bool) {
	var target *string
	var hasCatalog bool
	switch m.askAIFocus {
	case askAIFieldModel:
		if !m.askAIFieldSupported(askAIFieldModel) {
			return
		}
		target, hasCatalog = &m.askAIModel, len(m.askAIModels) > 0
	case askAIFieldEffort:
		if !m.askAIFieldSupported(askAIFieldEffort) {
			return
		}
		target, hasCatalog = &m.askAIEffort, len(m.askAIEfforts) > 0
	default:
		return
	}
	if hasCatalog {
		return
	}
	if backspace {
		runes := []rune(*target)
		if len(runes) > 0 {
			*target = string(runes[:len(runes)-1])
		}
		return
	}
	*target += text
}

// cycleIndex wraps an index into [0,size).
func cycleIndex(index, size, delta int) int {
	if size <= 0 {
		return 0
	}
	return ((index+delta)%size + size) % size
}

// confirmAskAI advances to the next field, or starts the run on the last one.
func (m model) confirmAskAI() (tea.Model, tea.Cmd) {
	next := -1
	for candidate := m.askAIFocus + 1; candidate < askAIFieldCount; candidate++ {
		if m.askAIFieldSupported(candidate) {
			next = candidate
			break
		}
	}
	if next != -1 {
		m.askAIFocus = next
		return m, nil
	}
	return m.startAskAI()
}

// startAskAI launches the headless run as a tea.Cmd, so the resolver keeps
// repainting and accepting navigation while the harness works.
func (m model) startAskAI() (tea.Model, tea.Cmd) {
	if m.askAIBusy || len(m.askAIList) == 0 || AgentBridge == nil {
		return m, nil
	}
	if m.state == nil || m.currentConflict < 0 || m.currentConflict >= len(m.doc.Conflicts) {
		m.askAIErr = "no conflict selected"
		return m, nil
	}
	harness := m.askAIList[min(m.askAIIndex, len(m.askAIList)-1)]
	if _, _, headless := AgentBridge.Capabilities(harness); !headless {
		// Refuse before spawning anything: no process, overlay stays open.
		m.askAIErr = harness + ": no headless mode"
		return m, nil
	}
	seg, ok := m.currentConflictSegment()
	if !ok {
		m.askAIErr = "no conflict selected"
		return m, nil
	}
	fingerprint, _ := fileHash(m.opts.MergedPath)
	m.askAISeq++
	seq := m.askAISeq
	conflict := m.currentConflict
	visitID := m.visitID
	path := m.opts.MergedPath
	modelName, effort := m.askAIModel, m.askAIEffort
	AgentBridge.Remember(harness, modelName, effort)

	m.askAIOpen = false
	m.askAIErr = ""
	m.askAIBusy = true
	m.aiStatus = fmt.Sprintf("resolving with %s...", harness)

	base := string(seg.Base)
	parent := m.ctx
	if parent == nil {
		parent = context.Background()
	}
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(parent, aiRunTimeout)
		defer cancel()
		text, err := AgentBridge.Resolve(ctx, harness, modelName, effort, path, string(seg.Ours), base, string(seg.Theirs))
		return aiResolvedMsg{
			visitID:     visitID,
			seq:         seq,
			conflict:    conflict,
			fingerprint: fingerprint,
			text:        text,
			err:         err,
		}
	}
}

// resolveAIResult applies a reply only when it still belongs to the conflict on
// screen and the merged file is unchanged since the run started.
func (m model) resolveAIResult(msg aiResolvedMsg) (tea.Model, tea.Cmd) {
	if msg.visitID != m.visitID || msg.seq != m.askAISeq {
		return m, nil
	}
	m.askAIBusy = false
	m.aiStatus = ""
	if msg.err != nil {
		return m, m.showToast("AI: "+msg.err.Error(), 4)
	}
	// Stale reply: the user moved on, so conflict 1 keeps its previous state.
	if msg.conflict != m.currentConflict {
		return m, nil
	}
	if current, err := fileHash(m.opts.MergedPath); err != nil || current != msg.fingerprint {
		return m, m.showToast("file changed on disk; AI reply discarded", 4)
	}
	if strings.TrimSpace(msg.text) == "" {
		return m, m.showToast("AI: empty resolution", 3)
	}
	if err := m.applyResolverMutation(func() error {
		return m.state.ApplyText(msg.conflict, []byte(msg.text))
	}); err != nil {
		return m, m.showToast("AI: "+err.Error(), 4)
	}
	m.refreshResolverCaches()
	m.updateViewports()
	return m, m.showToast("AI applied", 2)
}

// renderAskAIOverlay draws the picker over the resolver panes.
func (m model) renderAskAIOverlay(width, height int) string {
	lines := []string{""}
	switch {
	case m.askAINote != "":
		lines = append(lines, "  "+m.askAINote)
	case len(m.askAIList) == 0:
		lines = append(lines, "  no agent harness was found")
	default:
		name := m.askAIList[min(m.askAIIndex, len(m.askAIList)-1)]
		hasModel, hasEffort, hasHeadless := m.askAICapabilities(name)
		harnessLine := "  harness  " + m.paintField(name, askAIFieldHarness)
		if !hasHeadless {
			harnessLine += "  (unavailable: no headless mode)"
		}
		lines = append(lines, harnessLine)
		modelLine := "  model    " + m.paintField(m.askAIModel, askAIFieldModel)
		if !hasModel {
			modelLine += "  (not supported)"
		} else if len(m.askAIModels) == 0 {
			modelLine += "  (type a model, empty sends no flag)"
		}
		lines = append(lines, modelLine)
		effortLine := "  effort   " + m.paintField(m.askAIEffort, askAIFieldEffort)
		if !hasEffort {
			effortLine += "  (not supported)"
		} else if m.askAIEffort == "" {
			effortLine += "  (none)"
		}
		lines = append(lines, effortLine)
	}
	if m.askAIErr != "" {
		lines = append(lines, "")
		lines = append(lines, "  "+m.askAIErr)
	}
	lines = append(lines, "")
	lines = append(lines, titleStyle.Render("  tab field · j/k value · enter run · esc close"))
	lines = append(lines, "")

	boxWidth := max(50, min(90, width*6/10))
	bar := lipgloss.NewStyle().Bold(true).Foreground(paneStyle.GetBorderTopForeground())
	box := renderFrame(" Ask AI ", strings.Join(lines, "\n"), boxWidth, bar)
	return overlayCenter(m.renderResolverView(), box, width, height)
}

func (m model) askAICapabilities(name string) (model, effort, headless bool) {
	if AgentBridge == nil {
		return false, false, false
	}
	return AgentBridge.Capabilities(name)
}

// paintField highlights the focused field.
func (m model) paintField(text string, field int) string {
	if text == "" {
		text = "-"
	}
	if m.askAIFocus == field {
		return lipgloss.NewStyle().Bold(true).Underline(true).Render(text)
	}
	return text
}

// renderAIStatusLine shows the in-flight run so a slow harness never looks
// hung. It collapses to nothing when no run is active.
func (m model) renderAIStatusLine() string {
	if !m.askAIBusy || m.aiStatus == "" {
		return ""
	}
	return lipgloss.NewStyle().
		Foreground(footerStyle.GetForeground()).
		Width(m.width).
		Render(" " + m.aiStatus)
}
