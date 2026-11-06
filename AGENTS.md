# AGENTS.md — ec

A guide for agents with no prior context.

## What it is

A three-pane merge conflict resolver (TUI) in Go, plus non-interactive modes
and an AI handoff. The interactive view is a port of
[chojs23/ec](https://github.com/chojs23/ec) ("ec2"): file selector, then
`OURS | RESULT | THEIRS`, diff3 conflict style, undo/redo, syntax highlighting.

Local additions on top of ec2: AI handoff (`a` picker via agentbridge,
`--emit`, `--apply-json`), merge abort (`x`, `--abort`), file listing
(`--list`), in-TUI editor (`e`), external editor moved to `E`, and diff3
restoration when the merged file was written in the simple two-way style.

## Stack

- Go 1.26.3+, module `github.com/sovengar/ec` (agentbridge declares that floor)
- `charmbracelet/bubbletea`, `bubbles`, `lipgloss`, `chroma/v2`
- `internal/` packages vendored from ec2 (see `NOTICE.md`); edit them in place
- No cgo: git via subprocess (`git diff --name-only --diff-filter=U`,
  `git show :1/:2/:3:`, `git merge-file --diff3`)

## Commands

```bash
make check        # build + fmt-check + golangci-lint + go test -race
make install      # installs bin/ec into ~/.local/bin (MANDATORY after a change)
make smoke        # tmux smoke test against testdata/playground
make fixtures     # regenerates testdata/playground (many conflicts, 3 files)
make run          # runs the resolver against the current repo
```

**RULE**: after any change, run `make install`. The user runs
`~/.local/bin/ec`; a stale binary causes false symptoms (the old hunk-list TUI
appearing instead of the three panes).

The Go toolchain is mise's. If a bare `go` misbehaves, use
`env -u GOROOT ~/.local/share/mise/installs/go/1.27.3/bin/go`.

## Keymap contract

`↑`/`↓` and `j/k` navigate hunks; `ctrl+↑/↓` and `ctrl+j/k` scroll the file
line by line; `ctrl+←/→` scroll horizontally; `ctrl+h`/`ctrl+l` and the plain
arrow keys select the preview in RESULT; `tab` toggles ours/theirs; `o/t` apply
side, `O/T` all, `b` both, `d` discard, `u`/`ctrl+r` undo/redo, `gg/G/zz`
top/bottom/recenter, `e` in-TUI editor (syntax colored, conflict regions painted
by side, `tab` switches the clean reference side, `ctrl+g` toggles it), `E`
external editor, `a` Ask AI, `x` cancel merge, `w`/`ctrl+s` write, `esc` back to
selector, `q` quit, `?` keybind overlay grouped by section with `/` filtering.

`space` is deliberately unbound: accept was removed by user decision, and side
application stays on `o`/`t`/`O`/`T`. `A` is a no-op for the same reason; the
only AI entry point is lowercase `a`. There is no accept entry in
`resolverKeyHelp`. The bottom rounded `Keybinds`
box wraps the list at 7 hints per row and skips `overlayOnly` entries; `?`
shows the same list as a centered overlay. Both come from `resolverKeyHelp`.
Duplicate entries are a bug.

The RESULT hunk label carries the choice: `[RESOLVED: OURS|THEIRS|BOTH|REDONE]`
(a manual edit is REDONE). There is no separate state strip and no outer frame:
each pane title lives in its own border, and the RESULT one carries
`RESULT · Conflict N of M · R remaining · file`.

## Architecture

| Path | Role |
|---|---|
| `main.go` | CLI flags, non-interactive modes, TUI wiring and host hooks |
| `conflict.go`, `resolve.go`, `ai.go`, `abort.go`, `list.go` | extras: parse, strategies, CLI AI surfaces, merge abort, listing |
| `internal/handoff` | stdlib-only versioned wire protocol, prompt and tolerant reply extraction |
| `internal/agent` | the only agentbridge import: detect, options, capabilities, last choice, headless resolve |
| `internal/tui` | file selector, three panes, key actions; `extras.go` holds hooks/editors/abort, `askai.go` the Ask AI picker and async run |
| `internal/engine` | conflict state: apply resolutions, `ApplyText`, render, import merged |
| `internal/markers` | strict conflict parser/renderer (diff3 aware) |
| `internal/mergeview` | canonical document from base/local/remote stages |
| `internal/gitmerge` | `git merge-file --diff3` wrapper |
| `internal/gitutil` | git subprocess helpers, stages, repo discovery |
| `internal/run` | workspace loading, temp stage files, mode dispatch |
| `internal/cli` | options for the resolver |

## Critical gotchas

1. **Diff3 is required in the UI and both editors.** Conflicts render with
   `<<<<<<<`, `||||||| base`, `=======`, `>>>>>>>`. `State.restoreMissingBase`
   rebuilds the base section when the merged file only had two-way markers. Do
   not drop it, and keep the engine/tui tests that guard it.
2. **Write vs apply**: applying a side only changes in-memory state. `w`,
   `ctrl+s` in the in-TUI editor, and the external editor path are what write
   the file. Progress is kept in a draft under the user cache dir
   (`internal/tui/draft.go`), restored on reopen, and removed by a write or by
   `x` cancel. Do not clear it on `esc`/`q`: leaving must not lose progress.
3. **In-TUI editor**: keys go to the editor while `m.editing`; `ctrl+s` saves
   and reloads, `esc` cancels, `tab` switches the reference side, `ctrl+g`
   toggles the clean reference panel. External editor is `E` (`$EDITOR`, default
   `vi`), not `e`.
4. **Temp stages**: workspace mode writes `ec-base-*`, `ec-local-*`,
   `ec-remote-*` under `os.TempDir()` and removes them on cleanup. `4-path`
   mode uses the given files directly.
5. **TUI smoke tests**: use tmux `capture-pane`; `script` gives a blank
   alt-screen with bubbletea.
6. **`d` discards, `x` cancels the merge**: they are not duplicates. `d`
   applies `ResolutionNone`; `x` aborts the merge. The old `X` alias is gone.
7. **Watermark**: the AI reply text must contain no conflict markers; the
   engine rejects nothing by itself, so `internal/handoff.HasMarker` guards both
   `ApplyResponse` and `agent.Bridge.Resolve`. Agentbridge must never be
   imported outside `internal/agent`.
8. **Ask AI is asynchronous**: the picker only collects harness/model/effort;
   `startAskAI` returns a `tea.Cmd` and the reply arrives as `aiResolvedMsg`. It
   is applied only when its visit, sequence, conflict index and the merged-file
   fingerprint all still match. The AI path never writes the file — only `w`,
   the editor, and `--apply-json` do.
9. **Overlay ownership**: while `askAIOpen` the resolver consumes keys first, and
   `workspaceModel.childOverlayOpen` keeps `esc` from closing the whole resolver.
10. **`run` name clash**: package `internal/run` is imported as `ecrun` in
   `main.go` because `run` is the CLI entry function.

## Conventions

- Comments in English, and only for the WHY; no godoc that repeats the name.
- Tests are direct: build the state/model, call the function, inspect state.
  No golden TUI snapshots; tmux captures are for `make smoke`.
- New user-visible behavior updates `README.md` in the same change.
- `NOTICE.md` must list any new path vendored from ec2.
