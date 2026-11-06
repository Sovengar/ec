// Package agent is the only place ec imports agentbridge.
//
// Confining the dependency here keeps harness/argv knowledge out of the TUI and
// makes an agentbridge API change a one-file edit. The TUI consumes the small
// surface below through a package-level interface variable.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sovengar/agentbridge"

	"github.com/sovengar/ec/internal/handoff"
)

// Env overrides are the scriptable form of the picker's defaults.
const (
	EnvHarness = "EC_HARNESS"
	EnvModel   = "EC_MODEL"
	EnvEffort  = "EC_EFFORT"
)

// lastChoiceFile keeps the picker's last selection between runs.
const lastChoiceFile = "agent-last.json"

// lookPath probes a binary on PATH; a variable so tests never touch the real
// PATH and detection stays deterministic.
var lookPath = exec.LookPath

// cacheDir resolves the per-user cache root; a variable so tests use a temp
// tree instead of the developer's home.
var cacheDir = os.UserCacheDir

// Bridge adapts agentbridge to the resolver: which harnesses exist, what they
// offer, how to remember a choice, and how to run one conflict headless.
type Bridge struct {
	// Agents is the detected catalog. It is injected by tests and by New.
	Agents []agentbridge.Agent
	// Models is the catalog source. LocalSource reads each harness's on-disk
	// model list; a stale or missing list is not an error.
	Models agentbridge.ModelSource
	// Invoker runs the headless process; nil means the real process invoker.
	Invoker agentbridge.Invoker
}

// New detects the installed harnesses. Detection happens once per resolver
// visit, so a harness installed mid-session is picked up on the next visit.
func New() *Bridge {
	return &Bridge{
		Agents: agentbridge.Detect(nil, lookPath),
		Models: agentbridge.LocalSource{},
	}
}

// Harnesses lists the detected harness names, sorted by agentbridge.
func (b *Bridge) Harnesses() []string {
	names := make([]string, 0, len(b.Agents))
	for _, a := range b.Agents {
		names = append(names, a.Name)
	}
	return names
}

func (b *Bridge) agentNamed(name string) (agentbridge.Agent, bool) {
	for _, a := range b.Agents {
		if a.Name == name {
			return a, true
		}
	}
	return agentbridge.Agent{}, false
}

// Options returns the model and effort lists of one harness. An empty list is
// normal: the picker then accepts free text.
func (b *Bridge) Options(harness string) (models, efforts []string) {
	a, ok := b.agentNamed(harness)
	if !ok {
		return nil, nil
	}
	source := b.Models
	if source == nil {
		source = agentbridge.LocalSource{}
	}
	options, err := source.Options(context.Background(), a)
	if err != nil {
		return nil, nil
	}
	return options.Models, options.Efforts
}

// Capabilities reports which controls the harness actually accepts. A missing
// flag is surfaced as unsupported rather than invented.
func (b *Bridge) Capabilities(harness string) (model, effort, headless bool) {
	caps := agentbridge.CapabilitiesFor(harness)
	return len(caps.ModelFlag) > 0, len(caps.EffortFlag) > 0, len(caps.Headless) > 0
}

// choice is the persisted picker state.
type choice struct {
	Harness string `json:"harness,omitempty"`
	Model   string `json:"model,omitempty"`
	Effort  string `json:"effort,omitempty"`
}

func lastChoicePath() (string, error) {
	dir, err := cacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "ec", lastChoiceFile), nil
}

// loadChoice reads the remembered picker selection. A missing or malformed
// file is not an error: defaults simply come from detection.
func loadChoice() choice {
	path, err := lastChoicePath()
	if err != nil {
		return choice{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return choice{}
	}
	var c choice
	if json.Unmarshal(data, &c) != nil {
		return choice{}
	}
	return c
}

// Defaults resolves the picker's preselection: the env override wins, then the
// remembered choice, then the first detected harness with empty model/effort.
// A remembered harness that is no longer installed falls back instead of
// failing, so uninstalling a harness cannot wedge the picker.
func (b *Bridge) Defaults() (harness, model, effort string) {
	remembered := loadChoice()
	chosen, ok := b.agentNamed(remembered.Harness)

	switch {
	case os.Getenv(EnvHarness) != "":
		name := os.Getenv(EnvHarness)
		if a, found := b.agentNamed(name); found {
			chosen, ok = a, true
		} else {
			// An env harness that is not installed is still honoured as a name;
			// the run then fails closed with a clear message.
			chosen, ok = agentbridge.Agent{Name: name, Binary: name}, true
		}
	case ok:
	case len(b.Agents) > 0:
		chosen, ok = b.Agents[0], true
	}

	harness = chosen.Name
	if env := os.Getenv(EnvModel); env != "" {
		model = env
	} else {
		model = remembered.Model
	}
	if env := os.Getenv(EnvEffort); env != "" {
		effort = env
	} else {
		effort = remembered.Effort
	}
	if !ok {
		return "", model, effort
	}
	return harness, model, effort
}

// Remember persists the picker's selection. A failure to write the cache is
// ignored: remembering is a convenience, never a precondition for a run.
func (b *Bridge) Remember(harness, model, effort string) {
	path, err := lastChoicePath()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	data, err := json.Marshal(choice{Harness: harness, Model: model, Effort: effort})
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

// BuildRequest builds the single-conflict handoff payload. The wire shape is
// the same one `ec --emit` prints, so a harness that works here works there.
func BuildRequest(path, ours, base, theirs string) handoff.Request {
	return handoff.Request{Version: handoff.ProtocolVersion, Files: []handoff.File{{
		Path: path,
		Conflicts: []handoff.HandoffConflict{{
			Conflict: handoff.Conflict{
				ID:      1,
				Ours:    ours,
				Base:    base,
				Theirs:  theirs,
				HasBase: base != "",
			},
		}},
	}}}
}

// Resolve runs one harness headless on one conflict and returns the resolved
// text. Every failure mode — no headless mode, non-zero exit, unparseable or
// marker-carrying reply — is an error, so the caller never applies a guess.
func (b *Bridge) Resolve(ctx context.Context, harness, model, effort, path, ours, base, theirs string) (string, error) {
	a, ok := b.agentNamed(harness)
	if !ok {
		a = agentbridge.Agent{Name: harness, Binary: harness}
	}
	if _, _, headless := b.Capabilities(harness); !headless {
		return "", fmt.Errorf("%s: %w", harness, agentbridge.ErrNoHeadless)
	}
	prompt, err := handoff.Prompt(BuildRequest(path, ours, base, theirs))
	if err != nil {
		return "", err
	}
	invoker := b.Invoker
	if invoker == nil {
		invoker = agentbridge.ProcessInvoker{}
	}
	result, err := invoker.Invoke(ctx, agentbridge.Request{
		Agent:  a,
		Model:  model,
		Effort: effort,
		Prompt: prompt,
		Cwd:    filepath.Dir(path),
	})
	if err != nil {
		return "", err
	}
	body, err := handoff.ExtractResponse([]byte(result.Output))
	if err != nil {
		return "", fmt.Errorf("%s: %w", harness, err)
	}
	resp, err := handoff.ParseResponse(body)
	if err != nil {
		return "", fmt.Errorf("%s: %w", harness, err)
	}
	text, err := handoff.ResolutionFor(resp, 1)
	if err != nil {
		return "", fmt.Errorf("%s: %w", harness, err)
	}
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("%s: empty resolution", harness)
	}
	return text, nil
}
