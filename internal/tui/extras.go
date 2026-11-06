package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/sovengar/ec/internal/markers"
)

// footerSegment is one key hint in the bottom keybinds box.
type footerSegment struct {
	key   string
	label string
}

// footerSegments is the full hint list; the box wraps it, 7 per row.
func footerSegments(m model) []footerSegment {
	segments := make([]footerSegment, 0, len(resolverKeyHelp)+2)
	for _, entry := range resolverKeyHelp {
		if entry.overlayOnly {
			continue
		}
		segments = append(segments, footerSegment{key: entry.key, label: entry.description})
	}
	if m.undoDepth() > 0 {
		segments = append(segments, footerSegment{key: fmt.Sprintf("u x%d", m.undoDepth()), label: "undo"})
	}
	if m.redoDepth() > 0 {
		segments = append(segments, footerSegment{key: fmt.Sprintf("ctrl+r x%d", m.redoDepth()), label: "redo"})
	}
	return segments
}

// wrapFooterSegments breaks the hints at 7 per row; extra rows flow below.
func wrapFooterSegments(segments []footerSegment) [][]footerSegment {
	const perRow = 7
	var lines [][]footerSegment
	var current []footerSegment
	for _, seg := range segments {
		if len(current) >= perRow {
			lines = append(lines, current)
			current = nil
		}
		current = append(current, seg)
	}
	if len(current) > 0 {
		lines = append(lines, current)
	}
	return lines
}

// renderFrame draws a rounded box with the title embedded in the top border,
// like dbx does for its panes.
func renderFrame(title, content string, width int, bar lipgloss.Style) string {
	lines := strings.Split(content, "\n")
	inner := 0
	for _, line := range lines {
		inner = max(inner, lipgloss.Width(line))
	}
	inner = max(inner, width-2)
	if inner < 2 {
		inner = 2
	}

	topLeft := "─" + title
	if lipgloss.Width(topLeft) > inner {
		topLeft = ansi.Truncate(topLeft, inner, "...")
	}
	top := "╭" + topLeft + strings.Repeat("─", max(0, inner-lipgloss.Width(topLeft))) + "╮"

	out := make([]string, 0, len(lines)+2)
	out = append(out, bar.Render(top))
	for _, line := range lines {
		if lipgloss.Width(line) > inner {
			line = ansi.Truncate(line, inner, "")
		}
		out = append(out, bar.Render("│")+line+strings.Repeat(" ", max(0, inner-lipgloss.Width(line)))+bar.Render("│"))
	}
	out = append(out, bar.Render("╰"+strings.Repeat("─", inner)+"╯"))
	return strings.Join(out, "\n")
}

// overlayCenter paints box over base, keeping the views behind it visible.
func overlayCenter(base, box string, width, height int) string {
	lines := strings.Split(base, "\n")
	blocks := strings.Split(box, "\n")
	bw := 0
	for _, block := range blocks {
		bw = max(bw, lipgloss.Width(block))
	}
	bh := len(blocks)
	if bh > height {
		bh = height
		blocks = blocks[:bh]
	}
	for len(lines) < height {
		lines = append(lines, strings.Repeat(" ", max(0, width)))
	}
	x := max(0, (width-bw)/2)
	y := max(0, (height-bh)/2)
	for j := 0; j < bh; j++ {
		line := lines[y+j]
		lines[y+j] = ansi.Truncate(line, x, "") + blocks[j] + ansi.TruncateLeft(line, x+bw, "")
	}
	return strings.Join(lines, "\n")
}

// renderPaneFrame draws one pane with its title embedded in the top border.
func renderPaneFrame(title, content string, width, height int, pane lipgloss.Style) string {
	title = " " + strings.TrimSpace(title) + " "
	inner := max(2, width-2)
	content = padToHeight(content, height, inner)
	bar := lipgloss.NewStyle().Foreground(pane.GetBorderTopForeground()).Bold(true)
	return renderFrame(title, content, width, bar)
}

// padToHeight fills the frame so the footer always sits at the terminal bottom.
func padToHeight(block string, height, width int) string {
	have := lipgloss.Height(block)
	if have >= height || height <= 0 {
		return block
	}
	blank := strings.Repeat(" ", max(0, width))
	lines := make([]string, 0, height-have)
	for i := 0; i < height-have; i++ {
		lines = append(lines, blank)
	}
	return block + "\n" + strings.Join(lines, "\n")
}

