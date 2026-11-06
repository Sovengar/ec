# ec — three-pane merge conflict resolver with AI handoff

Go rewrite of `ec`, based on [chojs23/ec](https://github.com/chojs23/ec) (ec2).
The interactive view is the ec2 three-pane resolver: **OURS | RESULT | THEIRS**,
with a file selector, diff3 conflict view, undo/redo and syntax highlighting.
The AI handoff, merge abort and non-interactive modes are kept on top.

## Usage

```sh
ec                          # file selector, then the three-pane resolver
ec BASE LOCAL REMOTE MERGED # mergetool form
ec --base B --local L --remote R --merged M
ec --list                   # list conflicted files only
ec --check FILE...          # 0 = clean, 1 = conflicts remain
ec --apply-all ours|theirs|both|none FILE...
ec --emit FILE...           # AI handoff request as JSON
ec --apply-json reply.json FILE...
ec --ai 'agent-cmd' FILE... # pipe JSON to agent, apply its reply
ec --abort                  # cancel the merge, restore pre-merge state
```

Flags: `--backup`, `--dry-run`, `--yes`, `--version`, `--help`.
The old `--tui` flag still opens the resolver.

## Three-pane resolver

Rows are whole files: the resolver opens on the selected conflict, not on a hunk
list. The hunk marker shows the choice: `RESOLVED: OURS`, `RESOLVED: THEIRS`,
`RESOLVED: BOTH` or `RESOLVED: REDONE` for a manual edit. Nothing is written
until you press `w` (or save the editor).

Accepted hunks are kept in a draft under the user cache directory. Leaving the
TUI and reopening the same file restores the progress. The draft is removed
when the result is written (`w`), when the external editor closes, or when the
merge is aborted (`X`).

The RESULT pane border carries the current conflict, the remaining count and
the file name. The bottom is a rounded `Keybinds` box, 7 hints per row; the
preview-selection keys stay hidden there and show only in the `?` overlay,
which opens centered over the panes.

| Key | Action |
| --- | --- |
| `↑`/`↓`, `j` / `k` | navigate hunks |
| `ctrl+↑`/`ctrl+↓`, `ctrl+j` / `ctrl+k` | scroll the file line by line |
| `ctrl+←`/`ctrl+→` | scroll horizontally |
| `ctrl+h`/`ctrl+l`, `←`/`→` | Select preview in result (in `?` only) |
| `tab` | switch ours/theirs |
| `space` | Select (accept previewed side) |
| `?` | Keybinds overlay: grouped by section, `/` filters |
| `o` / `t` | apply ours / theirs (in `?` only) |
| `O` / `T` | apply ours / theirs to all conflicts |
| `b` / `x` | both / none (`x` deletes the block; there is no separate discard) |
| `u` / `ctrl+r` | undo / redo |
| `j` / `k`, arrows | scroll |
| `g g` / `G` / `z z` | top / bottom / recenter |
| `e` | edit the file in the TUI (syntax colored) |
| `E` | edit the file in `$EDITOR` |
| `A` | resolve selected conflict with the AI agent |
| `X` | abort the merge |
| `w` / `ctrl+s` | write the result |
| `esc` | back to file selector |
| `q` | quit the TUI (from anywhere) |

### e vs E

- `e` opens the in-TUI editor. The diff3 buffer paints ours/base/theirs regions
  with different colors, and a clean reference panel shows the previewed side.
  `tab` switches the reference side, `ctrl+g` shows/hides it, `ctrl+s` saves and
  reloads the resolver, `esc` cancels.
- `E` writes the current result and opens it in `$EDITOR` (default `vi`), then
  reloads the file.

Both keep the **diff3** conflict style: `<<<<<<<`, `||||||| base`, `=======`,
`>>>>>>>`. Files written by a plain two-way merge are re-rendered with the base
section from the merge stages, so the RESULT pane and both editors always show
the triple diff, never a simple ours-vs-theirs diff.

## AI handoff protocol

ec sends this on the agent's stdin:

```json
{"version":"1","files":[{"path":"f.txt","conflicts":[
  {"id":1,"start":2,"end":6,"ours":"A\n","base":"","theirs":"B\n",
   "before":["x\n"],"after":["z\n"]}]}]}
```

The agent replies on stdout:

```json
{"version":"1","resolutions":[
  {"path":"f.txt","id":1,"text":"AB\n","confidence":0.9,"note":"kept both"}]}
```

Guarantees before any write:

- every conflict id must have a resolution
- resolution text must contain no conflict markers
- write is atomic and keeps the file mode
- `--backup` writes `FILE.ec.bak`
- after apply, `--check` runs again
- exit codes: `0` clean, `1` conflicts remain, `2` usage or IO error

Set the agent for the `A` key with `--ai CMD` or `EC_AGENT=CMD`.

## Build and development

```sh
make check        # build + gofmt check + golangci-lint + go test -race
make install      # installs bin/ec into ~/.local/bin (run it after every change)
make smoke        # tmux smoke test against testdata/playground
make fixtures     # regenerates the conflict fixture (3 files, many blocks)
make run          # runs the resolver against the current repo
make help         # the full target list
```

Install path: `~/.local/bin/ec` comes before `/usr/bin` in PATH, so it shadows
the old binary. A stale install shows the old hunk-list TUI: reinstall after a
change.

Agent guide: `AGENTS.md`. Go toolchain note: this machine's `/usr/bin/go` is a
custom build that clashes with the mise GOROOT; the Makefile picks the mise
binary automatically.
