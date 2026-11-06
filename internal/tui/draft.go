package tui

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/sovengar/ec/internal/markers"
)

// draftFile keeps the resolver progress between TUI visits. The merged file is
// written only on `w`; without this, quitting after accepting hunks lost them.
type draftFile struct {
	Path     string `json:"path"`
	FileHash string `json:"file_hash"`
	Content  string `json:"content"`
}

func draftPathFor(mergedPath string) string {
	abs, err := filepath.Abs(mergedPath)
	if err != nil {
		abs = mergedPath
	}
	sum := sha256.Sum256([]byte(abs))
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "ec", "drafts", hex.EncodeToString(sum[:])+".json")
}

func fileHash(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// hasResolverProgress reports whether any conflict is resolved or manually
// edited. Without progress a draft would be noise, and an undo back to the
// baseline removes it.
func (m model) hasResolverProgress() bool {
	if len(m.state.ManualResolved()) > 0 {
		return true
	}
	doc := m.state.Document()
	for _, ref := range doc.Conflicts {
		seg, ok := doc.Segments[ref.SegmentIndex].(markers.ConflictSegment)
		if ok && seg.Resolution != markers.ResolutionUnset {
			return true
		}
	}
	return false
}

// saveDraft stores the current progress. The recorded file hash is the merged
// file as it is now, so a draft written after `w` still matches on reopen.
func (m *model) saveDraft() {
	if m.draftPath == "" || m.state == nil {
		return
	}
	if !m.hasResolverProgress() {
		m.removeDraft()
		return
	}
	abs, err := filepath.Abs(m.opts.MergedPath)
	if err != nil {
		return
	}
	hash, err := fileHash(m.opts.MergedPath)
	if err != nil {
		return
	}
	data, err := json.Marshal(draftFile{Path: abs, FileHash: hash, Content: string(m.state.RenderMerged())})
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(m.draftPath), 0o755); err != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(m.draftPath), ".draft-*")
	if err != nil {
		return
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return
	}
	_ = os.Rename(name, m.draftPath)
}

// applyDraft restores a matching draft and discards stale ones.
func (m *model) applyDraft() {
	if m.draftPath == "" || m.state == nil {
		return
	}
	data, err := os.ReadFile(m.draftPath)
	if err != nil {
		return
	}
	var draft draftFile
	if err := json.Unmarshal(data, &draft); err != nil {
		m.removeDraft()
		return
	}
	abs, err := filepath.Abs(m.opts.MergedPath)
	if err != nil || draft.Path != abs {
		m.removeDraft()
		return
	}
	hash, err := fileHash(m.opts.MergedPath)
	if err != nil || draft.FileHash != hash {
		m.removeDraft()
		return
	}
	if err := m.state.ImportMerged([]byte(draft.Content)); err != nil {
		m.removeDraft()
		return
	}
	m.refreshResolverCaches()
	m.currentConflict = firstUnresolvedConflict(m.doc, m.manualResolved)
}

func (m *model) removeDraft() {
	if m.draftPath == "" {
		return
	}
	if err := os.Remove(m.draftPath); err != nil && !os.IsNotExist(err) {
		return
	}
}