// Host hooks add project-specific actions to the resolver.
var (
	// AgentCommand is the shell command used by the AI handoff key.
	AgentCommand string
	// AgentResolve returns a resolved body for one conflict.
	AgentResolve func(path string, ours, base, theirs []byte) ([]byte, error)
	// AbortMergeFunc cancels the in-progress merge.
	AbortMergeFunc func() error
)

const (
	keyEditInline   = "e"
	keyEditExternal = "E"
	keyAI           = "A"
	keyAbortMerge   = "X"
)

func init() {
	resolverKeyActions[keyEditInline] = (*model).handleEditInline
	resolverKeyActions[keyEditExternal] = (*model).handleEdit
	resolverKeyActions[keyAI] = (*model).handleAI
	resolverKeyActions[keyAbortMerge] = (*model).handleAbortMerge
}

// handleToggleSide flips the preview between ours and theirs.
func (m *model) handleToggleSide() (tea.Cmd, error) {
	if m.selectedSide == selectedOurs {
		m.selectedSide = selectedTheirs
	} else {
		m.selectedSide = selectedOurs
	}
	m.updateViewports()
	return nil, nil
}

// handleEditInline opens the in-TUI editor on the current result.
func (m *model) handleEditInline() (tea.Cmd, error) {
	if m.state == nil {
		return nil, nil
	}
	m.edit = newEditorState(m.opts.MergedPath, string(m.state.RenderMerged()), m.selectedSide)
	m.editing = true
	m.editErr = ""
	return nil, nil
}

// handleAI resolves the selected conflict with the configured agent.
func (m *model) handleAI() (tea.Cmd, error) {
	if AgentResolve == nil {
		return m.showToast("no agent: use --ai CMD or EC_AGENT", 3), nil
	}
	if m.state == nil || m.currentConflict < 0 || m.currentConflict >= len(m.doc.Conflicts) {
		return nil, nil
	}
	seg, ok := m.currentConflictSegment()
	if !ok {
		return nil, nil
	}
	text, err := AgentResolve(m.opts.MergedPath, seg.Ours, seg.Base, seg.Theirs)
	if err != nil {
		return m.showToast("AI: "+err.Error(), 4), nil
	}
	if len(text) == 0 {
		return m.showToast("AI: empty resolution", 3), nil
	}
	if err := m.applyResolverMutation(func() error {
		return m.state.ApplyText(m.currentConflict, text)
	}); err != nil {
		return m.showToast("AI: "+err.Error(), 4), nil
	}
	m.refreshResolverCaches()
	m.updateViewports()
	return m.showToast("AI applied", 2), nil
}

// handleAbortMerge cancels the merge and returns to the selector.
func (m *model) handleAbortMerge() (tea.Cmd, error) {
	if AbortMergeFunc == nil {
		return m.showToast("abort not available", 2), nil
	}
	if err := AbortMergeFunc(); err != nil {
		return m.showToast("abort: "+err.Error(), 4), nil
	}
	m.removeDraft()
	m.err = ErrBackToSelector
	m.quitting = true
	return tea.Quit, nil
}

func (m *model) currentConflictSegment() (markers.ConflictSegment, bool) {
	ref := m.doc.Conflicts[m.currentConflict]
	seg, ok := m.doc.Segments[ref.SegmentIndex].(markers.ConflictSegment)
	return seg, ok
}

// updateHelp handles the keybind overlay: `/` starts a filter, `esc` clears it
// or closes, `q`/`?` close.
func (m model) updateHelp(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "esc":
		if m.helpQuery != "" {
			m.helpQuery = ""
			m.helpSearching = false
			return m, nil
		}
		m.showHelp = false
		return m, nil
	case "?", "q":
		m.showHelp = false
		m.helpQuery = ""
		m.helpSearching = false
		return m, nil
	case "/":
		m.helpSearching = true
		return m, nil
	}
	if m.helpSearching {
		switch key {
		case "backspace":
			runes := []rune(m.helpQuery)
			if len(runes) > 0 {
				m.helpQuery = string(runes[:len(runes)-1])
			}
		case "ctrl+u":
			m.helpQuery = ""
		case "enter":
			m.helpSearching = false
		default:
			if len(msg.Runes) > 0 && !msg.Alt {
				m.helpQuery += string(msg.Runes)
			}
		}
		return m, nil
	}
	m.showHelp = false
	return m, nil
}

