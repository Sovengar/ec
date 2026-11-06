package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/sovengar/ec/internal/cli"
	ecrun "github.com/sovengar/ec/internal/run"
	"github.com/sovengar/ec/internal/tui"
)

const version = "0.1.0"

const usage = `ec - merge conflict resolver with AI handoff

Usage:
  ec                          list conflicted files and pick one
  ec --list                   list conflicted files only
  ec --check PATH...          exit 0 when no conflict markers remain
  ec --apply-all STRATEGY PATH... resolve every block (ours|theirs|both|none)
  ec --emit PATH...           print the AI handoff request (JSON)
  ec --apply-json FILE PATH... apply an agent reply from a file
  ec --ai CMD PATH...         pipe JSON to CMD, apply its reply
  ec --abort                  cancel the merge and restore pre-merge state
  ec --tui                    open the interactive panel (buttons)

Compatibility form (old ec):
  ec <BASE> <LOCAL> <REMOTE> <MERGED>
  ec --base B --local L --remote R --merged M

Options:
  --backup        write PATH.ec.bak before replacing PATH
  --dry-run       report what would change, write nothing
  --yes           do not ask before --abort
  --version       print version
  --help          print this text

AI handoff:
  ec emits {"version":"1","files":[{path,conflicts:[{id,start,end,ours,base,theirs,before,after}]}]}
  agent replies {"version":"1","resolutions":[{path,id,text,confidence,note}]}
  ec verifies every conflict id, rejects markers, writes atomically, then re-checks.

Exit codes: 0 clean, 1 conflicts remain, 2 usage or IO error.
`

type options struct {
	check     bool
	applyAll  string
	backup    bool
	dryRun    bool
	emit      bool
	applyJSON string
	ai        string
	list      bool
	abort     bool
	tui       bool
	yes       bool
	base      string
	local     string
	remote    string
	merged    string
	version   bool
	help      bool
	paths     []string
}

func parseArgs(args []string) (*options, error) {
	o := &options{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			o.paths = append(o.paths, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") {
			o.paths = append(o.paths, a)
			continue
		}
		name, val := a, ""
		hasVal := false
		if k := strings.IndexByte(a, '='); k >= 0 {
			name, val, hasVal = a[:k], a[k+1:], true
		}
		need := func() (string, error) {
			if hasVal {
				return val, nil
			}
			if i+1 >= len(args) {
				return "", fmt.Errorf("flag %s needs a value", name)
			}
			i++
			return args[i], nil
		}
		var err error
		switch name {
		case "--check":
			o.check = true
		case "--apply-all":
			o.applyAll, err = need()
		case "--backup":
			o.backup = true
		case "--dry-run":
			o.dryRun = true
		case "--emit":
			o.emit = true
		case "--apply-json":
			o.applyJSON, err = need()
		case "--ai":
			o.ai, err = need()
		case "--list":
			o.list = true
		case "--abort":
			o.abort = true
		case "--tui":
			o.tui = true
		case "--yes", "-y":
			o.yes = true
		case "--base":
			o.base, err = need()
		case "--local":
			o.local, err = need()
		case "--remote":
			o.remote, err = need()
		case "--merged":
			o.merged, err = need()
		case "-v", "--version":
			o.version = true
		case "-h", "--help":
			o.help = true
		default:
			return nil, fmt.Errorf("unknown flag %s", name)
		}
		if err != nil {
			return nil, err
		}
	}
	return o, nil
}

// targetPaths resolves the files ec should act on.
func (o *options) targetPaths() ([]string, error) {
	if o.merged != "" {
		return []string{o.merged}, nil
	}
	if len(o.paths) == 4 && (o.check || o.applyAll != "") {
		if o.base == "" && o.local == "" && o.remote == "" {
			o.base, o.local, o.remote = o.paths[0], o.paths[1], o.paths[2]
		}
		return []string{o.paths[3]}, nil
	}
	if len(o.paths) > 0 {
		return o.paths, nil
	}
	// No explicit paths: fall back to the conflicted files from git.
	paths, err := ListConflicts()
	if err != nil {
		return nil, err
	}
	return paths, nil
}

func run(args []string) int {
	o, err := parseArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ec:", err)
		return 2
	}
	if o.help {
		fmt.Print(usage)
		return 0
	}
	if o.version {
		fmt.Println("ec v" + version)
		return 0
	}

	switch {
	case o.abort:
		return runAbort(o)
	case o.tui:
		return runEc2TUI(o)
	case o.list:
		return runListOnly()
	case o.emit:
		paths, err := o.targetPaths()
		if err != nil {
			return usageErr(err)
		}
		if err := Emit(paths); err != nil {
			fmt.Fprintln(os.Stderr, "ec:", err)
			return 2
		}
		return 0
	case o.check:
		paths, err := o.targetPaths()
		if err != nil {
			return usageErr(err)
		}
		return runCheck(paths)
	case o.applyAll != "":
		paths, err := o.targetPaths()
		if err != nil {
			return usageErr(err)
		}
		return runApplyAll(paths, o)
	case o.applyJSON != "":
		paths, err := o.targetPaths()
		if err != nil {
			return usageErr(err)
		}
		return runApplyJSON(o.applyJSON, paths, o)
	case o.ai != "":
		paths, err := o.targetPaths()
		if err != nil {
			return usageErr(err)
		}
		return runAI(o.ai, paths, o)
	case len(o.paths) == 0 && isTerminal(os.Stdin):
		return runEc2TUI(o)
	case len(o.paths) == 4:
		return runEc2TUI(o)
	case len(o.paths) == 0:
		return runList()
	default:
		paths, err := o.targetPaths()
		if err != nil {
			return usageErr(err)
		}
		return runCheck(paths)
	}
}