// renderHelpOverlay lists every keybind in a centered rounded box, grouped by
// section and filtered by the `/` query.
func (m model) renderHelpOverlay(width, height int) string {
	boxWidth := width * 7 / 10
	if boxWidth < 50 {
		boxWidth = 50
	}
	if boxWidth > 90 {
		boxWidth = 90
	}
	query := strings.ToLower(strings.TrimSpace(m.helpQuery))
	matches := func(entry keyHelpEntry) bool {
		if query == "" {
			return true
		}
		return strings.Contains(strings.ToLower(entry.key+" "+entry.description), query)
	}

	var lines []string
	lastSection := ""
	for _, entry := range resolverKeyHelp {
		if !matches(entry) {
			continue
		}
		if entry.section != lastSection {
			if lastSection != "" {
				lines = append(lines, "")
			}
			lines = append(lines, titleStyle.Render(entry.section))
			lastSection = entry.section
		}
		lines = append(lines, "  "+fmt.Sprintf("%-26s", entry.key)+entry.description)
	}
	if len(lines) == 0 {
		lines = append(lines, "  no matching keybinds")
	}
	lines = append(lines, "")
	prompt := "  / " + m.helpQuery
	if m.helpSearching {
		prompt += "_"
	} else if m.helpQuery == "" {
		prompt = "  / search"
	}
	lines = append(lines, prompt)

	maxLines := max(5, height-4)
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	bar := lipgloss.NewStyle().Bold(true).Foreground(paneStyle.GetBorderTopForeground())
	return renderFrame(" Keybinds ", strings.Join(lines, "\n"), boxWidth, bar)
}

// editorState is the small in-TUI editor. It keeps syntax color and paints the
// conflict regions by side: a plain diff3 buffer is unreadable at a glance.
type editorState struct {
	lines    []string
	row      int
	col      int
	top      int
	filename string
	trailing bool
	refSide  selectionSide
	showRef  bool
}

func newEditorState(filename, src string, refSide selectionSide) editorState {
	trailing := strings.HasSuffix(src, "\n")
	body := strings.TrimSuffix(src, "\n")
	lines := strings.Split(body, "\n")
	return editorState{lines: lines, filename: filename, trailing: trailing, refSide: refSide, showRef: true}
}

func (e *editorState) text() string {
	s := strings.Join(e.lines, "\n")
	if e.trailing {
		s += "\n"
	}
	return s
}

func (e *editorState) clampCursor() {
	if e.row < 0 {
		e.row = 0
	}
	if e.row >= len(e.lines) {
		e.row = len(e.lines) - 1
	}
	if e.col < 0 {
		e.col = 0
	}
	if e.col > len([]rune(e.lines[e.row])) {
		e.col = len([]rune(e.lines[e.row]))
	}
}

func (e *editorState) clampTop(height int) {
	e.clampCursor()
	if height < 1 {
		height = 1
	}
	if e.row < e.top {
		e.top = e.row
	}
	if e.row >= e.top+height {
		e.top = e.row - height + 1
	}
	if e.top < 0 {
		e.top = 0
	}
}

func (e *editorState) insert(text string) {
	runes := []rune(e.lines[e.row])
	textRunes := []rune(text)
	next := make([]rune, 0, len(runes)+len(textRunes))
	next = append(next, runes[:e.col]...)
	next = append(next, textRunes...)
	next = append(next, runes[e.col:]...)
	e.lines[e.row] = string(next)
	e.col += len(textRunes)
}

func (e *editorState) newline() {
	runes := []rune(e.lines[e.row])
	head := string(runes[:e.col])
	tail := string(runes[e.col:])
	e.lines[e.row] = head
	e.lines = append(e.lines, "")
	copy(e.lines[e.row+2:], e.lines[e.row+1:])
	e.lines[e.row+1] = tail
	e.row++
	e.col = 0
}

func (e *editorState) backspace() {
	if e.col > 0 {
		runes := []rune(e.lines[e.row])
		e.lines[e.row] = string(append(runes[:e.col-1], runes[e.col:]...))
		e.col--
		return
	}
	if e.row == 0 {
		return
	}
	prev := []rune(e.lines[e.row-1])
	e.col = len(prev)
	e.lines[e.row-1] = string(prev) + e.lines[e.row]
	e.lines = append(e.lines[:e.row], e.lines[e.row+1:]...)
	e.row--
}

func (e *editorState) delete() {
	runes := []rune(e.lines[e.row])
	if e.col < len(runes) {
		e.lines[e.row] = string(append(runes[:e.col], runes[e.col+1:]...))
		return
	}
	if e.row+1 >= len(e.lines) {
		return
	}
	e.lines[e.row] = string(runes) + e.lines[e.row+1]
	e.lines = append(e.lines[:e.row+1], e.lines[e.row+2:]...)
}

func (e *editorState) moveRow(delta int) {
	e.row += delta
	e.clampCursor()
}

func (e *editorState) moveCol(delta int) {
	e.col += delta
	e.clampCursor()
}

func (e *editorState) home() { e.col = 0 }
func (e *editorState) end()  { e.col = len([]rune(e.lines[e.row])) }

// updateEditor routes keys to the inline editor while it is open.
func (m model) updateEditor(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "esc":
		m.editing = false
		m.editErr = ""
		return m, nil
	case "ctrl+s":
		if err := m.state.ImportMerged([]byte(m.edit.text())); err != nil {
			m.editErr = err.Error()
			return m, nil
		}
		if err := m.writeResolved(); err != nil {
			m.editErr = err.Error()
			return m, nil
		}
		m.editing = false
		m.editErr = ""
		m.refreshResolverCaches()
		m.updateViewports()
		return m, m.showToast("Saved", 2)
	case "tab":
		if m.edit.refSide == selectedOurs {
			m.edit.refSide = selectedTheirs
		} else {
			m.edit.refSide = selectedOurs
		}
	case "ctrl+g":
		m.edit.showRef = !m.edit.showRef
	case "up":
		m.edit.moveRow(-1)
	case "down":
		m.edit.moveRow(1)
	case "left":
		m.edit.moveCol(-1)
	case "right":
		m.edit.moveCol(1)
	case "home", "ctrl+a":
		m.edit.home()
	case "end", "ctrl+e":
		m.edit.end()
	case "enter":
		m.edit.newline()
	case "backspace", "ctrl+h":
		m.edit.backspace()
	case "delete", "ctrl+d":
		m.edit.delete()
	case "pgup":
		m.edit.moveRow(-max(1, m.editorHeight()))
	case "pgdown":
		m.edit.moveRow(max(1, m.editorHeight()))
	default:
		if key == " " {
			m.edit.insert(" ")
		} else if len(msg.Runes) > 0 && !msg.Alt {
			m.edit.insert(string(msg.Runes))
		}
	}
	m.edit.clampTop(m.editorHeight())
	return m, nil
}

func (m model) editorHeight() int { return max(3, m.height-3) }

// cleanSide renders the full file with every conflict replaced by one side.
func (m model) cleanSide(side selectionSide) string {
	if m.state == nil {
		return ""
	}
	doc := m.state.Document()
	var b strings.Builder
	for _, seg := range doc.Segments {
		switch s := seg.(type) {
		case markers.TextSegment:
			b.Write(s.Bytes)
		case markers.ConflictSegment:
			if side == selectedTheirs {
				b.Write(s.Theirs)
			} else {
				b.Write(s.Ours)
			}
		}
	}
	return b.String()
}

// renderEditorView draws the editor and, optionally, the clean reference side.
func (m model) renderEditorView() string {
	height := m.editorHeight()
	top := min(m.edit.top, max(0, len(m.edit.lines)-1))
	end := min(len(m.edit.lines), top+height)
	visible := append([]string(nil), m.edit.lines[top:end]...)
	cursorLine := m.edit.row - top

	var left strings.Builder
	for i, line := range renderEditorCode(m.edit.filename, visible, cursorLine, m.edit.col, editorLineStyles(visible)) {
		numStyle := lineNumberStyle
		if top+i == m.edit.row {
			numStyle = numStyle.Bold(true)
		}
		left.WriteString(numStyle.Render(fmt.Sprintf("%4d ", top+i+1)))
		left.WriteString(line)
		left.WriteString("\n")
	}
	leftTitle := fmt.Sprintf(" EDIT %s %d:%d · tab side · ctrl+g ref · ctrl+s save · esc cancel ",
		m.opts.MergedPath, m.edit.row+1, m.edit.col+1)
	bar := lipgloss.NewStyle().Foreground(paneStyle.GetBorderTopForeground()).Bold(true)

	panels := []string{renderFrame(leftTitle, strings.TrimSuffix(left.String(), "\n"), m.width, bar)}
	if m.edit.showRef {
		leftWidth := max(30, (m.width-2)*3/5)
		rightWidth := max(20, m.width-leftWidth)
		panels[0] = renderFrame(leftTitle, strings.TrimSuffix(left.String(), "\n"), leftWidth, bar)

		refLines := strings.Split(strings.TrimSuffix(m.cleanSide(m.edit.refSide), "\n"), "\n")
		if len(refLines) > height {
			refLines = refLines[:height]
		}
		refName := "OURS"
		if m.edit.refSide == selectedTheirs {
			refName = "THEIRS"
		}
		refText := strings.Join(renderEditorCode(m.edit.filename, refLines, -1, 0, editorLineStyles(refLines)), "\n")
		panels = append(panels, renderFrame(" "+refName+" (reference) ", refText, rightWidth, bar))
	}

	view := lipgloss.JoinHorizontal(lipgloss.Top, panels...)
	hint := footerStyle.Render(" tab: side · ctrl+g: reference · ctrl+s: save · esc: cancel ")
	if m.editErr != "" {
		hint = statusUnresolvedStyle.Render(" error: " + m.editErr + " ")
	}
	return lipgloss.JoinVertical(lipgloss.Left, view, hint)
}