func usageErr(err error) int {
	fmt.Fprintln(os.Stderr, "ec:", err)
	fmt.Fprint(os.Stderr, "\nRun 'ec --help' for usage.\n")
	return 2
}

func runAbort(o *options) int {
	active, err := MergeInProgress("")
	if err != nil {
		fmt.Fprintln(os.Stderr, "ec:", err)
		return 2
	}
	if !active {
		fmt.Println("ec: no merge in progress")
		return 0
	}
	if o.dryRun {
		fmt.Println("ec: would abort merge")
		return 0
	}
	if !o.yes && isTerminal(os.Stdin) {
		fmt.Print("abort merge and restore pre-merge state? [y/N]: ")
		var answer string
		_, _ = fmt.Fscanln(os.Stdin, &answer)
		if strings.ToLower(strings.TrimSpace(answer)) != "y" {
			fmt.Println("ec: abort cancelled")
			return 0
		}
	}
	if err := AbortMerge("", false); err != nil {
		fmt.Fprintln(os.Stderr, "ec:", err)
		return 2
	}
	active, conflicts, _ := CancelState("")
	if active || conflicts > 0 {
		fmt.Fprintln(os.Stderr, "ec: abort incomplete, merge still active")
		return 1
	}
	fmt.Println("ec: merge aborted")
	return 0
}

// runEc2TUI opens the three-pane resolver. With no paths it uses the
// repository's conflicted files; four paths select the mergetool form.
func runEc2TUI(o *options) int {
	agent := o.ai
	if agent == "" {
		agent = os.Getenv("EC_AGENT")
	}
	tui.AgentCommand = agent
	tui.AgentResolve = func(path string, ours, base, theirs []byte) ([]byte, error) {
		return aiResolveOne(agent, path, ours, base, theirs)
	}
	tui.AbortMergeFunc = func() error { return AbortMerge("", false) }

	opts := cli.Options{Backup: o.backup}
	switch {
	case o.merged != "":
		opts.BasePath, opts.LocalPath = o.base, o.local
		opts.RemotePath, opts.MergedPath = o.remote, o.merged
	case len(o.paths) == 4:
		opts.BasePath, opts.LocalPath = o.paths[0], o.paths[1]
		opts.RemotePath, opts.MergedPath = o.paths[2], o.paths[3]
	}
	return ecrun.Run(context.Background(), opts)
}

func runCheck(paths []string) int {
	dirty := 0
	for _, p := range paths {
		f, err := Load(p)
		if err != nil {
			fmt.Fprintln(os.Stderr, "ec:", err)
			return 2
		}
		if len(f.Conflicts) > 0 {
			dirty++
			fmt.Printf("%s: %d conflict(s)\n", p, len(f.Conflicts))
		}
	}
	if dirty > 0 {
		return 1
	}
	return 0
}

func runApplyAll(paths []string, o *options) int {
	strategy, err := ParseStrategy(o.applyAll)
	if err != nil {
		return usageErr(err)
	}
	dirty := 0
	for _, p := range paths {
		f, err := Load(p)
		if err != nil {
			fmt.Fprintln(os.Stderr, "ec:", err)
			return 2
		}
		if len(f.Conflicts) == 0 {
			continue
		}
		dirty++
		next, n, err := f.Resolve(strategy)
		if err != nil {
			fmt.Fprintln(os.Stderr, "ec:", err)
			return 2
		}
		if o.dryRun {
			fmt.Printf("%s: would resolve %d conflict(s) with %s\n", p, n, strategy)
			continue
		}
		if o.backup {
			if _, err := Backup(p); err != nil {
				fmt.Fprintln(os.Stderr, "ec:", err)
				return 2
			}
		}
		if err := WriteAtomic(p, next); err != nil {
			fmt.Fprintln(os.Stderr, "ec:", err)
			return 2
		}
		fmt.Printf("%s: resolved %d conflict(s) with %s\n", p, n, strategy)
	}
	if dirty > 0 {
		return 0
	}
	return 0
}

func runApplyJSON(file string, paths []string, o *options) int {
	data, err := os.ReadFile(file)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ec:", err)
		return 2
	}
	resp, err := ParseResponse(data)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ec:", err)
		return 2
	}
	return applyAndReport(paths, resp, o)
}

func runAI(command string, paths []string, o *options) int {
	files, err := LoadAll(paths)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ec:", err)
		return 2
	}
	if len(files) == 0 {
		fmt.Println("ec: no conflicts found")
		return 0
	}
	req := BuildRequest(files, 3)
	resp, err := RunAgent(command, req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ec:", err)
		return 2
	}
	return applyAndReport(paths, resp, o)
}

func applyAndReport(paths []string, resp Response, o *options) int {
	files, err := LoadAll(paths)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ec:", err)
		return 2
	}
	resolved, failed, err := ApplyResponse(files, resp, o.backup, o.dryRun)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ec:", err)
		return 2
	}
	for _, msg := range failed {
		fmt.Fprintln(os.Stderr, "ec:", msg)
	}
	fmt.Printf("resolved %d conflict(s)\n", resolved)
	if len(failed) > 0 {
		return 1
	}
	if o.dryRun {
		return 0
	}
	return runCheck(paths)
}

func main() {
	os.Exit(run(os.Args[1:]))
}