// editorLineStyles paints diff3 regions: ours, base and theirs get different
// backgrounds so the editable buffer reads like the resolver panes.
func editorLineStyles(lines []string) []lipgloss.Style {
	styles := make([]lipgloss.Style, len(lines))
	marker := lipgloss.NewStyle().Bold(true).Foreground(statusUnresolvedStyle.GetForeground())
	regionOurs := resultLineStyle.Background(oursHighlightStyle.GetBackground())
	regionBase := resultLineStyle.Foreground(lineNumberStyle.GetForeground())
	regionTheirs := resultLineStyle.Background(theirsHighlightStyle.GetBackground())
	state := 0
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "<<<<<<<"):
			styles[i] = marker
			state = 1
		case strings.HasPrefix(line, "|||||||"):
			styles[i] = marker
			state = 2
		case strings.HasPrefix(line, "======="):
			styles[i] = marker
			state = 3
		case strings.HasPrefix(line, ">>>>>>>"):
			styles[i] = marker
			state = 0
		default:
			switch state {
			case 1:
				styles[i] = regionOurs
			case 2:
				styles[i] = regionBase
			case 3:
				styles[i] = regionTheirs
			default:
				styles[i] = resultLineStyle
			}
		}
	}
	return styles
}

// renderEditorCode colorises the visible lines and marks the cursor cell.
func renderEditorCode(filename string, lines []string, cursorLine, cursorCol int, bases []lipgloss.Style) []string {
	if len(lines) == 0 {
		return nil
	}
	baseAt := func(i int) lipgloss.Style {
		if i >= 0 && i < len(bases) {
			return bases[i]
		}
		return resultLineStyle
	}
	builders := make([]strings.Builder, len(lines))
	col := 0
	lineIdx := 0

	writeRune := func(r rune, style lipgloss.Style) {
		if lineIdx == cursorLine && col == cursorCol {
			builders[lineIdx].WriteString(baseAt(lineIdx).Reverse(true).Render(string(r)))
		} else {
			builders[lineIdx].WriteString(style.Render(string(r)))
		}
		col++
	}

	tokens := tokeniseDiffCode(filename, strings.Join(lines, "\n"))
	if tokens == nil {
		for i, line := range lines {
			lineIdx = i
			col = 0
			for _, r := range line {
				writeRune(r, baseAt(i))
			}
		}
	} else {
		for _, token := range tokens {
			for _, r := range token.Value {
				if r == '\n' {
					if lineIdx == cursorLine && col == cursorCol {
						builders[lineIdx].WriteString(baseAt(lineIdx).Reverse(true).Render(" "))
					}
					lineIdx++
					col = 0
					continue
				}
				if lineIdx >= len(lines) {
					break
				}
				writeRune(r, diffSyntaxTokenStyle(baseAt(lineIdx), token.Type))
			}
		}
	}
	if cursorLine >= 0 && cursorLine < len(lines) {
		runes := len([]rune(lines[cursorLine]))
		if cursorCol >= runes {
			builders[cursorLine].WriteString(baseAt(cursorLine).Reverse(true).Render(" "))
		}
	}

	out := make([]string, len(lines))
	for i := range builders {
		out[i] = builders[i].String()
	}
	return out
}
